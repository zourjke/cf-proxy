// Package config 提供 DNS 优选能力。
//
// 说明：原始二进制中该包为私有模块 mediaTool/config，本仓库按实测行为
// 重新实现，对外签名保持一致（见 docs/README_DEV.md）。
package config

import (
	"context"
	"net"
	"sync"
	"time"
)

// maxCost 是单个 DNS 的测速上限，超过即视为不可用。
const maxCost = 5 * time.Second

// queryTimeout 单次解析超时。
const queryTimeout = 3 * time.Second

var mu sync.Mutex

// dns 返回一个强制走指定 DNS 服务器的解析器。
func dns(server string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: queryTimeout}
			return d.DialContext(ctx, network, server)
		},
	}
}

// avgCost 返回该 DNS 解析全部测速域名的平均耗时；全部失败时返回 maxCost。
func avgCost(server string, domains []string) time.Duration {
	r := dns(server)
	var sum time.Duration
	var n int
	for _, dom := range domains {
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		start := time.Now()
		_, err := r.LookupHost(ctx, dom)
		cancel()
		if err != nil {
			continue
		}
		sum += time.Since(start)
		n++
	}
	if n == 0 {
		return maxCost
	}
	return sum / time.Duration(n)
}

// SelectFastestDNSCustom 并发测速全部候选 DNS，返回平均耗时最低者。
// servers 为空时返回空字符串。
func SelectFastestDNSCustom(servers []string, domains []string) string {
	if len(servers) == 0 {
		return ""
	}
	type result struct {
		server string
		cost   time.Duration
	}
	ch := make(chan result, len(servers))
	for _, s := range servers {
		go func(s string) { ch <- result{s, avgCost(s, domains)} }(s)
	}

	best := ""
	bestCost := maxCost
	for range servers {
		r := <-ch
		if r.cost < bestCost {
			bestCost = r.cost
			best = r.server
		}
	}
	if best == "" {
		best = servers[0]
	}

	mu.Lock()
	defer mu.Unlock()
	return best
}
