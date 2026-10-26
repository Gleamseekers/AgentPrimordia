package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// ===== Qwen / GLM 共享实现的差分断言 =====
//
// 背景：qwen_provider.go 与 glm_provider.go 曾是两份逐字节相同的实现
// （唯一差异是 6 个厂商常量 + CallTools 支持性），任何一侧的缺陷修复都
// 不会自动同步到另一侧。现将共同逻辑上提为 openAICompatMultimodal。
//
// 本文件的职责是把"两侧必须一致"钉成测试：
//   - TestOpenAICompat_QwenAndGLMIdenticalRequest/StreamRequest：
//     相同输入必须产出逐字节相同的请求体。若有人再次把一侧单独改掉，
//     这两个测试立刻变红——这是去重真正的回归网。
//   - TestOpenAICompat_BothProvidersShareImplementation：
//     反射验证两个 Provider 内嵌同一共享类型，而不是各自一份拷贝。
//   - TestOpenAICompat_ProfileDifferencesPreserved：
//     反向钉住 6 处**应当**不同的厂商常量，防止去重时被误统一。

// captureServer 启动一个记录请求体的 httptest 服务端。
// 每个收到的请求体写入 bodies 通道（容量 4，覆盖非流式 + 流式两次调用）。
func captureServer(t *testing.T, respBody map[string]any) (*httptest.Server, <-chan []byte) {
	t.Helper()
	bodies := make(chan []byte, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取请求体失败: %v", err)
			return
		}
		bodies <- b
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(respBody)
	}))
	t.Cleanup(srv.Close)
	return srv, bodies
}

// diffSampleRequest 差分样本：覆盖纯文本消息、混合多模态消息、
// 空文本 part（应被丢弃）、不支持的音频类型（应被丢弃）、
// URL 图片与 base64 图片两条路径，以及 temperature / max_tokens /
// response_format 三个可选字段。
func diffSampleRequest() *CompletionRequestExt {
	temp := 0.5
	return &CompletionRequestExt{
		Model:          "shared-model",
		Temperature:    &temp,
		MaxTokens:      777,
		ResponseFormat: &ResponseFormat{Type: ResponseFormatJSONObject},
		Messages: []*ChatMessageExt{
			NewUserTextMessage("纯文本消息"),
			NewUserMultimodalMessage(
				NewTextContent("描述这张图片："),
				NewImageB64Content("QUJD", "image/png"),
				NewImageURLContent("https://example.com/a.png"),
				NewTextContent(""),                   // 空文本 part 应被丢弃
				NewAudioContent("QUJD", "audio/mp3"), // 音频暂不支持，应被丢弃
			),
			NewAssistantMessage(""),
		},
	}
}

func chatOKResponse() map[string]any {
	return map[string]any{
		"id":    "chatcmpl-diff",
		"model": "shared-model",
		"choices": []map[string]any{
			{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
		},
		"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12},
	}
}

// TestOpenAICompat_QwenAndGLMIdenticalRequest 非流式路径差分：
// 相同输入下，两个 Provider 发出的 HTTP 请求体必须逐字节相同。
func TestOpenAICompat_QwenAndGLMIdenticalRequest(t *testing.T) {
	qwenSrv, qwenBody := captureServer(t, chatOKResponse())
	glmSrv, glmBody := captureServer(t, chatOKResponse())

	qwen, err := NewQwenProvider(Config{APIKey: "k", BaseURL: qwenSrv.URL, Model: "shared-model"})
	if err != nil {
		t.Fatalf("NewQwenProvider: %v", err)
	}
	qwen.client = qwenSrv.Client()
	glm, err := NewGLMProvider(Config{APIKey: "k", BaseURL: glmSrv.URL, Model: "shared-model"})
	if err != nil {
		t.Fatalf("NewGLMProvider: %v", err)
	}
	glm.client = glmSrv.Client()

	req := diffSampleRequest()
	if _, err := qwen.CompleteMultimodal(context.Background(), req); err != nil {
		t.Fatalf("Qwen CompleteMultimodal: %v", err)
	}
	if _, err := glm.CompleteMultimodal(context.Background(), req); err != nil {
		t.Fatalf("GLM CompleteMultimodal: %v", err)
	}

	gotQwen := <-qwenBody
	gotGLM := <-glmBody
	if string(gotQwen) != string(gotGLM) {
		t.Errorf("Qwen 与 GLM 的请求体不一致（共享实现已被单独改动？）\nqwen: %s\nglm:  %s", gotQwen, gotGLM)
	}

	// 样本自身也要真的覆盖到多模态分支，否则差分是空跑。
	var body struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(gotQwen, &body); err != nil {
		t.Fatalf("解析请求体失败: %v", err)
	}
	if len(body.Messages) != 3 {
		t.Fatalf("消息数 = %d, want 3", len(body.Messages))
	}
	if len(body.Messages[1].Content) == 0 || body.Messages[1].Content[0] != '[' {
		t.Errorf("第二条消息应为 content parts 数组，得到: %s", body.Messages[1].Content)
	}
}

// TestOpenAICompat_QwenAndGLMIdenticalStreamRequest 流式路径差分：
// 流式请求额外带 stream / stream_options，同样必须逐字节一致。
func TestOpenAICompat_QwenAndGLMIdenticalStreamRequest(t *testing.T) {
	qwenSrv, qwenBody := captureServer(t, chatOKResponse())
	glmSrv, glmBody := captureServer(t, chatOKResponse())

	qwen, err := NewQwenProvider(Config{APIKey: "k", BaseURL: qwenSrv.URL, Model: "shared-model"})
	if err != nil {
		t.Fatalf("NewQwenProvider: %v", err)
	}
	qwen.streamClient = qwenSrv.Client()
	glm, err := NewGLMProvider(Config{APIKey: "k", BaseURL: glmSrv.URL, Model: "shared-model"})
	if err != nil {
		t.Fatalf("NewGLMProvider: %v", err)
	}
	glm.streamClient = glmSrv.Client()

	req := diffSampleRequest()
	qwenCh, err := qwen.StreamMultimodal(context.Background(), req)
	if err != nil {
		t.Fatalf("Qwen StreamMultimodal: %v", err)
	}
	for range qwenCh { // 排空通道，确保 goroutine 结束
	}
	glmCh, err := glm.StreamMultimodal(context.Background(), req)
	if err != nil {
		t.Fatalf("GLM StreamMultimodal: %v", err)
	}
	for range glmCh { // 排空通道，确保 goroutine 结束
	}

	gotQwen := <-qwenBody
	gotGLM := <-glmBody
	if string(gotQwen) != string(gotGLM) {
		t.Errorf("流式请求体不一致\nqwen: %s\nglm:  %s", gotQwen, gotGLM)
	}
	var body struct {
		Stream        bool `json:"stream"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	if err := json.Unmarshal(gotQwen, &body); err != nil {
		t.Fatalf("解析请求体失败: %v", err)
	}
	if !body.Stream || !body.StreamOptions.IncludeUsage {
		t.Errorf("流式请求应带 stream=true 与 include_usage=true，得到: %s", gotQwen)
	}
}

// TestOpenAICompat_BothProvidersShareImplementation 结构断言：
// 两个 Provider 必须内嵌**同一个**共享类型。若将来有人把某一侧
// 改回独立实现（复制粘贴），本测试变红。
func TestOpenAICompat_BothProvidersShareImplementation(t *testing.T) {
	qwenType := reflect.TypeOf(QwenProvider{})
	glmType := reflect.TypeOf(GLMProvider{})

	qwenField, ok := qwenType.FieldByName("openAICompatMultimodal")
	if !ok {
		t.Fatalf("QwenProvider 未内嵌 openAICompatMultimodal（共享实现已丢失？）")
	}
	glmField, ok := glmType.FieldByName("openAICompatMultimodal")
	if !ok {
		t.Fatalf("GLMProvider 未内嵌 openAICompatMultimodal（共享实现已丢失？）")
	}
	if !qwenField.Anonymous || !glmField.Anonymous {
		t.Error("openAICompatMultimodal 必须是内嵌字段（匿名），否则方法不会被提升")
	}
	if qwenField.Type != glmField.Type {
		t.Errorf("两侧内嵌类型不同：%v vs %v", qwenField.Type, glmField.Type)
	}

	// 共享方法必须真的被两侧提升出来（否则调用方拿不到实现）。
	for _, name := range []string{"CompleteMultimodal", "StreamMultimodal", "Complete", "Stream", "Info", "InfoExt"} {
		for _, typ := range []reflect.Type{reflect.TypeOf(&QwenProvider{}), reflect.TypeOf(&GLMProvider{})} {
			if _, ok := typ.MethodByName(name); !ok {
				t.Errorf("%s 缺少提升方法 %s", typ.Elem().Name(), name)
			}
		}
	}
}

// TestOpenAICompat_ProfileDifferencesPreserved 反向钉住厂商差异：
// 去重只能统一"行为"，不能统一"配置"。以下 6 项必须保持不同，
// 否则等于把通义千问的限额/默认模型错配到智谱上（或反之）。
func TestOpenAICompat_ProfileDifferencesPreserved(t *testing.T) {
	qwen, err := NewQwenProvider(Config{APIKey: "k"})
	if err != nil {
		t.Fatalf("NewQwenProvider: %v", err)
	}
	glm, err := NewGLMProvider(Config{APIKey: "k"})
	if err != nil {
		t.Fatalf("NewGLMProvider: %v", err)
	}

	if got := qwen.config.BaseURL; got != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Errorf("Qwen 默认 BaseURL = %q", got)
	}
	if got := glm.config.BaseURL; got != "https://open.bigmodel.cn/api/paas/v4" {
		t.Errorf("GLM 默认 BaseURL = %q", got)
	}
	if got := qwen.config.Model; got != "qwen-vl-max-latest" {
		t.Errorf("Qwen 默认 Model = %q", got)
	}
	if got := glm.config.Model; got != "glm-4v-flash" {
		t.Errorf("GLM 默认 Model = %q", got)
	}
	if got := qwen.Info().Provider; got != "qwen" {
		t.Errorf("Qwen Info().Provider = %q", got)
	}
	if got := glm.Info().Provider; got != "zhipu" {
		t.Errorf("GLM Info().Provider = %q", got)
	}
	if got := qwen.Info().MaxContext; got != 32768 {
		t.Errorf("Qwen MaxContext = %d, want 32768", got)
	}
	if got := glm.Info().MaxContext; got != 128000 {
		t.Errorf("GLM MaxContext = %d, want 128000", got)
	}
	if got := qwen.InfoExt().MaxImageSize; got != 10 {
		t.Errorf("Qwen MaxImageSize = %d, want 10", got)
	}
	if got := glm.InfoExt().MaxImageSize; got != 20 {
		t.Errorf("GLM MaxImageSize = %d, want 20", got)
	}

	// 默认 max_tokens 走 resolveMaxTokens 的第三优先级，必须各自不同。
	if got := qwen.resolveMaxTokens(&CompletionRequestExt{}); got != 2000 {
		t.Errorf("Qwen 默认 max_tokens = %d, want 2000", got)
	}
	if got := glm.resolveMaxTokens(&CompletionRequestExt{}); got != 4096 {
		t.Errorf("GLM 默认 max_tokens = %d, want 4096", got)
	}

	// 两侧默认值必须真的不完全相同，否则本测试失去意义。
	if qwen.Info().MaxContext == glm.Info().MaxContext {
		t.Error("Qwen 与 GLM 的 MaxContext 意外相同，差分样本不足")
	}
}

// TestOpenAICompat_ResolveMaxTokensPrecedence 优先级钉住：
// 请求值 > 配置值 > 厂商默认值，两侧行为必须一致。
func TestOpenAICompat_ResolveMaxTokensPrecedence(t *testing.T) {
	qwen, _ := NewQwenProvider(Config{APIKey: "k", MaxTokens: 555})
	glm, _ := NewGLMProvider(Config{APIKey: "k", MaxTokens: 555})

	cases := []struct {
		name string
		req  *CompletionRequestExt
		want int
	}{
		{"请求值优先", &CompletionRequestExt{MaxTokens: 1500}, 1500},
		{"配置值次之", &CompletionRequestExt{}, 555},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := qwen.resolveMaxTokens(tc.req); got != tc.want {
				t.Errorf("Qwen resolveMaxTokens = %d, want %d", got, tc.want)
			}
			if got := glm.resolveMaxTokens(tc.req); got != tc.want {
				t.Errorf("GLM resolveMaxTokens = %d, want %d", got, tc.want)
			}
		})
	}

	// 未配置时落到各自厂商默认值。
	bareQwen, _ := NewQwenProvider(Config{APIKey: "k"})
	bareGLM, _ := NewGLMProvider(Config{APIKey: "k"})
	if got := bareQwen.resolveMaxTokens(&CompletionRequestExt{}); got != 2000 {
		t.Errorf("Qwen 厂商默认 = %d, want 2000", got)
	}
	if got := bareGLM.resolveMaxTokens(&CompletionRequestExt{}); got != 4096 {
		t.Errorf("GLM 厂商默认 = %d, want 4096", got)
	}
}
