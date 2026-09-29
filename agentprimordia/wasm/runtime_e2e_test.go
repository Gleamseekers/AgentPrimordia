// runtime_e2e_test.go — v3.8-3 WASM 工具运行时端到端
// 用真实 WASM 模块（wazero 执行）验证：自定义 WASM 工具可注册进
// tools.Registry 并被 ReActAgent 运行时调用（AsTool 桥）。
package wasm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// validTestWASM 构造一个最小有效 WASM 模块：
//
//	(module
//	  (memory (export "memory") 1)
//	  (func (export "alloc") (param i32) (result i32) i32.const 16)
//	  (func (export "tool_execute") (param i32 i32) (result i32 i32)
//	    i32.const 0 i32.const 2)
//	  (func (export "free") (param i32 i32)))
func validTestWASM() []byte {
	// magic + version
	b := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	// type section (id=1)
	b = append(b, 0x01, 0x12,
		0x03,
		0x60, 0x02, 0x7f, 0x7f, 0x02, 0x7f, 0x7f, // type0: (i32,i32)->(i32,i32)
		0x60, 0x01, 0x7f, 0x01, 0x7f, // type1: (i32)->(i32)
		0x60, 0x02, 0x7f, 0x7f, 0x00, // type2: (i32,i32)->()
	)
	// function section (id=3): alloc->type1, tool_execute->type0, free->type2
	b = append(b, 0x03, 0x04, 0x03, 0x01, 0x00, 0x02)
	// memory section (id=5): 1 memory, min 1 page
	b = append(b, 0x05, 0x03, 0x01, 0x00, 0x01)
	// export section (id=7)
	b = append(b, 0x07, 0x28, 0x04,
		0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00, // memory
		0x05, 'a', 'l', 'l', 'o', 'c', 0x00, 0x00, // alloc -> func 0
		0x0c, 't', 'o', 'o', 'l', '_', 'e', 'x', 'e', 'c', 'u', 't', 'e', 0x00, 0x01, // tool_execute -> func 1
		0x04, 'f', 'r', 'e', 'e', 0x00, 0x02, // free -> func 2
	)
	// code section (id=10)
	b = append(b, 0x0a, 0x10, 0x03,
		0x04, 0x00, 0x41, 0x10, 0x0b, // alloc: i32.const 16, end
		0x06, 0x00, 0x41, 0x00, 0x41, 0x02, 0x0b, // tool_execute: i32.const 0, i32.const 2, end
		0x02, 0x00, 0x0b, // free: end
	)
	return b
}

// TestWASM_RuntimeExecution 验证真实 WASM 模块在 wazero 沙箱中可执行。
func TestWASM_RuntimeExecution(t *testing.T) {
	sandbox := NewSandbox(DefaultSandboxConfig())
	defer sandbox.Close()

	adapter := NewWASMToolAdapter(sandbox)
	err := adapter.RegisterTool(context.Background(), signedFor(t, ToolMetadata{
		Name:        "calculator",
		Description: "A WASM calculator",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		ExecuteFunc: "tool_execute",
		Version:     "1.0.0",
	}, validTestWASM()), validTestWASM())
	if err != nil {
		t.Fatalf("注册 WASM 工具失败: %v", err)
	}

	res, err := adapter.ExecuteTool(context.Background(), "calculator", json.RawMessage(`{"expression":"1+1"}`))
	if err != nil {
		t.Fatalf("ExecuteTool 失败: %v", err)
	}
	if res.IsError {
		t.Errorf("WASM 工具执行应成功, got %q", res.Content)
	}
}

// TestWASM_ToolRegistryIntegration 验证 WASM 工具经 AsTool 注册进 tools.Registry 可被调用。
func TestWASM_ToolRegistryIntegration(t *testing.T) {
	sandbox := NewSandbox(DefaultSandboxConfig())
	defer sandbox.Close()

	adapter := NewWASMToolAdapter(sandbox)
	err := adapter.RegisterTool(context.Background(), signedFor(t, ToolMetadata{
		Name:        "wasm-tool",
		Description: "WASM runtime tool",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"x":{"type":"number"}}}`),
		ExecuteFunc: "tool_execute",
		Version:     "1.0.0",
	}, validTestWASM()), validTestWASM())
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	// AsTool → tools.Tool
	wt, err := adapter.AsTool("wasm-tool")
	if err != nil {
		t.Fatalf("AsTool 失败: %v", err)
	}
	if wt.Name() != "wasm-tool" {
		t.Errorf("Name = %q", wt.Name())
	}
	if wt.Description() != "WASM runtime tool" {
		t.Errorf("Description = %q", wt.Description())
	}

	// 注册进 tools.Registry 并执行（等价于 ReActAgent toolkit 调用路径）
	reg := tools.NewRegistry()
	if err := reg.Register(wt); err != nil {
		t.Fatalf("registry.Register 失败: %v", err)
	}
	tool, ok := reg.Get("wasm-tool")
	if !ok {
		t.Fatal("registry 未找到 wasm-tool")
	}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatalf("registry.Execute 失败: %v", err)
	}
	if result.IsError {
		t.Errorf("WASM 工具经 registry 执行应成功, got %q", result.Content)
	}
}

// TestWASM_AsToolNotFound 未注册工具 AsTool 返回错误。
func TestWASM_AsToolNotFound(t *testing.T) {
	sandbox := NewSandbox(DefaultSandboxConfig())
	defer sandbox.Close()
	adapter := NewWASMToolAdapter(sandbox)
	if _, err := adapter.AsTool("nope"); err == nil {
		t.Error("未注册工具 AsTool 应报错")
	}
}

// TestWASM_InvalidModule 无效 WASM 字节码注册应失败。
func TestWASM_InvalidModule(t *testing.T) {
	sandbox := NewSandbox(DefaultSandboxConfig())
	defer sandbox.Close()
	adapter := NewWASMToolAdapter(sandbox)
	err := adapter.RegisterTool(context.Background(), signedFor(t, ToolMetadata{
		Name:        "bad",
		ExecuteFunc: "x",
	}, []byte("not wasm")), []byte("not wasm"))
	if err == nil {
		t.Error("无效 WASM 字节码注册应失败")
	}
}
