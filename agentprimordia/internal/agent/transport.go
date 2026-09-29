package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/transport"
)

// Transport 跨进程 Agent 通信传输层接口
// 类型别名保持向后兼容
type Transport = transport.Transport

// HTTPTransport 基于 HTTP 的跨进程 Agent 通信传输层
// 类型别名保持向后兼容
type HTTPTransport = transport.HTTPTransport

// TCPTransport 基于 TCP 的跨进程 Agent 通信传输层
// 类型别名保持向后兼容
type TCPTransport = transport.TCPTransport

// TCPTransportConfig TCP 传输层配置
// 类型别名保持向后兼容
type TCPTransportConfig = transport.TCPTransportConfig

// NewHTTPTransport 创建 HTTP 传输层实例
func NewHTTPTransport() *HTTPTransport {
	return transport.NewHTTPTransport()
}

// NewTCPTransport 创建 TCP 传输层实例
func NewTCPTransport() *TCPTransport {
	return transport.NewTCPTransport()
}

// NewTCPTransportWithConfig 使用配置创建 TCP 传输层实例
func NewTCPTransportWithConfig(cfg TCPTransportConfig) *TCPTransport {
	return transport.NewTCPTransportWithConfig(cfg)
}

// DefaultTCPTransportConfig 返回默认 TCP 传输层配置
func DefaultTCPTransportConfig() TCPTransportConfig {
	return transport.DefaultTCPTransportConfig()
}
