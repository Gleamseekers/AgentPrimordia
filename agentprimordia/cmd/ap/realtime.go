package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/realtime"
)

const realtimeUsage = `Usage: ap realtime <subcommand> [arguments]

Subcommands:
  voice    语音会话（默认本地 mock 可跑；--asr/--tts 接入真实服务）

Examples:
  ap realtime voice
  ap realtime voice --asr=openai --tts=openai --asr-url=http://127.0.0.1:9000/v1/audio/transcriptions --tts-url=http://127.0.0.1:5002
  ap realtime voice --asr=openai --tts=openai --asr-url=https://api.openai.com/v1/audio/transcriptions --tts-url=https://api.openai.com/v1/audio/speech --tts-voice=nova
`

// openAIEndpoint 判断 URL 是否为 OpenAI 官方端点（需要 API Key）。
func openAIEndpoint(url string) bool {
	return strings.Contains(url, "api.openai.com")
}

// buildRealtimeAdapters 按 CLI 旗标装配真实/模拟 ASR、TTS 适配器。
//
// mock（默认）→ nil（Runtime 自动回退 MockASR/MockTTS）；
// openai → 真实 HTTP 适配器，URL 必填；OpenAI 官方端点且缺 Key 时报错。
func buildRealtimeAdapters(asr, tts, asrURL, ttsURL, voice string) (realtime.ASRAdapter, realtime.TTSAdapter, error) {
	apiKey := os.Getenv("AP_OPENAI_API_KEY")

	var asrAdapter realtime.ASRAdapter
	switch asr {
	case "", "mock":
		// 保持 mock
	case "openai":
		if asrURL == "" {
			return nil, nil, fmt.Errorf("--asr=openai 需要 --asr-url（本地 faster-whisper/whisper.cpp 兼容端点，或 OpenAI 端点）；改用默认 mock 运行：ap realtime voice")
		}
		if openAIEndpoint(asrURL) && apiKey == "" {
			return nil, nil, fmt.Errorf("--asr-url 指向 api.openai.com 但缺少 API Key：请设置环境变量 AP_OPENAI_API_KEY，或改用本地兼容服务（如 faster-whisper）；mock 用法：ap realtime voice")
		}
		asrAdapter = realtime.NewOpenAIASR(asrURL, apiKey)
	default:
		return nil, nil, fmt.Errorf("unknown ASR adapter %q（支持 mock | openai）", asr)
	}

	var ttsAdapter realtime.TTSAdapter
	switch tts {
	case "", "mock":
		// 保持 mock
	case "openai":
		if ttsURL == "" {
			return nil, nil, fmt.Errorf("--tts=openai 需要 --tts-url（本地 Piper/edge-tts 兼容端点，或 OpenAI 端点）；改用默认 mock 运行：ap realtime voice")
		}
		if openAIEndpoint(ttsURL) && apiKey == "" {
			return nil, nil, fmt.Errorf("--tts-url 指向 api.openai.com 但缺少 API Key：请设置环境变量 AP_OPENAI_API_KEY，或改用本地兼容服务（如 Piper）；mock 用法：ap realtime voice")
		}
		ttsAdapter = realtime.NewOpenAITTS(ttsURL, apiKey, realtime.WithTTSVoice(voice))
	default:
		return nil, nil, fmt.Errorf("unknown TTS adapter %q（支持 mock | openai）", tts)
	}

	return asrAdapter, ttsAdapter, nil
}

func runRealtime(args []string) error {
	if len(args) == 0 {
		fmt.Print(realtimeUsage)
		return nil
	}

	sub := args[0]
	switch sub {
	case "voice":
		return runRealtimeVoice(args[1:])
	case "--help", "-h", "help":
		fmt.Print(realtimeUsage)
		return nil
	default:
		return fmt.Errorf("unknown realtime subcommand %q, run \"ap realtime --help\"", sub)
	}
}

func runRealtimeVoice(args []string) error {
	fs := flag.NewFlagSet("ap realtime voice", flag.ContinueOnError)
	asr := fs.String("asr", "mock", "ASR 适配器：mock（默认）| openai")
	tts := fs.String("tts", "mock", "TTS 适配器：mock（默认）| openai")
	asrURL := fs.String("asr-url", "", "ASR 端点（--asr=openai 必填）")
	ttsURL := fs.String("tts-url", "", "TTS 端点（--tts=openai 必填）")
	voice := fs.String("tts-voice", "alloy", "TTS 发音人（默认 alloy）")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), realtimeUsage)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	asrAdapter, ttsAdapter, err := buildRealtimeAdapters(*asr, *tts, *asrURL, *ttsURL, *voice)
	if err != nil {
		return err
	}

	mode := "mock"
	if asrAdapter != nil || ttsAdapter != nil {
		mode = "real"
	}
	fmt.Printf("🎧 语音会话（%s 模式）\n", mode)

	rt := realtime.NewRuntime(realtime.RuntimeConfig{ASR: asrAdapter, TTS: ttsAdapter})
	ctx := context.Background()
	rt.OpenSession("voice-1")

	// 模拟一轮：推入音频 → 处理轮次
	audio := make([]byte, 3200)
	for i := 0; i < len(audio)-1; i += 2 {
		audio[i+1] = 0x7f
	}
	if _, err := rt.PushAudio("voice-1", audio); err != nil {
		return fmt.Errorf("push audio: %w", err)
	}

	text, audioOut, err := rt.ProcessTurn(ctx, "voice-1", "今天天气怎么样")
	if err != nil {
		return fmt.Errorf("process turn: %w", err)
	}
	fmt.Printf("   🗣️  用户: 今天天气怎么样\n")
	fmt.Printf("   🔊 Agent: %s (%d bytes 音频)\n", text, len(audioOut))

	// 演示打断
	s, _ := rt.Hub.GetSession("voice-1")
	_ = s.TransitionTo(realtime.SessionThinking, "demo")
	_ = s.TransitionTo(realtime.SessionSpeaking, "demo")
	if err := rt.BargeIn.TryBargeIn("voice-1", "用户插入"); err != nil {
		return fmt.Errorf("barge-in: %w", err)
	}
	fmt.Println("   ⚡ 打断成功 → 回到 listening")
	if mode == "real" {
		fmt.Printf("   提示: ASR=%s TTS=%s\n", asrAdapter.Name(), ttsAdapter.Name())
	} else {
		fmt.Println("   提示: 接入真实 ASR/TTS：ap realtime voice --asr=openai --tts=openai --asr-url=<端点> --tts-url=<端点>（详见 ap realtime --help）")
	}
	return nil
}
