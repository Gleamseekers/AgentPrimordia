package llm

import (
	"encoding/json"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/jsonutil"
)

// BenchmarkRequestMarshal_Map 对比 map[string]any 与 typed struct 的序列化性能
func BenchmarkRequestMarshal_Map(b *testing.B) {
	body := map[string]any{
		"model":    "gpt-4o",
		"messages": []map[string]any{{"role": "user", "content": "hello world this is a longer prompt to simulate realistic payload size"}},
		"stream":   true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(body)
	}
}

// BenchmarkRequestMarshal_Struct typed struct 的序列化性能
func BenchmarkRequestMarshal_Struct(b *testing.B) {
	req := openaiChatRequest{
		Model: "gpt-4o",
		Messages: []map[string]any{
			{"role": "user", "content": "hello world this is a longer prompt to simulate realistic payload size"},
		},
		Stream: true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(req)
	}
}

// BenchmarkRequestMarshalPooled 复用 buffer 的 jsonutil 路径
func BenchmarkRequestMarshalPooled(b *testing.B) {
	req := openaiChatRequest{
		Model: "gpt-4o",
		Messages: []map[string]any{
			{"role": "user", "content": "hello world this is a longer prompt to simulate realistic payload size"},
		},
		Stream: true,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = jsonutil.Marshal(req)
	}
}
