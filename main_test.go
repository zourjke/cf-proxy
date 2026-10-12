package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
