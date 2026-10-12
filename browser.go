// 浏览器兜底：仅在「配置了 -browser」且「上游响应确实是 CF 挑战页」时启用。
//
// 未配置 -browser、或浏览器不可用时，本文件的代码路径不可达 ——
// L1/L2（纯 uTLS 指纹过盾）行为与不引入本文件时完全一致。
package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/imroc/req/v3"
	"github.com/sirupsen/logrus"
)

var (
	browserMu    sync.Mutex
	browserInst  *rod.Browser
	browserErr   error
	browserTried bool
)

// ensureBrowser 惰性启动浏览器，全程只尝试一次；失败后永久降级为纯 HTTP。
func ensureBrowser() (*rod.Browser, error) {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserTried {
		return browserInst, browserErr
	}
	browserTried = true

	l := launcher.New().
		Bin(browserPath).
		HeadlessNew(true). // 新版 headless 比老 headless 更难被识别
		NoSandbox(true).
		Set("disable-blink-features", "AutomationControlled").
		Set("disable-dev-shm-usage").
		Set("window-size", "1920,1080").
		Set("lang", "zh-CN")

	ctrl, err := l.Launch()
	if err != nil {
		browserErr = fmt.Errorf("启动浏览器失败: %w", err)
		logrus.Warnf("%v —— 已降级为纯 HTTP 模式，指纹过盾不受影响", browserErr)
		return nil, browserErr
	}
	br := rod.New().ControlURL(ctrl)
	if err := br.Connect(); err != nil {
		browserErr = fmt.Errorf("连接浏览器失败: %w", err)
		logrus.Warnf("%v —— 已降级为纯 HTTP 模式，指纹过盾不受影响", browserErr)
		return nil, browserErr
	}
	browserInst = br
	logrus.Infof("浏览器兜底已就绪: %s", browserPath)
	return br, nil
}

// isChallenge 判定上游响应是否为 Cloudflare 挑战页。
// 只认 Cf-Mitigated: challenge 响应头 —— CF 官方信号，零误判，
// 不会让普通 403/503 误触发浏览器。
func isChallenge(resp *req.Response) bool {
	if resp == nil || resp.Response == nil {
		return false
	}
	return strings.EqualFold(resp.Header.Get("Cf-Mitigated"), "challenge")
}

// solveChallenge 用浏览器打开目标页，等待 CF 下发 cf_clearance，返回全部 cookie。
//
// UA 必须与 HTTP 请求一致：cf_clearance 绑定 (出口 IP, User-Agent)，
// 不一致则拿到的 cookie 对 HTTP 请求无效。
func solveChallenge(target, ua string, timeout time.Duration) ([]*http.Cookie, error) {
	br, err := ensureBrowser()
	if err != nil {
		return nil, err
	}

	page, err := br.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, fmt.Errorf("创建标签页失败: %w", err)
	}
	defer func() { _ = page.Close() }()

	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{UserAgent: ua}); err != nil {
		return nil, fmt.Errorf("设置 UA 失败: %w", err)
	}
	if err := page.Navigate(target); err != nil {
		return nil, fmt.Errorf("打开目标失败: %w", err)
	}
	_ = page.WaitLoad()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cs, cerr := proto.NetworkGetAllCookies{}.Call(page)
		if cerr == nil {
			out := make([]*http.Cookie, 0, len(cs.Cookies))
			cleared := false
			for _, c := range cs.Cookies {
				out = append(out, &http.Cookie{Name: c.Name, Value: c.Value})
				if c.Name == "cf_clearance" {
					cleared = true
				}
			}
			if cleared {
				return out, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil, errors.New("过盾超时（未取得 cf_clearance）")
}
