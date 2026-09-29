package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

// ToolMetadata WASM 工具元数据
type ToolMetadata struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	// ExecuteFunc 是 WASM 模块中导出的执行函数名
	ExecuteFunc string `json:"execute_func"`
	// Version 模块版本
	Version string `json:"version"`
	// Signature 模块签名字节（INV-0/A6：注册前置——缺签即拒）
	Signature []byte `json:"signature,omitempty"`
	// PublicKey 签名方公钥（ed25519；与 Signature 成对必填）
	PublicKey []byte `json:"public_key,omitempty"`
}

// WASMToolAdapter 将 WASM 模块适配为 tools.Tool 接口
//
// 使用方式：
//
//	sandbox := wasm.NewSandbox(wasm.DefaultSandboxConfig())
//	adapter := wasm.NewWASMToolAdapter(sandbox)
//	err := adapter.RegisterTool(ctx, wasm.ToolMetadata{
//	    Name:        "calculator",
//	    Description: "A simple calculator tool",
//	    Parameters:  json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string"}}}`),
//	    ExecuteFunc: "calculate",
//	    Version:     "1.0.0",
//	}, wasmBytes)
type WASMToolAdapter struct {
	sandbox *Sandbox
	mu      sync.RWMutex
	tools   map[string]*wasmToolEntry
	logger  *slog.Logger
}

type wasmToolEntry struct {
	metadata ToolMetadata
	module   string // 沙箱中的模块名
}

// WASMToolResult WASM 工具执行结果（从 WASM 内存读取）
type WASMToolResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// NewWASMToolAdapter 创建 WASM 工具适配器
func NewWASMToolAdapter(sandbox *Sandbox) *WASMToolAdapter {
	return &WASMToolAdapter{
		sandbox: sandbox,
		tools:   make(map[string]*wasmToolEntry),
		logger:  slog.Default(),
	}
}

// WithLogger 设置日志器
func (a *WASMToolAdapter) WithLogger(logger *slog.Logger) *WASMToolAdapter {
	a.logger = logger
	return a
}

// RegisterTool 注册一个 WASM 工具
//
// 流程：
// 1. 验证元数据
// 2. 将 WASM 字节码加载到沙箱
// 3. 注册工具元数据
func (a *WASMToolAdapter) RegisterTool(ctx context.Context, meta ToolMetadata, wasmBytes []byte) error {
	if meta.Name == "" {
		return fmt.Errorf("wasm: tool name is required")
	}
	if meta.ExecuteFunc == "" {
		return fmt.Errorf("wasm: execute_func is required")
	}

	// INV-0 断言 A6（提案-code层沙箱受控释放.md §2.2）：签名前置——
	// 任何工具进入注册表前，其字节码必须通过签名验证；缺签/验签失败
	// 一律拒绝（确定性可穷尽：篡改任一字节即失败）。
	if len(meta.Signature) == 0 || len(meta.PublicKey) == 0 {
		return fmt.Errorf("wasm: tool %q 注册被拒绝：缺少签名或公钥（A6 签名前置）", meta.Name)
	}
	if err := VerifySignature(wasmBytes, meta.Signature, meta.PublicKey); err != nil {
		return fmt.Errorf("wasm: tool %q 注册被拒绝：验签失败（A6）: %w", meta.Name, err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// 检查是否已注册
	if _, exists := a.tools[meta.Name]; exists {
		return fmt.Errorf("wasm: tool %q already registered", meta.Name)
	}

	// 加载 WASM 模块到沙箱
	moduleName := "tool_" + meta.Name
	if err := a.sandbox.Load(moduleName, wasmBytes); err != nil {
		return fmt.Errorf("wasm: load module for tool %q: %w", meta.Name, err)
	}

	a.tools[meta.Name] = &wasmToolEntry{
		metadata: meta,
		module:   moduleName,
	}

	a.logger.Info("WASM 工具注册成功",
		"name", meta.Name,
		"version", meta.Version,
		"execute_func", meta.ExecuteFunc,
	)

	return nil
}

// UnregisterTool 注销一个 WASM 工具
func (a *WASMToolAdapter) UnregisterTool(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	entry, exists := a.tools[name]
	if !exists {
		return fmt.Errorf("wasm: tool %q not found", name)
	}

	if err := a.sandbox.Unload(entry.module); err != nil {
		return fmt.Errorf("wasm: unload module for tool %q: %w", name, err)
	}

	delete(a.tools, name)
	a.logger.Info("WASM 工具已注销", "name", name)
	return nil
}

// ListTools 列出已注册的 WASM 工具
func (a *WASMToolAdapter) ListTools() []ToolMetadata {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]ToolMetadata, 0, len(a.tools))
	for _, entry := range a.tools {
		result = append(result, entry.metadata)
	}
	return result
}

// GetTool 获取指定工具的元数据
func (a *WASMToolAdapter) GetTool(name string) (ToolMetadata, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	entry, exists := a.tools[name]
	if !exists {
		return ToolMetadata{}, false
	}
	return entry.metadata, true
}

// wasmTool 把已注册的 WASM 工具适配为 tools.Tool 接口（v3.8-3）。
type wasmTool struct {
	adapter *WASMToolAdapter
	name    string
}

// Name 实现 tools.Tool。
func (w *wasmTool) Name() string { return w.name }

// Description 实现 tools.Tool。
func (w *wasmTool) Description() string {
	meta, _ := w.adapter.GetTool(w.name)
	return meta.Description
}

// Parameters 实现 tools.Tool。
func (w *wasmTool) Parameters() json.RawMessage {
	meta, _ := w.adapter.GetTool(w.name)
	return meta.Parameters
}

// Execute 实现 tools.Tool：委托给适配器的 ExecuteTool。
func (w *wasmTool) Execute(ctx context.Context, args json.RawMessage) (*tools.Result, error) {
	res, err := w.adapter.ExecuteTool(ctx, w.name, args)
	if err != nil {
		return &tools.Result{Content: "wasm tool error: " + err.Error(), IsError: true}, nil
	}
	return &tools.Result{Content: res.Content, IsError: res.IsError}, nil
}

// AsTool 返回已注册 WASM 工具的 tools.Tool 适配（v3.8-3）。
// 使自定义 WASM 工具可注册进 tools.Registry，被 ReActAgent 运行时直接调用。
func (a *WASMToolAdapter) AsTool(name string) (tools.Tool, error) {
	if _, ok := a.GetTool(name); !ok {
		return nil, fmt.Errorf("wasm: tool %q not found", name)
	}
	return &wasmTool{adapter: a, name: name}, nil
}

// ExecuteTool 执行 WASM 工具（V3.1 真实内存传递实现）
//
// 流程：
// 1. 查找工具元数据
// 2. 将 JSON 参数序列化
// 3. 通过沙箱内存 API 写入参数并调用导出函数
// 4. 从 WASM 内存读取 JSON 返回值
func (a *WASMToolAdapter) ExecuteTool(ctx context.Context, toolName string, args json.RawMessage) (*WASMToolResult, error) {
	a.mu.RLock()
	entry, exists := a.tools[toolName]
	a.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("wasm: tool %q not found", toolName)
	}

	// 构造输入 JSON（包含工具名和参数）
	input := map[string]any{
		"tool_name": toolName,
		"args":      json.RawMessage(args),
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return &WASMToolResult{
			Content: fmt.Sprintf("marshal input error: %v", err),
			IsError: true,
		}, nil
	}

	// 通过沙箱内存 API 执行（alloc → write → call → read）
	outputBytes, err := a.sandbox.ExecuteWithMemory(ctx, entry.module, entry.metadata.ExecuteFunc, inputBytes)
	if err != nil {
		return &WASMToolResult{
			Content: fmt.Sprintf("execution error: %v", err),
			IsError: true,
		}, nil
	}

	// 解析输出 JSON
	var result WASMToolResult
	if err := json.Unmarshal(outputBytes, &result); err != nil {
		// 输出不是有效 JSON，将原始字节作为内容
		return &WASMToolResult{
			Content: string(outputBytes),
			IsError: false,
		}, nil
	}

	return &result, nil
}

// Close 关闭适配器，卸载所有模块
func (a *WASMToolAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for name, entry := range a.tools {
		_ = a.sandbox.Unload(entry.module)
		delete(a.tools, name)
	}

	a.logger.Info("WASM 工具适配器已关闭")
	return nil
}

// UploadRequest WASM 工具上传请求
type UploadRequest struct {
	ToolName    string          `json:"tool_name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	ExecuteFunc string          `json:"execute_func"`
	Version     string          `json:"version"`
	// WASM 字节码
	WasmBytes []byte `json:"-"`
	// 签名（Ed25519）
	Signature []byte `json:"signature"`
	// 公钥
	PublicKey []byte `json:"public_key"`
	// 最大执行时间
	MaxExecutionTime time.Duration `json:"max_execution_time"`
	// 最大内存页数
	MaxMemoryPages int `json:"max_memory_pages"`
}

// UploadResponse WASM 工具上传响应
type UploadResponse struct {
	Success  bool   `json:"success"`
	ToolName string `json:"tool_name"`
	Message  string `json:"message"`
}

// UploadTool 上传并注册一个 WASM 工具
//
// 流程：
// 1. 验证签名（如果提供）
// 2. 设置沙箱资源限制
// 3. 注册到适配器
func (a *WASMToolAdapter) UploadTool(ctx context.Context, req UploadRequest) (*UploadResponse, error) {
	if req.ToolName == "" {
		return &UploadResponse{Success: false, Message: "tool_name is required"}, nil
	}

	if len(req.WasmBytes) == 0 {
		return &UploadResponse{Success: false, Message: "wasm bytes is empty"}, nil
	}

	// 验证签名（如果提供了签名和公钥）
	if len(req.Signature) > 0 && len(req.PublicKey) > 0 {
		if err := VerifySignature(req.WasmBytes, req.Signature, req.PublicKey); err != nil {
			return &UploadResponse{
				Success:  false,
				ToolName: req.ToolName,
				Message:  fmt.Sprintf("signature verification failed: %v", err),
			}, nil
		}
		a.logger.Info("签名验证通过", "tool", req.ToolName)
	}

	// 设置沙箱资源限制
	if req.MaxMemoryPages > 0 {
		a.sandbox.SetMemoryLimit(req.MaxMemoryPages)
	}
	if req.MaxExecutionTime > 0 {
		a.sandbox.SetTimeLimit(req.MaxExecutionTime)
	}

	// 注册工具
	meta := ToolMetadata{
		Name:        req.ToolName,
		Description: req.Description,
		Parameters:  req.Parameters,
		ExecuteFunc: req.ExecuteFunc,
		Version:     req.Version,
	}

	if err := a.RegisterTool(ctx, meta, req.WasmBytes); err != nil {
		return &UploadResponse{
			Success:  false,
			ToolName: req.ToolName,
			Message:  err.Error(),
		}, nil
	}

	return &UploadResponse{
		Success:  true,
		ToolName: req.ToolName,
		Message:  "tool uploaded and registered successfully",
	}, nil
}
