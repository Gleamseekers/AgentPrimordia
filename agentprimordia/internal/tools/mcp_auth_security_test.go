// mcp_auth_security_test.go — tools 包 MCPServer 认证加固测试（P1 安全修复）
//
// 背景：
//  1. API key 比较非常量时间（timing attack 面）；
//  2. strings.TrimPrefix 不校验 "Bearer " 方案前缀——把裸 key 直接作为
//     Authorization 值也能通过认证。
package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMCPServer_APIKey_RawKeyWithoutBearerRejected 验证裸 key 被拒绝
// （修复前 TrimPrefix 直接放行）
func TestMCPServer_APIKey_RawKeyWithoutBearerRejected(t *testing.T) {
	reg := NewRegistry()
	server := NewMCPServer(MCPServerConfig{
		Name:    "auth-server",
		Version: "1.0.0",
		APIKey:  "secret-key",
	}, reg)

	body := mcpServerRequest(1, "ping", nil)
	rec := mcpDoRequest(server, "ping", body, map[string]string{
		"Authorization": "secret-key", // 无 Bearer 前缀
	})

	resp := mcpDecodeResponse(rec)
	if resp.Error == nil {
		t.Fatal("裸 key（无 Bearer 前缀）应被拒绝")
	}
	if resp.Error.Code != -32001 {
		t.Errorf("错误码 = %d, 期望 -32001", resp.Error.Code)
	}
}

// TestMCPServer_APIKey_BearerCaseSensitiveScheme 验证方案前缀大小写敏感
// （RFC 6750 规定 scheme 大小写不敏感，但本实现采用严格 "Bearer " 前缀，
// 与 tools 包既有测试保持一致；小写 bearer 应被拒绝而非静默接受）
func TestMCPServer_APIKey_BearerCaseSensitiveScheme(t *testing.T) {
	reg := NewRegistry()
	server := NewMCPServer(MCPServerConfig{
		Name:    "auth-server",
		Version: "1.0.0",
		APIKey:  "secret-key",
	}, reg)

	body := mcpServerRequest(1, "ping", nil)
	rec := mcpDoRequest(server, "ping", body, map[string]string{
		"Authorization": "bearer secret-key",
	})

	resp := mcpDecodeResponse(rec)
	if resp.Error == nil {
		t.Fatal("小写 bearer 方案前缀应被拒绝")
	}
}

// TestMCPServer_APIKey_CorrectKeyStillAccepted 回归：正确 Bearer 仍放行
func TestMCPServer_APIKey_CorrectKeyStillAccepted(t *testing.T) {
	reg := NewRegistry()
	server := NewMCPServer(MCPServerConfig{
		Name:    "auth-server",
		Version: "1.0.0",
		APIKey:  "secret-key",
	}, reg)

	body := mcpServerRequest(1, "ping", nil)
	rec := mcpDoRequest(server, "ping", body, map[string]string{
		"Authorization": "Bearer secret-key",
	})

	resp := mcpDecodeResponse(rec)
	if resp.Error != nil {
		t.Fatalf("正确 Bearer 令牌应成功: %s", resp.Error.Message)
	}
}

// TestBearerAuthorized 单元测试认证辅助函数
func TestBearerAuthorized(t *testing.T) {
	cases := []struct {
		name   string
		header string
		key    string
		want   bool
	}{
		{"正确", "Bearer secret", "secret", true},
		{"错误令牌", "Bearer wrong", "secret", false},
		{"缺前缀", "secret", "secret", false},
		{"空头", "", "secret", false},
		{"空前缀仅空格", "Bearer ", "secret", false},
		{"key 为空不启用", "Bearer ", "", true == false}, // key 为空时认证不启用，此路径不应到达；语义上返回 false
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bearerAuthorized(tc.header, tc.key); got != tc.want {
				t.Errorf("bearerAuthorized(%q, %q) = %v, want %v", tc.header, tc.key, got, tc.want)
			}
		})
	}
}

// TestMCPServer_ToolsCall_ScopePolicyInjection 验证可为 MCPServer 注入
// ScopePolicy（tools/call 默认执行器无策略，可调任意工具含 shell——
// 部署方应显式注入策略收敛暴露面）
func TestMCPServer_ToolsCall_ScopePolicyInjection(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&mockTool{name: "shell", description: "dangerous", response: "ran"})
	server := NewMCPServer(MCPServerConfig{Name: "scoped", Version: "1.0.0"}, reg)

	policy := &denyAllScopePolicy{}
	server.SetScopePolicy(policy, "agent-1")

	body := mcpServerRequest(1, "tools/call", map[string]any{
		"name":      "shell",
		"arguments": map[string]any{"path": "/etc/passwd"},
	})
	rec := mcpDoRequest(server, "tools/call", body, nil)
	resp := mcpDecodeResponse(rec)
	if resp.Error != nil {
		// JSON-RPC 层错误（tool not found 等）不应出现
		if strings.Contains(resp.Error.Message, "not found") {
			t.Fatalf("工具应存在: %s", resp.Error.Message)
		}
	}
	var result MCPToolCallResult
	resultJSON, _ := json.Marshal(resp.Result)
	_ = json.Unmarshal(resultJSON, &result)
	if !result.IsError {
		t.Fatal("ScopePolicy 拒绝执行时应返回 isError=true")
	}
}

// denyAllScopePolicy 拒绝一切资源的 ScopePolicy 测试桩
type denyAllScopePolicy struct{}

func (d *denyAllScopePolicy) Allow(agentID, resource string) bool { return false }

func (d *denyAllScopePolicy) Validate(agentScopes map[string][]string) error { return nil }
