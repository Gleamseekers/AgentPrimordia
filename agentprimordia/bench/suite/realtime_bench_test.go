package suite

// v3.6 实时会话基准测试
//
// 测量实时核心路径性能：
//   - 会话建立延迟
//   - 打断响应延迟
//   - 音频流推入吞吐

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/realtime"
)

// BenchmarkRealtimeSessionCreate 会话建立延迟
func BenchmarkRealtimeSessionCreate(b *testing.B) {
	rt := realtime.NewRuntime(realtime.RuntimeConfig{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt.OpenSession("bench-s")
		rt.CloseSession("bench-s")
	}
}

// BenchmarkRealtimeBargeIn 打断响应延迟
func BenchmarkRealtimeBargeIn(b *testing.B) {
	rt := realtime.NewRuntime(realtime.RuntimeConfig{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := rt.OpenSession("bench-bi")
		// 走到 speaking 状态
		_ = s.TransitionTo(realtime.SessionListening, "x")
		_ = s.TransitionTo(realtime.SessionThinking, "x")
		_ = s.TransitionTo(realtime.SessionSpeaking, "x")
		_ = rt.BargeIn.TryBargeIn("bench-bi", "bench")
		rt.CloseSession("bench-bi")
	}
}

// BenchmarkRealtimePushAudio 音频流推入吞吐
func BenchmarkRealtimePushAudio(b *testing.B) {
	rt := realtime.NewRuntime(realtime.RuntimeConfig{})
	rt.OpenSession("bench-a")
	data := make([]byte, 3200)
	for i := 0; i < len(data)-1; i += 2 {
		data[i+1] = 0x7f
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rt.PushAudio("bench-a", data)
	}
}

// BenchmarkRealtimeProcessTurn 实时交互轮次延迟（echo 模式）
func BenchmarkRealtimeProcessTurn(b *testing.B) {
	rt := realtime.NewRuntime(realtime.RuntimeConfig{})
	rt.OpenSession("bench-t")
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = rt.ProcessTurn(ctx, "bench-t", "hi")
	}
}
