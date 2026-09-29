// multimodal_test.go — agent 包多模态门面测试。
//
// 2026-09-28 整理：原文件（及 multimodal_adapter_test.go）被 //go:build ignore
// 禁用而腐烂——其测试的 MultimodalMessage/MultimodalAdapter/ContentPart 类型
// 与逻辑已迁入 internal/agent/multimodal 子包，由该包 38 个测试全面覆盖
// （multimodal_test.go / types_test.go / vision_test.go / tools_test.go）。
// 此处仅保留 agent 包独有的门面覆盖：UserMultimodalMessage / UserImageMessage
// 的 multimodal.Message → agent.Message 转换逻辑，及 IsMultimodalProvider 委托。
package agent

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
)

func TestUserMultimodalMessage(t *testing.T) {
	msg := UserMultimodalMessage(
		ContentPart{Type: "text", Text: "what is this?"},
		ContentPart{Type: "image_url", URL: "https://example.com/img.png"},
	)

	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want %q", msg.Role, RoleUser)
	}
	if !msg.HasMultimodal() {
		t.Error("should be multimodal")
	}
	if len(msg.ContentParts) != 2 {
		t.Errorf("len(ContentParts) = %d, want 2", len(msg.ContentParts))
	}
}

func TestUserImageMessage(t *testing.T) {
	msg := UserImageMessage("describe this image", "https://example.com/photo.jpg")

	if msg.Role != RoleUser {
		t.Errorf("Role = %q, want %q", msg.Role, RoleUser)
	}
	if !msg.HasMultimodal() {
		t.Error("should be multimodal")
	}
	if msg.TextContent() != "describe this image" {
		t.Errorf("TextContent() = %q, want %q", msg.TextContent(), "describe this image")
	}
}

// TestIsMultimodalProvider_Delegate agent 门面委托 multimodal 包判定。
func TestIsMultimodalProvider_Delegate(t *testing.T) {
	if IsMultimodalProvider(nil) {
		t.Error("nil provider 不应判定为多模态")
	}
	// DemoProvider 无多模态能力，不应判定为多模态
	if IsMultimodalProvider(llm.NewDemoProvider()) {
		t.Error("DemoProvider 不应判定为多模态")
	}
}
