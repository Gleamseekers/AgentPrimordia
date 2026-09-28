// server_auth_test.go — MCPServer Bearer 认证测试（P1 安全修复）
//
// 背景：mcp 子包 MCPServer 此前完全无认证，任何能访问 /mcp 端点的客户端
// 均可调用全部已注册工具。修复：WithAPIKey 选项（默认不改变行为）。
package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// postMCPAuth 发送带自定义 Authorization 头的 MCP 请求，返回原始响应。
func postMCPAuth(t *testing.T, base, authHeader string) (*http.Response, []byte) {
	t.Helper()
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+"/mcp", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data
}

func TestMCPServer_WithAPIKey_MissingHeader(t *testing.T) {
	s := NewMCPServer(WithAPIKey("secret-key"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, _ := postMCPAuth(t, srv.URL, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("缺少 Authorization 头应返回 401, 得到 %d", resp.StatusCode)
	}
}

func TestMCPServer_WithAPIKey_WrongKey(t *testing.T) {
	s := NewMCPServer(WithAPIKey("secret-key"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, _ := postMCPAuth(t, srv.URL, "Bearer wrong-key")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错误 Bearer 令牌应返回 401, 得到 %d", resp.StatusCode)
	}
}

func TestMCPServer_WithAPIKey_RawKeyWithoutBearerRejected(t *testing.T) {
	s := NewMCPServer(WithAPIKey("secret-key"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	// 不带 "Bearer " 方案前缀的裸 key 必须被拒绝（旧式 TrimPrefix 会放行）
	resp, _ := postMCPAuth(t, srv.URL, "secret-key")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("裸 key（无 Bearer 前缀）应返回 401, 得到 %d", resp.StatusCode)
	}
}

func TestMCPServer_WithAPIKey_CorrectKey(t *testing.T) {
	s := NewMCPServer(WithAPIKey("secret-key"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, data := postMCPAuth(t, srv.URL, "Bearer secret-key")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("正确 Bearer 令牌应返回 200, 得到 %d: %s", resp.StatusCode, data)
	}
	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(data, &rpcResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("正确令牌的 ping 应成功: %v", rpcResp.Error)
	}
}

func TestMCPServer_NoAPIKey_OpenAccess(t *testing.T) {
	// 默认（未设置 APIKey）保持既有行为：无需认证
	s := NewMCPServer()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, _ := postMCPAuth(t, srv.URL, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("无 APIKey 配置时应保持开放访问, 得到 %d", resp.StatusCode)
	}
}
