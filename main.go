// cf-proxy —— Cloudflare 过盾代理服务
//
// 用法：
//
//	cf-proxy -port 12525 [-proxy http://host:port] [-dns 8.8.8.8]
//	GET http://127.0.0.1:12525?url=<urlencode(目标URL)>
//
// 本文件为 cf-proxy-linux-amd64 的逆向重建实现，原始源文件名为 demo.go，
// 还原过程与逐条证据见 docs/README_DEV.md。
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/imroc/req/v3"
	"github.com/sirupsen/logrus"

	"cf-proxy/config"
)

// 构建信息，由 -ldflags 注入。
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// 命令行参数（默认值取自二进制静态数据：port 默认 0x30ed = 12525）。
var (
	proxyURL  string
	dnsServer string
)

// defaultUA 未指定 User-Agent 时使用的默认值（二进制内 111 字节常量）。
const defaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

// DNS 优选候选与测速域名（二进制静态数据实测）。
var (
	dnsCandidates  = []string{"8.8.8.8", "8.8.4.4", "119.29.29.29", "182.254.116.116",
		"180.76.76.76", "223.5.5.5", "223.6.6.6"}
	dnsTestDomains = []string{"pan.baidu.com", "pan.quark.cn", "drive.uc.cn"}
)

func main() {
	port := flag.Int("port", 12525, "监听端口号，默认为12525")
	flag.StringVar(&proxyURL, "proxy", "", "代理服务器地址，格式：http://host:port 或 socks5://host:port")
	flag.StringVar(&dnsServer, "dns", "", "DNS 服务地址 (可选)")
	flag.Parse()

	// 后台异步优选 DNS，避免阻塞启动。
	go func() {
		if dnsServer == "" {
			dnsServer = config.SelectFastestDNSCustom(dnsCandidates, dnsTestDomains)
		}
	}()

	logrus.SetLevel(logrus.InfoLevel)

	mux := http.NewServeMux()
	// /health 为插件市场健康检查新增，不属于原始实现，不影响代理路径。
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/", handleProxyRequest)

	logrus.Infof("cf代理服务器启动，监听端口: %d", *port)
	logrus.Infof("版本: %s 构建: %s 提交: %s", Version, BuildTime, GitCommit)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: mux,
	}
	if err := srv.ListenAndServe(); err != nil {
		logrus.Fatalf("服务器启动失败: %v", err)
	}
}

// handleHealth 供 drpy-node 插件市场做健康探测。
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"version":%q,"dns":%q}`, Version, dnsServer)
}

// handleProxyRequest 处理 GET /?url=<目标>[&其余参数]。
func handleProxyRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		// 原始实现的该分支文案未能从字符串池定位，此处为等价实现。
		http.Error(w, "cf代理仅支持GET请求", http.StatusMethodNotAllowed)
		return
	}

	// 256 KiB 输出缓冲（二进制实测 0x40000）。
	bw := bufio.NewWriterSize(w, 256*1024)
	defer bw.Flush()

	connHeader := r.Header.Get("Connection")
	if connHeader == "" {
		connHeader = "close"
	}

	q := r.URL.Query()
	rawURL := q.Get("url")
	if rawURL == "" {
		http.Error(w, "cf代理缺少url参数", http.StatusBadRequest)
		return
	}

	// 除 url 外的 query 参数原样拼回目标 URL。
	for k, vs := range q {
		if k == "url" {
			continue
		}
		rawURL += "&" + k + "=" + vs[0]
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("cf代理解析URL错误: %v", err), http.StatusBadRequest)
		return
	}
	r.URL = u

	// 重建请求头。
	headers := map[string]string{}
	for k, vs := range r.Header {
		if shouldFilterHeaderName(k) {
			continue
		}
		switch strings.ToLower(k) {
		case "referer":
			// 仅当原 Referer 指向同一 Host 时保留，并改写为本次目标 URL。
			if strings.Contains(strings.Join(vs, ""), r.Host) {
				headers["Referer"] = rawURL
			}
		case "host":
			headers["Host"] = u.Host
		case "cache-control":
			headers[k] = "no-store, no-cache"
		case "pragma":
			headers[k] = "no-cache"
		default:
			if _, exists := headers[k]; !exists {
				headers[k] = strings.Join(vs, ",")
			}
		}
	}
	headers["Origin"] = fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	ua := headers["User-Agent"]
	if ua == "" {
		ua = defaultUA
	}

	// Range 透传 → 206，否则 200。
	status := http.StatusOK
	if rng := r.Header.Get("Range"); rng != "" {
		headers["Range"] = rng
		status = http.StatusPartialContent
	}

	client := createRestClient(ua)
	for k, v := range headers {
		client.SetCommonHeader(k, v)
	}

	// 透传客户端 Cookie。
	for _, ck := range r.Cookies() {
		client.GetTransport().Cookies = append(client.GetTransport().Cookies,
			&http.Cookie{Name: ck.Name, Value: ck.Value})
	}

	if proxyURL != "" {
		logrus.Infof("使用代理服务器: %s", proxyURL)
		client.SetProxyURL(proxyURL)
	}

	resp, err := client.R().
		SetContext(r.Context()).
		SetRetryCount(3).
		Send(r.Method, rawURL)
	if err != nil {
		if errors.Is(r.Context().Err(), context.Canceled) {
			logrus.Infof("请求已取消: %s", rawURL)
			http.Error(w, "请求已取消", http.StatusRequestTimeout)
		} else {
			logrus.Infof("请求失败: %s %v", rawURL, err)
			http.Error(w, fmt.Sprintf("请求失败: %v", err), http.StatusBadGateway)
		}
		return
	}

	defer func() {
		if resp != nil && resp.Response != nil && resp.Response.Body != nil {
			if resp.Response.Body.Close() != nil {
				return
			}
		}
		client.GetTransport().CloseIdleConnections()
	}()

	processResponse(w, resp, status, connHeader, bw)
}

// createRestClient 构造带 TLS 指纹伪装与自定义 DNS 的 req 客户端。
func createRestClient(userAgent string) *req.Client {
	client := req.C().SetTimeout(30 * time.Second)
	client.SetCommonRetryBackoffInterval(3*time.Second, 100*time.Millisecond)
	client.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})

	// 重试条件：网络错误，或结果状态非成功（含 CF 挑战）。
	client.AddCommonRetryCondition(func(resp *req.Response, err error) bool {
		if err != nil {
			logrus.Infof("请求错误，准备重试: %v", err)
			return true
		}
		if resp != nil && resp.Response != nil {
			if resp.ResultState() != req.SuccessState {
				logrus.Infof("响应异常，准备重试: %d", resp.Response.StatusCode)
				return true
			}
		}
		return false
	})

	if dnsServer != "" {
		logrus.Infof("使用DNS: %s", dnsServer)

		// 非 IPv4 则补方括号，供 Dial 使用。
		if ip := net.ParseIP(dnsServer); ip == nil || ip.To4() == nil {
			if !strings.HasPrefix(dnsServer, "[") {
				dnsServer = "[" + dnsServer
			}
			if !strings.HasSuffix(dnsServer, "]") {
				dnsServer = dnsServer + "]"
			}
		}

		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				if !strings.Contains(dnsServer, ":53") {
					return dialer.DialContext(ctx, network, dnsServer+":53")
				}
				return dialer.DialContext(ctx, network, dnsServer)
			},
		}
		client.SetDial(func(ctx context.Context, network, addr string) (net.Conn, error) {
			if i := strings.LastIndexByte(addr, ':'); i > 0 {
				host := strings.TrimPrefix(strings.TrimSuffix(addr[:i], "]"), "[")
				port := addr[i+1:]
				if ips, err := resolver.LookupIPAddr(ctx, host); err == nil && len(ips) > 0 {
					var lastErr error
					for _, ip := range ips {
						c, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
						if err == nil {
							return c, nil
						}
						lastErr = err
					}
					if lastErr != nil {
						return nil, lastErr
					}
				}
			}
			return dialer.DialContext(ctx, network, addr)
		})
	}

	client.GetTransport().SetTLSHandshakeTimeout(30 * time.Second)
	client.GetTransport().SetResponseHeaderTimeout(30 * time.Second)

	SetTLSFingerprint(client, userAgent)
	return client
}

// SetTLSFingerprint 按 User-Agent 选择 TLS 指纹。
func SetTLSFingerprint(client *req.Client, userAgent string) {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "edge"):
		client.ImpersonateChrome()
	case strings.Contains(ua, "chrome"):
		client.ImpersonateChrome()
	case strings.Contains(ua, "android"):
		client.ImpersonateChrome()
	case strings.Contains(ua, "firefox"):
		client.ImpersonateFirefox()
	case strings.Contains(ua, "mac os"):
		client.ImpersonateSafari()
	default:
		client.ImpersonateChrome()
	}
}

// processResponse 回写响应头并流式转发 body。
func processResponse(w http.ResponseWriter, resp *req.Response, status int,
	connHeader string, bw *bufio.Writer) {

	// 301 / 302 且带 Location → 只回状态码与 Location，由客户端自行跟随。
	if (resp.StatusCode == http.StatusMovedPermanently ||
		resp.StatusCode == http.StatusFound) && resp.Header.Get("Location") != "" {
		loc := resp.Header.Get("Location")
		w.Header()[textproto.CanonicalMIMEHeaderKey("Location")] = []string{loc}
		w.WriteHeader(resp.StatusCode)
		logrus.Infof("重定向: %s", loc)
		return
	}

	if resp.StatusCode == http.StatusNotFound {
		w.WriteHeader(http.StatusNotFound)
		logrus.Infof("目标返回404")
		return
	}

	for k, vs := range resp.Header {
		if shouldFilterHeaderName(k) {
			continue
		}
		w.Header()[textproto.CanonicalMIMEHeaderKey(k)] = []string{strings.Join(vs, ",")}
	}
	w.Header()[textproto.CanonicalMIMEHeaderKey("Connection")] = []string{connHeader}

	w.WriteHeader(status)

	if resp.Body != nil {
		if _, err := io.Copy(bw, resp.Body); err == nil {
			logrus.Infof("转发完成")
		} else {
			logrus.Infof("转发中断: %v", err)
		}
	}
}

// shouldFilterHeaderName 判断请求头是否应被过滤（13 项 + if- / x- 前缀）。
func shouldFilterHeaderName(key string) bool {
	if len(strings.TrimSpace(key)) == 0 {
		return true
	}
	key = strings.ToLower(key)
	switch key {
	case "start", "end", "etag", "eagleid", "range", "connection",
		"http-client-ip", "remote-addr", "accept-encoding", "proxy-connection",
		"content-size", "transfer-encoding", "reqable-id":
		return true
	}
	return strings.HasPrefix(key, "if-") || strings.HasPrefix(key, "x-")
}
