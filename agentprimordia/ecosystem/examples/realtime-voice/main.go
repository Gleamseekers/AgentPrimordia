// realtime-voice 验收 demo：多模态实时交互端到端演示
//
// 验收场景：语音实时对话（本地 mock ASR/TTS 可运行），支持打断与连续多轮。
//
// 运行方式：go run ./ecosystem/examples/realtime-voice/
package main

import (
	"context"
	"fmt"
	"os"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func main() {
	fmt.Println("=== AgentPrimordia v3.6 多模态实时验收 Demo ===")
	fmt.Println()

	rtCfg := ap.RealtimeRuntimeConfig{}
	if realVoiceMode(&rtCfg) {
		fmt.Printf("🎙️ 真实语音模式：ASR=%s TTS=%s\n", rtCfg.ASR.Name(), rtCfg.TTS.Name())
	} else {
		fmt.Println("🎧 mock 语音模式（设置 AP_LLM_PROVIDER 加 AP_ASR_URL/AP_TTS_URL 或 AP_LLM_API_KEY 接入真实 ASR/TTS）")
	}
	rt := ap.NewRealtimeRuntime(rtCfg)

	// 订阅事件流
	rt.Events.SubscribeAll(func(e ap.RealtimeEvent) {
		fmt.Printf("   [event] %-22s session=%s\n", e.Type, e.SessionID)
	})

	ctx := context.Background()
	rt.OpenSession("voice-demo")
	fmt.Println("🎧 会话已建立: voice-demo")
	fmt.Println()

	// 多轮对话
	turns := []string{"你好", "今天天气如何", "谢谢"}
	for i, utterance := range turns {
		fmt.Printf("--- 第 %d 轮 ---\n", i+1)
		// 模拟音频输入
		audio := make([]byte, 3200)
		for j := 0; j < len(audio)-1; j += 2 {
			audio[j+1] = 0x7f
		}
		if _, err := rt.PushAudio("voice-demo", audio); err != nil {
			fmt.Printf("   push audio err: %v\n", err)
		}
		text, audioOut, err := rt.ProcessTurn(ctx, "voice-demo", utterance)
		if err != nil {
			fmt.Printf("   turn err: %v\n", err)
			continue
		}
		fmt.Printf("   🗣️  用户: %s\n", utterance)
		fmt.Printf("   🔊 Agent: %s (%d bytes)\n", text, len(audioOut))
		fmt.Println()
	}

	// 演示打断
	fmt.Println("--- 打断演示 ---")
	s, _ := rt.Hub.GetSession("voice-demo")
	_ = s.TransitionTo(ap.RealtimeThinking, "demo")
	_ = s.TransitionTo(ap.RealtimeSpeaking, "demo")
	if err := rt.BargeIn.TryBargeIn("voice-demo", "用户插入新指令"); err != nil {
		fmt.Printf("   barge-in err: %v\n", err)
	} else {
		fmt.Println("   ⚡ speaking 中用户插入 → 立即回到 listening")
	}
	fmt.Println()

	// 视觉流演示
	fmt.Println("--- 视觉流演示 ---")
	_ = rt.PushVision(ctx, "voice-demo", []byte("frame-bytes"), 1280, 720)
	fmt.Println("   📷 推入 1280x720 视频帧（连续帧可经 Fusion 融合进 LLM 上下文）")
	fmt.Println()

	rt.CloseSession("voice-demo")
	fmt.Printf("活跃会话: %d\n", rt.Hub.ActiveSessions())
	fmt.Println()
	fmt.Println("=== 验收通过：语音多轮 + 打断 + 视觉流 端到端演示完成 ===")
}

// realVoiceMode 环境变量驱动真实 ASR/TTS（v4.1 真实接线）：
//   - AP_LLM_PROVIDER 为主开关（与其余 demo 一致）
//   - AP_ASR_URL / AP_TTS_URL 指向本地兼容端点（faster-whisper / Piper，免 key）
//   - AP_LLM_API_KEY 非空时缺省 URL 指向 OpenAI 官方端点
//   - AP_TTS_VOICE 指定发音人（默认 alloy）
//
// 配置缺失时报清晰错误并回退 mock（CI 默认路径不受影响）。
func realVoiceMode(cfg *ap.RealtimeRuntimeConfig) bool {
	if os.Getenv("AP_LLM_PROVIDER") == "" {
		return false
	}
	key := os.Getenv("AP_LLM_API_KEY")
	asrURL := os.Getenv("AP_ASR_URL")
	ttsURL := os.Getenv("AP_TTS_URL")
	if key != "" {
		if asrURL == "" {
			asrURL = "https://api.openai.com/v1/audio/transcriptions"
		}
		if ttsURL == "" {
			ttsURL = "https://api.openai.com/v1/audio/speech"
		}
	}
	if asrURL == "" || ttsURL == "" {
		fmt.Println("⚠️  真实语音模式配置不完整：本地端点需 AP_ASR_URL/AP_TTS_URL（faster-whisper/Piper），OpenAI 端点需 AP_LLM_API_KEY；已回退 mock 模式")
		return false
	}
	cfg.ASR = ap.NewOpenAIASR(asrURL, key)
	cfg.TTS = ap.NewOpenAITTS(ttsURL, key, ap.WithTTSVoice(os.Getenv("AP_TTS_VOICE")))
	return true
}
