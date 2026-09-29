// Package agent - discovery 类型别名
// 保持向后兼容，实际实现已迁移到 discovery 子包
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/discovery"
)

// AgentInfo 表示 Agent 的注册信息
type AgentInfo = discovery.AgentInfo

// Discovery 服务发现接口
type Discovery = discovery.Discovery

// LocalDiscovery 本地内存实现
type LocalDiscovery = discovery.LocalDiscovery

// HTTPDiscoveryClient HTTP 客户端实现
type HTTPDiscoveryClient = discovery.HTTPDiscoveryClient

// DiscoveryServer 发现服务 HTTP 服务器
type DiscoveryServer = discovery.DiscoveryServer

// NewLocalDiscovery 创建本地发现服务
func NewLocalDiscovery() *LocalDiscovery {
	return discovery.NewLocalDiscovery()
}

// NewHTTPDiscoveryClient 创建 HTTP 发现客户端
func NewHTTPDiscoveryClient(baseURL string) *HTTPDiscoveryClient {
	return discovery.NewHTTPDiscoveryClient(baseURL)
}

// NewDiscoveryServer 创建发现服务服务器
func NewDiscoveryServer(d Discovery, addr string) *DiscoveryServer {
	return discovery.NewDiscoveryServer(d, addr)
}
