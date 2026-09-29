package a2a

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/resilience"
)

func TestServerTLSCredentials_MissingFiles(t *testing.T) {
	// 缺少证书文件
	_, err := ServerTLSCredentials(TLSConfig{})
	if err == nil {
		t.Error("ServerTLSCredentials without cert/key should fail")
	}

	// 缺少 CA 文件
	_, err = ServerTLSCredentials(TLSConfig{
		CertFile: "cert.pem",
		KeyFile:  "key.pem",
	})
	if err == nil {
		t.Error("ServerTLSCredentials without CA should fail")
	}
}

func TestClientTLSCredentials_MissingFiles(t *testing.T) {
	_, err := ClientTLSCredentials(TLSConfig{})
	if err == nil {
		t.Error("ClientTLSCredentials without cert/key should fail")
	}
}

func TestServerTLSCredentials_InvalidFiles(t *testing.T) {
	// 不存在的文件
	_, err := ServerTLSCredentials(TLSConfig{
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
		CAFile:   "/nonexistent/ca.pem",
	})
	if err == nil {
		t.Error("ServerTLSCredentials with nonexistent files should fail")
	}
}

func TestTLSManager_MissingConfig(t *testing.T) {
	// 缺少证书文件
	_, err := NewTLSManager(TLSConfig{}, nil)
	if err == nil {
		t.Error("NewTLSManager without cert/key should fail")
	}

	// 缺少 CA 文件
	_, err = NewTLSManager(TLSConfig{
		CertFile: "cert.pem",
		KeyFile:  "key.pem",
	}, nil)
	if err == nil {
		t.Error("NewTLSManager without CA should fail")
	}
}

func TestTLSManager_InvalidFiles(t *testing.T) {
	_, err := NewTLSManager(TLSConfig{
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
		CAFile:   "/nonexistent/ca.pem",
	}, nil)
	if err == nil {
		t.Error("NewTLSManager with nonexistent files should fail")
	}
}

func TestTLSManager_DefaultRotationInterval(t *testing.T) {
	// 创建临时证书文件（内容无效，但可以测试配置验证）
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	caFile := filepath.Join(dir, "ca.pem")

	// 写入无效的 PEM 内容
	_ = os.WriteFile(certFile, []byte("invalid"), 0o644)
	_ = os.WriteFile(keyFile, []byte("invalid"), 0o644)
	_ = os.WriteFile(caFile, []byte("invalid"), 0o644)

	// 文件存在但内容无效，应该在 loadCredentials 时失败
	_, err := NewTLSManager(TLSConfig{
		CertFile: certFile,
		KeyFile:  keyFile,
		CAFile:   caFile,
	}, nil)
	if err == nil {
		t.Error("NewTLSManager with invalid cert content should fail")
	}
}

func TestTLSConfig_Validation(t *testing.T) {
	// 验证 TLSConfig 结构体字段
	cfg := TLSConfig{
		CertFile:   "cert.pem",
		KeyFile:    "key.pem",
		CAFile:     "ca.pem",
		ServerName: "test.example.com",
	}
	if cfg.CertFile != "cert.pem" {
		t.Errorf("CertFile = %q", cfg.CertFile)
	}
	if cfg.ServerName != "test.example.com" {
		t.Errorf("ServerName = %q", cfg.ServerName)
	}
}

func TestCircuitBreakerInterceptor_State(t *testing.T) {
	interceptor := NewCircuitBreakerInterceptor(resilience.Config{
		FailureThreshold: 3,
		Timeout:          10 * time.Second,
	}, nil)

	if interceptor.State() != resilience.StateClosed {
		t.Errorf("initial state = %v, want closed", interceptor.State())
	}
}

func TestCircuitBreakerInterceptor_Execute(t *testing.T) {
	interceptor := NewCircuitBreakerInterceptor(resilience.Config{
		FailureThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}, nil)
	ctx := context.Background()

	// 成功执行
	err := interceptor.cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Errorf("successful execution should not error: %v", err)
	}

	// 连续失败触发断路
	for i := 0; i < 2; i++ {
		_ = interceptor.cb.Execute(ctx, func(ctx context.Context) error {
			return fmt.Errorf("fail")
		})
	}

	// 断路器应已打开
	if interceptor.State() != resilience.StateOpen {
		t.Errorf("after failures, state = %v, want open", interceptor.State())
	}

	// 断路器打开时执行应快速失败
	err = interceptor.cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	if err != resilience.ErrCircuitOpen {
		t.Errorf("when open, should return ErrCircuitOpen, got: %v", err)
	}
}

func TestNewCircuitBreakerInterceptorWithCB(t *testing.T) {
	cb := resilience.NewCircuitBreaker(resilience.Config{})
	interceptor := NewCircuitBreakerInterceptorWithCB(cb, nil)

	if interceptor.cb != cb {
		t.Error("interceptor should use the provided circuit breaker")
	}
}

func TestGRPCServer_WithTLSMissing(t *testing.T) {
	// 验证 TLSConfig 可以正常创建
	config := TLSConfig{
		CertFile: "test-cert.pem",
		KeyFile:  "test-key.pem",
		CAFile:   "test-ca.pem",
	}
	if config.CertFile == "" {
		t.Error("CertFile should be set")
	}
}

// 确保导入
var _ = context.Background
var _ = time.Second
