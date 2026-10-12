package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/imroc/req/v3"
)

func TestShouldFilterHeaderName(t *testing.T) {
	filtered := []string{
		"", "   ",
		"start", "END", "ETag", "eagleid", "Range", "Connection",
		"http-client-ip", "remote-addr", "accept-encoding", "proxy-connection",
		"content-size", "transfer-encoding", "reqable-id",
		"if-none-match", "IF-MODIFIED-SINCE", "x-forwarded-for", "X-Real-IP",
	}
	for _, k := range filtered {
		if !shouldFilterHeaderName(k) {
			t.Errorf("shouldFilterHeaderName(%q) = false, 期望 true", k)
		}
	}

	kept := []string{"user-agent", "referer", "cookie", "origin", "accept", "accept-language"}
	for _, k := range kept {
		if shouldFilterHeaderName(k) {
			t.Errorf("shouldFilterHeaderName(%q) = true, 期望 false", k)
		}
	}
}

func TestSetTLSFingerprintNoPanic(t *testing.T) {
	uas := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/138.0.0.0",
		"Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Version/17.0 Safari/605.1.15",
		"Mozilla/5.0 (Linux; Android 14) Chrome/120.0.0.0 Mobile",
		"Mozilla/5.0 (Windows NT 10.0) Edg/120.0.0.0",
		"",
	}
	for _, ua := range uas {
		c := createRestClient(ua)
		if c == nil {
			t.Fatalf("createRestClient(%q) 返回 nil", ua)
		}
	}
}

func TestHandleHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("响应体 = %q", rec.Body.String())
	}
}

func TestProxyMethodNotAllowed(t *testing.T) {
	rec := httptest.NewRecorder()
	handleProxyRequest(rec, httptest.NewRequest(http.MethodPost, "/?url=http://example.com", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("状态码 = %d, 期望 405", rec.Code)
	}
}

func TestProxyMissingURL(t *testing.T) {
	rec := httptest.NewRecorder()
	handleProxyRequest(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, 期望 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "cf代理缺少url参数") {
		t.Fatalf("响应体 = %q", rec.Body.String())
	}
}

func TestProxyBadURL(t *testing.T) {
	rec := httptest.NewRecorder()
	handleProxyRequest(rec, httptest.NewRequest(http.MethodGet, "/?url=::bad::", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, 期望 400", rec.Code)
	}
}

func TestProxyForwardAndHeaderFilter(t *testing.T) {
	var target string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Forwarded-For"); got != "" {
			t.Errorf("x- 前缀头未被过滤: %q", got)
		}
		// Referer 指向同一 Host 时会被改写为本次目标 URL
		if got := r.Header.Get("Referer"); got != target {
			t.Errorf("Referer = %q, 期望改写为目标 URL %q", got, target)
		}
		w.Header().Set("Content-Language", "zh-CN")
		w.Header().Set("Etag", "should-be-filtered")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello-proxy"))
	}))
	defer upstream.Close()

	target = upstream.URL + "/x?a=1"
	req := httptest.NewRequest(http.MethodGet, "/?url="+url.QueryEscape(target), nil)
	req.Host = "example.com"
	req.Header.Set("Referer", "http://example.com/page")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	rec := httptest.NewRecorder()

	handleProxyRequest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200; body=%q", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello-proxy" {
		t.Fatalf("响应体 = %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Language") != "zh-CN" {
		t.Fatalf("上游响应头未透传: %v", rec.Header())
	}
	// etag 在黑名单内，响应头同样应被过滤
	if rec.Header().Get("Etag") != "" {
		t.Fatalf("etag 响应头未被过滤: %v", rec.Header())
	}
}

func TestProxyRangeTo206(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=0-3" {
			t.Errorf("Range = %q, 期望透传", r.Header.Get("Range"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("abcd"))
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/?url="+url.QueryEscape(upstream.URL), nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()

	handleProxyRequest(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("状态码 = %d, 期望 206", rec.Code)
	}
}

// resetJars 清空全局 cookie jar，避免用例间互相影响。
func resetJars() {
	jarMu.Lock()
	jars = map[string]http.CookieJar{}
	jarMu.Unlock()
}

func proxyGet(t *testing.T, target, ua string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/?url="+url.QueryEscape(target), nil)
	req.Header.Set("User-Agent", ua)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handleProxyRequest(rec, req)
	return rec
}

// 上游 Set-Cookie 应被 jar 接住，并在后续同 UA 请求中自动带上。
func TestCookieSessionPersistence(t *testing.T) {
	resetJars()

	var echoed []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/set":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc123", Path: "/"})
			w.Write([]byte("set"))
		case "/echo":
			echoed = append(echoed, r.Header.Get("Cookie"))
			w.Write([]byte("echo"))
		}
	}))
	defer upstream.Close()

	if rec := proxyGet(t, upstream.URL+"/set", "ua-persist", nil); rec.Code != http.StatusOK {
		t.Fatalf("/set 状态码 = %d", rec.Code)
	}
	if rec := proxyGet(t, upstream.URL+"/echo", "ua-persist", nil); rec.Code != http.StatusOK {
		t.Fatalf("/echo 状态码 = %d", rec.Code)
	}
	if len(echoed) != 1 || !strings.Contains(echoed[0], "sid=abc123") {
		t.Fatalf("第二次请求未带上 cookie: %v", echoed)
	}

	// 不同 UA 之间必须隔离
	echoed = nil
	if rec := proxyGet(t, upstream.URL+"/echo", "ua-other", nil); rec.Code != http.StatusOK {
		t.Fatalf("/echo(其他UA) 状态码 = %d", rec.Code)
	}
	if len(echoed) != 1 || strings.Contains(echoed[0], "sid=abc123") {
		t.Fatalf("不同 UA 之间 cookie 泄漏: %v", echoed)
	}
}

// 调用方传入的 Cookie（如外部浏览器取得的 cf_clearance）应被注入并转发。
func TestCallerCookieInjection(t *testing.T) {
	resetJars()

	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Cookie")
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	rec := proxyGet(t, upstream.URL+"/x", "ua-inject",
		[]*http.Cookie{{Name: "cf_clearance", Value: "xyz789"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if !strings.Contains(got, "cf_clearance=xyz789") {
		t.Fatalf("注入的 cookie 未转发: %q", got)
	}
}

// 注入的 cookie 应进入 jar，后续不带 cookie 的请求仍能自动带上。
func TestInjectedCookiePersists(t *testing.T) {
	resetJars()

	var got []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Cookie"))
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	proxyGet(t, upstream.URL+"/a", "ua-inject2",
		[]*http.Cookie{{Name: "cf_clearance", Value: "keepme"}})
	proxyGet(t, upstream.URL+"/b", "ua-inject2", nil)

	if len(got) != 2 || !strings.Contains(got[1], "cf_clearance=keepme") {
		t.Fatalf("注入的 cookie 未持久化: %v", got)
	}
}

// ---------- 浏览器兜底：降级保证 ----------

func resetBrowser() {
	browserMu.Lock()
	browserInst, browserErr, browserTried = nil, nil, false
	browserMu.Unlock()
}

func challengeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cf-Mitigated", "challenge")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("challenge-body"))
	}))
}

func TestIsChallenge(t *testing.T) {
	mk := func(k, v string) *req.Response {
		h := http.Header{}
		h.Set(k, v) // Set 会规范化 key，与真实 HTTP 解析行为一致
		return &req.Response{Response: &http.Response{Header: h}}
	}
	if !isChallenge(mk("Cf-Mitigated", "challenge")) {
		t.Error("Cf-Mitigated: challenge 应判定为挑战页")
	}
	if !isChallenge(mk("Cf-Mitigated", "Challenge")) {
		t.Error("值的大小写不敏感匹配失败")
	}
	if isChallenge(mk("Server", "cloudflare")) {
		t.Error("普通响应不应判定为挑战页")
	}
	if isChallenge(mk("Cf-Mitigated", "blocked")) {
		t.Error("Cf-Mitigated 非 challenge 值不应判定为挑战页")
	}
	if isChallenge(nil) {
		t.Error("nil 不应判定为挑战页")
	}
}

// 核心保证：未配置 -browser 时，遇到挑战页也必须原样返回，
// 且绝不启动浏览器 —— L1/L2 行为不受影响。
func TestNoBrowserWhenUnset(t *testing.T) {
	resetBrowser()
	browserPath = ""
	defer func() { browserPath = "" }()

	up := challengeUpstream(t)
	defer up.Close()

	rec := proxyGet(t, up.URL, "ua-nobrowser", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "challenge-body" {
		t.Fatalf("纯 HTTP 模式应原样返回挑战页: %d %q", rec.Code, rec.Body.String())
	}
	browserMu.Lock()
	tried := browserTried
	browserMu.Unlock()
	if tried {
		t.Fatal("未配置 -browser 时不应尝试启动浏览器")
	}
}

// 配置了不可用的 -browser 路径时必须降级：原样返回，且只尝试一次。
func TestBadBrowserPathDegrades(t *testing.T) {
	resetBrowser()
	browserPath = "/nonexistent/chrome-does-not-exist"
	defer func() {
		browserPath = ""
		resetBrowser()
	}()

	up := challengeUpstream(t)
	defer up.Close()

	// 第一次：尝试启动失败 -> 降级，原样返回
	rec := proxyGet(t, up.URL, "ua-badpath", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "challenge-body" {
		t.Fatalf("浏览器不可用时应原样返回: %d %q", rec.Code, rec.Body.String())
	}
	browserMu.Lock()
	err1, tried1 := browserErr, browserTried
	browserMu.Unlock()
	if !tried1 || err1 == nil {
		t.Fatal("应记录一次启动失败")
	}

	// 第二次：不得重复尝试启动 —— 复用缓存的错误对象（指针相同即证明未重新启动）
	rec = proxyGet(t, up.URL, "ua-badpath", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("第二次请求状态码 = %d", rec.Code)
	}
	browserMu.Lock()
	err2, inst2 := browserErr, browserInst
	browserMu.Unlock()
	if inst2 != nil {
		t.Fatal("浏览器不可用时不应有实例")
	}
	if err2 != err1 {
		t.Fatal("第二次请求重新尝试了启动（错误对象不同），失败记忆未生效")
	}
}

// 普通响应绝不能触发浏览器路径（零误判）。
func TestNormalResponseNeverTouchesBrowser(t *testing.T) {
	resetBrowser()
	browserPath = "/nonexistent/chrome-does-not-exist"
	defer func() {
		browserPath = ""
		resetBrowser()
	}()

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("normal"))
	}))
	defer up.Close()

	rec := proxyGet(t, up.URL, "ua-normal", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "normal" {
		t.Fatalf("普通响应被污染: %d %q", rec.Code, rec.Body.String())
	}
	browserMu.Lock()
	tried := browserTried
	browserMu.Unlock()
	if tried {
		t.Fatal("普通响应不应触发浏览器路径")
	}
}
