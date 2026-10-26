// Package wasm 提供基于 wazero 的 WASM 沙箱执行环境。
package wasm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// SandboxConfig WASM 沙箱配置。
type SandboxConfig struct {
	MaxMemoryPages   int
	MaxExecutionTime time.Duration
	MaxFuel          uint64
	AllowedImports   []string
	EnableDebug      bool
}

// DefaultSandboxConfig 返回默认配置。
//
// AllowedImports 默认空集 = 拒绝一切宿主导入。checkImportWhitelist 以批准集建表
// 后逐条比对，空集下任何导入都命中「未批准的宿主导入」，天然 fail-closed。
//
// 历史矛盾（v7.5.2 整改）：此处曾默认放行 wasi_snapshot_preview1，与
// INV-0 断言 A4「WASI 默认关闭」（wasm/runtime_boundary_test.go，根 wasm 模块
// DefaultConfig().EnableWASI=false）以及根模块 EnhancedSandbox 的默认
// （sandbox_enhanced.go: AllowedImports=[]string{}，注释「默认不允许 WASI imports」）
// 构成两条确定性不变式相互矛盾。统一为 fail-closed：需要 WASI 的工件必须显式
// 设置 AllowedImports，不得由默认配置静默授予。
func DefaultSandboxConfig() SandboxConfig {
	return SandboxConfig{
		MaxMemoryPages:   256,
		MaxExecutionTime: 30 * time.Second,
		MaxFuel:          1_000_000_000,
		AllowedImports:   []string{},
	}
}

// Sandbox WASM 沙箱实例。
type Sandbox struct {
	config  SandboxConfig
	runtime wazero.Runtime
	mu      sync.RWMutex
	modules map[string]wazero.CompiledModule
}

// NewSandbox 创建 WASM 沙箱。
//
// INV-0 断言 A4/A5（提案-code层沙箱受控释放.md §2.2）：
//   - WithCloseOnContextDone(true)：解释器逐指令检查 ctx 取消——
//     MaxExecutionTime 才能真实终止无限循环（零值配置兜底默认开启）；
//   - WithMemoryLimitPages：零值配置兜底 256 页（16MB）。
func NewSandbox(config SandboxConfig) *Sandbox {
	ctx := context.Background()
	if config.MaxMemoryPages <= 0 {
		config.MaxMemoryPages = 256
	}
	if config.MaxExecutionTime <= 0 {
		config.MaxExecutionTime = 30 * time.Second
	}
	rc := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(uint32(config.MaxMemoryPages)).
		WithCloseOnContextDone(true)
	r := wazero.NewRuntimeWithConfig(ctx, rc)
	return &Sandbox{
		config:  config,
		runtime: r,
		modules: make(map[string]wazero.CompiledModule),
	}
}

// Load 加载并编译 WASM 模块（强制导入段白名单——INV-0 断言 A7）。
func (s *Sandbox) Load(name string, wasmBytes []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ctx := context.Background()
	compiled, err := s.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("compile module %s: %w", name, err)
	}
	// INV-0 断言 A7：导入段白名单——编译后枚举 ImportedFunctions 逐条比对；
	// 未显式批准的宿主函数（含未启用 WASI 时的 wasi_snapshot_preview1）即拒绝。
	if err := s.checkImportWhitelist(compiled); err != nil {
		return fmt.Errorf("module %s import whitelist: %w", name, err)
	}
	s.modules[name] = compiled
	return nil
}

// checkImportWhitelist 导入段白名单校验（须持锁；A7 确定性可穷尽——
// 导入段是二进制静态可枚举结构，逐条比对）。
func (s *Sandbox) checkImportWhitelist(compiled wazero.CompiledModule) error {
	approved := make(map[string]bool, len(s.config.AllowedImports))
	for _, mod := range s.config.AllowedImports {
		approved[mod] = true
	}
	for _, imp := range compiled.ImportedFunctions() {
		modName, fnName, _ := imp.Import()
		if !approved[modName] {
			return fmt.Errorf("未批准的宿主导入 %s.%s（批准集 %v）", modName, fnName, s.config.AllowedImports)
		}
	}
	return nil
}

// ImportedFunctions 枚举已加载模块的导入段（审计面）。
func (s *Sandbox) ImportedFunctions(name string) ([]string, error) {
	s.mu.RLock()
	compiled, ok := s.modules[name]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("module %s not found", name)
	}
	var out []string
	for _, imp := range compiled.ImportedFunctions() {
		modName, fnName, _ := imp.Import()
		out = append(out, modName+"."+fnName)
	}
	return out, nil
}

// Execute 执行 WASM 模块的导出函数。
func (s *Sandbox) Execute(ctx context.Context, moduleName, funcName string, args ...uint64) (uint64, error) {
	s.mu.RLock()
	compiled, ok := s.modules[moduleName]
	s.mu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("module %s not found", moduleName)
	}

	// INV-0 断言 A4/A5：执行面强制 MaxExecutionTime（零值配置已在
	// NewSandbox 兜底 30s）——调用方透传的 ctx 不得绕过 CPU 配额。
	if s.config.MaxExecutionTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.MaxExecutionTime)
		defer cancel()
	}
	mod, err := s.runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(moduleName))
	if err != nil {
		return 0, fmt.Errorf("instantiate %s: %w", moduleName, err)
	}
	defer mod.Close(ctx)

	fn := mod.ExportedFunction(funcName)
	if fn == nil {
		return 0, fmt.Errorf("function %s not found in %s", funcName, moduleName)
	}

	results, err := fn.Call(ctx, args...)
	if err != nil {
		return 0, fmt.Errorf("execute %s.%s: %w", moduleName, funcName, err)
	}
	if len(results) > 0 {
		return results[0], nil
	}
	return 0, nil
}

// ExecuteWithMemory 通过内存 API 执行 WASM 工具（V3.1 真实实现）。
//
// 调用协议：
//   - 模块必须导出 alloc(uint64) -> ptr、memory、以及目标函数 (ptr, len) -> (ptr, len)
//   - 宿主通过 alloc 分配内存，写入 JSON 参数，调用函数，读取 JSON 结果
//
// 这是替代旧版 Execute（仅返回 uint64）的生产级实现。
func (s *Sandbox) ExecuteWithMemory(ctx context.Context, moduleName, funcName string, input []byte) ([]byte, error) {
	s.mu.RLock()
	compiled, ok := s.modules[moduleName]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("module %s not found", moduleName)
	}

	// 施加执行超时
	execCtx, cancel := context.WithTimeout(ctx, s.config.MaxExecutionTime)
	defer cancel()

	mod, err := s.runtime.InstantiateModule(execCtx, compiled,
		wazero.NewModuleConfig().WithName(moduleName).
			WithStdout(nil).WithStderr(nil).WithStdin(nil))
	if err != nil {
		return nil, fmt.Errorf("instantiate %s: %w", moduleName, err)
	}
	defer mod.Close(execCtx)

	// 获取导出函数
	fn := mod.ExportedFunction(funcName)
	if fn == nil {
		return nil, fmt.Errorf("function %s not found in %s", funcName, moduleName)
	}
	alloc := mod.ExportedFunction("alloc")
	if alloc == nil {
		return nil, fmt.Errorf("module %s does not export alloc", moduleName)
	}
	mem := mod.ExportedMemory("memory")
	if mem == nil {
		return nil, fmt.Errorf("module %s does not export memory", moduleName)
	}

	// 分配并写入参数
	argLen := uint64(len(input))
	offset, err := alloc.Call(execCtx, argLen)
	if err != nil {
		return nil, fmt.Errorf("alloc in %s: %w", moduleName, err)
	}

	if !mem.Write(uint32(offset[0]), input) {
		return nil, fmt.Errorf("memory write failed in %s", moduleName)
	}

	// 调用工具函数：(ptr, len) -> (ret_ptr, ret_len)
	results, err := fn.Call(execCtx, offset[0], argLen)
	if err != nil {
		return nil, fmt.Errorf("execute %s.%s: %w", moduleName, funcName, err)
	}

	if len(results) < 2 {
		return nil, fmt.Errorf("%s.%s must return (ptr, len), got %d results", moduleName, funcName, len(results))
	}

	retPtr := uint32(results[0])
	retLen := uint32(results[1])

	// 从内存读取结果
	result, ok := mem.Read(retPtr, retLen)
	if !ok {
		return nil, fmt.Errorf("memory read failed in %s (ptr=%d, len=%d)", moduleName, retPtr, retLen)
	}

	// 尝试释放返回的内存（可选）
	if free := mod.ExportedFunction("free"); free != nil {
		_, _ = free.Call(execCtx, uint64(retPtr), uint64(retLen))
	}

	return result, nil
}

// Unload 卸载模块。
func (s *Sandbox) Unload(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.modules, name)
	return nil
}

// Memory 获取模块内存（需要在 Execute 后调用）。
func (s *Sandbox) Memory(moduleName string) api.Memory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return nil // 需要在 Execute 上下文中获取
}

// SetMemoryLimit 设置最大内存页数。
func (s *Sandbox) SetMemoryLimit(pages int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.MaxMemoryPages = pages
}

// SetTimeLimit 设置执行超时。
func (s *Sandbox) SetTimeLimit(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.MaxExecutionTime = d
}

// ListModules 列出已加载模块。
func (s *Sandbox) ListModules() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.modules))
	for name := range s.modules {
		names = append(names, name)
	}
	return names
}

// Close 关闭沙箱。
func (s *Sandbox) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modules = nil
	return s.runtime.Close(context.Background())
}

// AddFuel 添加 CPU 燃料（通过 context 传递）。
func (s *Sandbox) WithFuel(ctx context.Context) context.Context {
	if s.config.MaxFuel > 0 {
		return ctx // wazero fuel 在 runtime 配置中设置
	}
	return ctx
}
