// Stability: Experimental — v3.0.0 新增 WASM 沙箱tool能力，API 可能随运行时演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/wasm"
)

// ===== WASM 沙箱 =====

// WASMSandbox WASM 沙箱实例，基于 wazero 运行时
// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type WASMSandbox = wasm.Sandbox

// WASMSandboxConfig WASM 沙箱配置
// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type WASMSandboxConfig = wasm.SandboxConfig

var (
	// NewWASMSandbox 创建 WASM 沙箱
	// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewWASMSandbox = wasm.NewSandbox
	// DefaultWASMSandboxConfig 返回默认沙箱配置
	// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	DefaultWASMSandboxConfig = wasm.DefaultSandboxConfig
)

// ===== WASM tool适配器 =====

// WASMToolAdapter 将 WASM 模块适配为tool接口
// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type WASMToolAdapter = wasm.WASMToolAdapter

// WASMToolMetadata WASM tool元数据
// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type WASMToolMetadata = wasm.ToolMetadata

// WASMToolResult WASM tool执行结果
// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type WASMToolResult = wasm.WASMToolResult

// WASMUploadRequest WASM tool上传请求
// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type WASMUploadRequest = wasm.UploadRequest

// WASMUploadResponse WASM tool上传响应
// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type WASMUploadResponse = wasm.UploadResponse

var (
	// NewWASMToolAdapter 创建 WASM tool适配器
	// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewWASMToolAdapter = wasm.NewWASMToolAdapter
)

// ===== WASM 签名验证 =====

var (
	// WASMVerifySignature 验证 WASM 字节码的 Ed25519 签名
	// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	WASMVerifySignature = wasm.VerifySignature
	// WASMSignWASM 使用 Ed25519 私钥对 WASM 字节码签名
	// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	WASMSignWASM = wasm.SignWASM
	// WASMGenerateKeyPair 生成 Ed25519 密钥对
	// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	WASMGenerateKeyPair = wasm.GenerateKeyPair
	// WASMKeyFingerprint 计算公钥的指纹
	// Deprecated: 预留 API：仓内唯一消费者是 ecosystem/examples/tool-intelligence 演示（3 处引用），无外部采用证据，v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	WASMKeyFingerprint = wasm.KeyFingerprint
)
