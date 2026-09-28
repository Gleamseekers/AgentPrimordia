package builtin

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

// TestValidateIPNotInternal_保留网段 表驱动覆盖各保留/特殊用途网段的边界 IP：
// CGNAT、192.0.0.0/24、基准测试、240.0.0.0/4、6to4、NAT64、文档段、
// IPv4-mapped IPv6 等；同时覆盖边界外的公网 IP（不应误杀）。
func TestValidateIPNotInternal_保留网段(t *testing.T) {
	blocked := []struct {
		name string
		ip   string
	}{
		// CGNAT 100.64.0.0/10（RFC 6598）边界
		{"CGNAT 起始", "100.64.0.0"},
		{"CGNAT 中部", "100.100.100.100"},
		{"CGNAT 结束", "100.127.255.255"},
		{"CGNAT IPv4-mapped", "::ffff:100.64.0.1"},
		// 192.0.0.0/24 IETF 协议赋值（RFC 6890）
		{"192.0.0.0/24 起始", "192.0.0.0"},
		{"192.0.0.0/24 结束", "192.0.0.255"},
		{"192.0.0.0/24 mapped", "::ffff:192.0.0.8"},
		// 198.18.0.0/15 基准测试（RFC 2544）
		{"基准测试 起始", "198.18.0.0"},
		{"基准测试 结束", "198.19.255.255"},
		// 240.0.0.0/4 保留段（RFC 1112，含 255.255.255.255）
		{"保留段 起始", "240.0.0.1"},
		{"保留段 广播地址", "255.255.255.255"},
		// 6to4 2002::/16（RFC 3056）
		{"6to4 边界内", "2002:0607:1234::1"},
		{"6to4 起始", "2002::"},
		// NAT64 64:ff9b::/96（RFC 6052）
		{"NAT64 边界内", "64:ff9b::1.2.3.4"},
		{"NAT64 起始", "64:ff9b::"},
		// 既有行为必须保持：环回/链路本地/元数据地址
		{"环回", "127.0.0.1"},
		{"环回 mapped", "::ffff:127.0.0.1"},
		{"IPv6 环回", "::1"},
		{"链路本地元数据", "169.254.169.254"},
		{"链路本地 mapped", "::ffff:169.254.169.254"},
		{"私有 10 段", "10.0.0.1"},
		{"私有 192.168", "192.168.1.1"},
		{"组播", "224.0.0.1"},
		{"未指定地址", "0.0.0.0"},
		{"本网络 0.0.0.0/8", "0.1.2.3"},
		// 文档段（RFC 5737 / RFC 3849）
		{"TEST-NET-1", "192.0.2.1"},
		{"TEST-NET-2", "198.51.100.7"},
		{"TEST-NET-3", "203.0.113.9"},
		{"IPv6 文档段", "2001:db8::1"},
		// IPv6 ULA / 链路本地
		{"IPv6 ULA", "fd00::1"},
		{"IPv6 链路本地", "fe80::1"},
	}

	allowed := []struct {
		name string
		ip   string
	}{
		{"公网 IPv4", "8.8.8.8"},
		{"公网 IPv4-2", "1.1.1.1"},
		{"公网 IPv4 mapped", "::ffff:8.8.8.8"},
		{"CGNAT 下边界外", "100.63.255.255"},
		{"CGNAT 上边界外", "100.128.0.0"},
		{"192.0.0.0/24 外", "192.0.1.0"},
		{"基准测试 下边界外", "198.17.255.255"},
		{"基准测试 上边界外", "198.20.0.0"},
		{"TEST-NET-1 外", "192.0.1.1"},
		{"6to4 边界外", "2003::1"},
		{"NAT64 边界外", "64:ff9a::1"},
		{"公网 IPv6", "2606:4700:4700::1111"},
	}

	for _, tc := range blocked {
		t.Run("拦截/"+tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("测试用 IP 解析失败: %s", tc.ip)
			}
			if err := validateIPNotInternal(ip); err == nil {
				t.Errorf("IP %s 应被拦截，实际放行", tc.ip)
			}
		})
	}

	for _, tc := range allowed {
		t.Run("放行/"+tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("测试用 IP 解析失败: %s", tc.ip)
			}
			if err := validateIPNotInternal(ip); err != nil {
				t.Errorf("公网 IP %s 应放行，实际拦截: %v", tc.ip, err)
			}
		})
	}
}

// TestWeb_SSRF_保留网段端到端 通过 Execute 端到端验证 CGNAT 等保留段被
// Transport.DialContext 实时拦截（无需真实网络：字面 IP 不触发 DNS）。
func TestWeb_SSRF_保留网段端到端(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"CGNAT", "http://100.64.0.1/"},
		{"基准测试段", "http://198.18.0.1/"},
		{"保留段", "http://240.0.0.1/"},
		{"6to4", "http://[2002:0607:1234::1]/"},
		{"NAT64", "http://[64:ff9b::1.2.3.4]/"},
		{"元数据地址", "http://169.254.169.254/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			web := NewWeb()
			args, _ := json.Marshal(map[string]any{
				"action": "fetch",
				"url":    tc.url,
			})
			result, err := web.Execute(context.Background(), args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("URL %s 应被 SSRF 防护拦截", tc.url)
			}
			if !strings.Contains(result.Content, "internal") &&
				!strings.Contains(result.Content, "private") &&
				!strings.Contains(result.Content, "reserved") {
				t.Errorf("期望 SSRF 拦截信息，got: %s", result.Content)
			}
		})
	}
}
