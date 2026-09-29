package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

func TestPlugin_Name(t *testing.T) {
	p := New()
	if p.Name() == "" {
		t.Fatal("插件名称不能为空")
	}
}

func TestPlugin_Version(t *testing.T) {
	p := New()
	if !strings.HasPrefix(p.Version(), "0.") {
		t.Fatalf("版本必须以 0. 开头，实际 %q", p.Version())
	}
}

func TestPlugin_Init(t *testing.T) {
	p := New()
	if err := p.Init(map[string]any{"api_key": "x"}); err != nil {
		t.Fatalf("Init 返回错误: %v", err)
	}
}

func TestPlugin_Close(t *testing.T) {
	p := New()
	if err := p.Close(); err != nil {
		t.Fatalf("Close 返回错误: %v", err)
	}
}

func TestPlugin_Tools(t *testing.T) {
	p := New()
	if len(p.Tools()) == 0 {
		t.Fatal("插件至少应注册一个工具")
	}
}

func TestPlugin_MatchesToolInterface(t *testing.T) {
	p := New()
	for _, tl := range p.Tools() {
		// 必须实现 tools.Tool 接口
		var _ tools.Tool = tl
		_ = tl.Name()
		_ = tl.Description()
		_ = tl.Parameters()
	}
}

func TestEchoTool_Execute(t *testing.T) {
	tool := NewEchoTool()
	args := json.RawMessage(`{"text":"hello"}`)
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute 返回错误: %v", err)
	}
	if res.IsError {
		t.Fatalf("不应是错误结果: %s", res.Content)
	}
	if !strings.Contains(res.Content, `"echo":"hello"`) {
		t.Fatalf("期望包含 echo=hello, got %s", res.Content)
	}
}

func TestEchoTool_Execute_MissingArg(t *testing.T) {
	tool := NewEchoTool()
	args := json.RawMessage(`{}`)
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatal("缺少 text 参数时应返回错误")
	}
}

func TestEchoTool_Execute_WrongType(t *testing.T) {
	tool := NewEchoTool()
	_, err := tool.Execute(context.Background(), json.RawMessage(`123`))
	if err == nil {
		t.Fatal("非法 JSON 时应返回错误")
	}
}
