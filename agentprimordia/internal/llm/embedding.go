// embedding.go — S0-3 语义原生化：Embedding Provider 抽象与三线实现
// （OpenAI 兼容 / ollama 原生 / 无 key 词面降级位）。
//
// 与既有接口的关系（docs/V7路线图.md §二 S0-3、docs/双线豁免矩阵.md #6）：
//   - types.go 的 Embedder 是历史上散落在 *OpenAIProvider / *OllamaProvider 上的
//     可选最小能力（仅 Embeddings）；
//   - 本文件在其上定义 EmbeddingProvider（内嵌 Embedder，补 Dimension/Model/Semantic），
//     作为 memory 向量路径注入与双线召回基准的统一装配口径；
//   - 既有仅实现 Embedder 的 Provider 可经 NewEmbeddingProviderAdapter 升格；
//   - LexicalEmbedder 是「无 key 降级位」：确定性词面伪嵌入（CJK 字符 bigram +
//     拉丁词 token、sublinear TF、L2 归一化），不是语义嵌入——Semantic() 恒为 false，
//     其结果不得计入任何语义验收数字，仅供未注入真实 Provider 时的检索兜底。
//
// 本文件仅使用标准库；Go 与 TS 侧 sdk/typescript/src/llm/embedding.ts 算法逐位对齐，
// 任何一侧改动必须同步另一侧（双线差 ≤0.02 门依赖逐位一致）。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/jsonutil"
)

// EmbeddingProvider 语义嵌入 Provider 抽象（S0-3 语义原生化）。
//
// 与 Embedder 的关系：Embedder（types.go）仅承诺 Embeddings；本接口内嵌 Embedder
// 并补充三项元信息：
//   - Dimension：向量维度；0 表示「由服务端决定且尚未观测到真实维度」；
//   - Model：嵌入模型标识（降级位返回固定名 lexical-fallback-v1）；
//   - Semantic：是否真实语义嵌入。false 表示无 key 降级位（词面伪嵌入），
//     其输出只可用于兜底检索与回归底档基准，不得用于任何语义验收数字。
type EmbeddingProvider interface {
	Embedder
	// Dimension 返回向量维度；0 表示维度由服务端决定且尚未观测。
	Dimension() int
	// Model 返回嵌入模型标识。
	Model() string
	// Semantic 报告是否为真实语义嵌入；false=无 key 降级位。
	Semantic() bool
}

// EmbeddingConfig 语义嵌入 Provider 配置（OpenAI 兼容与 ollama 原生共用）。
type EmbeddingConfig struct {
	// APIKey Bearer 令牌；ollama 原生端点可留空。
	APIKey string
	// BaseURL 服务根地址。OpenAI 兼容实现默认 https://api.openai.com/v1，
	// 对任何 OpenAI 兼容端点可用（含 ollama 暴露的 /v1/embeddings）；
	// ollama 原生实现默认 http://127.0.0.1:11434（直接写回环 IP，避免容器内解析差异）。
	BaseURL string
	// Model 嵌入模型（如 text-embedding-3-small / bge-m3 / nomic-embed-text）。
	Model string
	// Dimensions 期望输出维度；0 表示由服务端决定。OpenAI 兼容实现会在请求体
	// 携带 dimensions 参数（不支持该参数的端点按 OpenAI 规范应忽略之）。
	Dimensions int
	// Timeout 单次 HTTP 超时；0 取 defaultEmbeddingTimeout。
	Timeout time.Duration
	// Client 自定义 HTTP 客户端（测试注入 httptest.Server 用）；nil 时按 Timeout 构造。
	Client *http.Client
}

const (
	// defaultOpenAIEmbeddingsBaseURL OpenAI 兼容嵌入端点默认根地址。
	defaultOpenAIEmbeddingsBaseURL = "https://api.openai.com/v1"
	// defaultEmbeddingModel 默认嵌入模型，与 openai_provider.go 的
	// defaultOpenAIEmbedModel 口径一致。
	defaultEmbeddingModel = "text-embedding-3-small"
	// ollamaEmbeddingsDefaultBaseURL ollama 原生端点默认地址（回环 IP 直写）。
	ollamaEmbeddingsDefaultBaseURL = "http://127.0.0.1:11434"
	// defaultOllamaEmbeddingModel 默认 ollama 嵌入模型，与 ollama_provider.go
	// 的 defaultOllamaEmbedModel 口径一致。
	defaultOllamaEmbeddingModel = "nomic-embed-text"
	// defaultEmbeddingTimeout 嵌入调用默认超时（嵌入批量较聊天轻，取较短值）。
	defaultEmbeddingTimeout = 60 * time.Second
)

// ===== OpenAI 兼容实现 =====

// OpenAIEmbeddingProvider OpenAI 兼容嵌入实现：POST {base}/embeddings，
// wire format {"model":…, "input":[…], "dimensions":…}（dimensions 仅在显式配置
// 期望维度时携带）。对任何 OpenAI 兼容端点可用（含 ollama 的 /v1/embeddings）。
type OpenAIEmbeddingProvider struct {
	cfg    EmbeddingConfig
	client *http.Client
	// observed 首次成功响应观测到的真实维度（未显式配置 Dimensions 时供 Dimension() 返回）
	observed atomic.Int64
}

// NewOpenAIEmbeddingProvider 创建 OpenAI 兼容嵌入 Provider。
// API key 必填（与 NewOpenAIProvider 同口径；本地无 key 端点请用 ollama 原生实现，
// 或经 NewEmbeddingProviderFromEnv 的 ollama 分支装配）。
func NewOpenAIEmbeddingProvider(cfg EmbeddingConfig) (*OpenAIEmbeddingProvider, error) {
	if cfg.APIKey == "" {
		return nil, ErrAPIKeyRequired
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultOpenAIEmbeddingsBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")
	if cfg.Model == "" {
		cfg.Model = defaultEmbeddingModel
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultEmbeddingTimeout
	}
	client := cfg.Client
	if client == nil {
		client = NewDefaultLLMClient(cfg.Timeout)
	}
	return &OpenAIEmbeddingProvider{cfg: cfg, client: client}, nil
}

// embeddingWireRequest OpenAI /embeddings 请求体（字段与 openai_provider.go 的
// openaiEmbedRequest 对齐，另支持可选 dimensions）。
type embeddingWireRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

// embeddingWireResponse OpenAI /embeddings 响应体。
type embeddingWireResponse struct {
	Model string `json:"model"`
	Data  []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *APIError `json:"error,omitempty"`
}

// Embeddings 批量嵌入（OpenAI wire format）。
func (p *OpenAIEmbeddingProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	req := embeddingWireRequest{Model: p.cfg.Model, Input: texts}
	if p.cfg.Dimensions > 0 {
		req.Dimensions = p.cfg.Dimensions
	}
	raw, err := p.doRequest(ctx, "/embeddings", req)
	if err != nil {
		return nil, err
	}
	var resp embeddingWireResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("%w: embeddings 返回条数 %d ≠ 请求条数 %d",
			ErrResponseParseFailed, len(resp.Data), len(texts))
	}
	// index 容错：规范端点回填与位置一致的 index；部分兼容端点不回填或恒 0，
	// 此时若 index 集合构不成 [0,n) 的排列，按响应顺序对位。
	wellFormed := true
	for i, d := range resp.Data {
		if d.Index != i {
			wellFormed = false
			break
		}
	}
	if !wellFormed {
		seen := make(map[int]bool, len(resp.Data))
		perm := len(resp.Data) > 0
		for _, d := range resp.Data {
			if d.Index < 0 || d.Index >= len(resp.Data) || seen[d.Index] {
				perm = false
				break
			}
			seen[d.Index] = true
		}
		if !perm {
			for i := range resp.Data {
				resp.Data[i].Index = i
			}
		}
	}
	out := make([][]float32, len(texts))
	for _, d := range resp.Data {
		out[d.Index] = d.Embedding
	}
	if p.observed.Load() == 0 && len(out[0]) > 0 {
		p.observed.Store(int64(len(out[0])))
	}
	return out, nil
}

// Dimension 返回配置维度；未配置时返回首次响应观测值（仍未知则为 0）。
func (p *OpenAIEmbeddingProvider) Dimension() int {
	if p.cfg.Dimensions > 0 {
		return p.cfg.Dimensions
	}
	return int(p.observed.Load())
}

// Model 返回嵌入模型名。
func (p *OpenAIEmbeddingProvider) Model() string { return p.cfg.Model }

// Semantic 恒 true：真实语义嵌入。
func (p *OpenAIEmbeddingProvider) Semantic() bool { return true }

func (p *OpenAIEmbeddingProvider) doRequest(ctx context.Context, path string, body any) (json.RawMessage, error) {
	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.cfg.BaseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 与既有 Provider 同口径：携带 Retry-After + 错误分类
		return nil, NewHTTPError("openai-embeddings", resp.StatusCode, respBody, resp.Header)
	}
	return respBody, nil
}

// ===== ollama 原生实现 =====

// /api/embed 批量端点可用性状态：未探测 / 可用 / 不可用（回退逐条）。
const (
	ollamaBatchUnknown int32 = 0
	ollamaBatchOK      int32 = 1
	ollamaBatchNo      int32 = 2
)

// OllamaEmbeddingProvider ollama 原生嵌入实现：优先 POST {base}/api/embed 批量
// （{"model":…,"input":[…]} → {"embeddings":[[…],…]}）；端点不存在（404）时降级
// 逐条 POST {base}/api/embeddings（{"model":…,"prompt":…} → {"embedding":[…]}），
// 与 ollama_provider.go 既有 Embedder 的逐条口径一致。探测结果进程内固化。
type OllamaEmbeddingProvider struct {
	cfg        EmbeddingConfig
	client     *http.Client
	batchState atomic.Int32
	observed   atomic.Int64
}

// NewOllamaEmbeddingProvider 创建 ollama 原生嵌入 Provider（无需 API key）。
func NewOllamaEmbeddingProvider(cfg EmbeddingConfig) (*OllamaEmbeddingProvider, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = ollamaEmbeddingsDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(baseURL, "/")
	if cfg.Model == "" {
		cfg.Model = defaultOllamaEmbeddingModel
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultEmbeddingTimeout
	}
	client := cfg.Client
	if client == nil {
		client = NewDefaultLLMClient(cfg.Timeout)
	}
	return &OllamaEmbeddingProvider{cfg: cfg, client: client}, nil
}

// ollamaEmbedBatchRequest /api/embed 批量请求体。
type ollamaEmbedBatchRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// ollamaEmbedBatchResponse /api/embed 批量响应体。
type ollamaEmbedBatchResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error,omitempty"`
}

// ollamaEmbedOneRequest /api/embeddings 逐条请求体（与 ollama_provider.go 一致）。
type ollamaEmbedOneRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaEmbedOneResponse /api/embeddings 逐条响应体。
type ollamaEmbedOneResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embeddings 批量嵌入（/api/embed 批量优先，404 降级 /api/embeddings 逐条）。
func (p *OllamaEmbeddingProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	// 首次调用探测 /api/embed 可用性并固化结论（进程内记忆，后续调用直达分支）
	if p.batchState.Load() == ollamaBatchUnknown {
		raw, err := p.tryRequest(ctx, "/api/embed", ollamaEmbedBatchRequest{Model: p.cfg.Model, Input: texts})
		switch {
		case err == nil:
			var resp ollamaEmbedBatchResponse
			if jsonErr := json.Unmarshal(raw, &resp); jsonErr != nil {
				return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, jsonErr)
			}
			if resp.Error != "" {
				return nil, fmt.Errorf("%s: %w", resp.Error, ErrLLMCallFailed)
			}
			if len(resp.Embeddings) != len(texts) {
				return nil, fmt.Errorf("%w: /api/embed 返回条数 %d ≠ 请求条数 %d",
					ErrResponseParseFailed, len(resp.Embeddings), len(texts))
			}
			p.batchState.Store(ollamaBatchOK)
			p.recordObserved(resp.Embeddings[0])
			return resp.Embeddings, nil
		case isNotFound(err):
			p.batchState.Store(ollamaBatchNo) // 旧版 ollama 无批量端点 → 固化走逐条
		default:
			return nil, err
		}
	}

	if p.batchState.Load() == ollamaBatchOK {
		raw, err := p.tryRequest(ctx, "/api/embed", ollamaEmbedBatchRequest{Model: p.cfg.Model, Input: texts})
		if err != nil {
			return nil, err
		}
		var resp ollamaEmbedBatchResponse
		if jsonErr := json.Unmarshal(raw, &resp); jsonErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, jsonErr)
		}
		if resp.Error != "" {
			return nil, fmt.Errorf("%s: %w", resp.Error, ErrLLMCallFailed)
		}
		if len(resp.Embeddings) != len(texts) {
			return nil, fmt.Errorf("%w: /api/embed 返回条数 %d ≠ 请求条数 %d",
				ErrResponseParseFailed, len(resp.Embeddings), len(texts))
		}
		p.recordObserved(resp.Embeddings[0])
		return resp.Embeddings, nil
	}

	// 逐条回退（/api/embeddings，与既有 OllamaProvider.Embeddings 同口径）
	out := make([][]float32, len(texts))
	for i, text := range texts {
		raw, err := p.tryRequest(ctx, "/api/embeddings", ollamaEmbedOneRequest{Model: p.cfg.Model, Prompt: text})
		if err != nil {
			return nil, err
		}
		var one ollamaEmbedOneResponse
		if jsonErr := json.Unmarshal(raw, &one); jsonErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, jsonErr)
		}
		out[i] = one.Embedding
	}
	p.recordObserved(out[0])
	return out, nil
}

// Dimension 返回配置维度（ollama 维度由模型决定，通常未显式配置 → 观测值或 0）。
func (p *OllamaEmbeddingProvider) Dimension() int {
	if p.cfg.Dimensions > 0 {
		return p.cfg.Dimensions
	}
	return int(p.observed.Load())
}

// Model 返回嵌入模型名。
func (p *OllamaEmbeddingProvider) Model() string { return p.cfg.Model }

// Semantic 恒 true：真实语义嵌入。
func (p *OllamaEmbeddingProvider) Semantic() bool { return true }

func (p *OllamaEmbeddingProvider) recordObserved(vec []float32) {
	if p.observed.Load() == 0 && len(vec) > 0 {
		p.observed.Store(int64(len(vec)))
	}
}

// isNotFound 判断错误是否为 HTTP 404（用于 /api/embed 可用性探测）。
func isNotFound(err error) bool {
	var re *RetryableError
	if errors.As(err, &re) {
		return re.StatusCode == http.StatusNotFound
	}
	return false
}

func (p *OllamaEmbeddingProvider) tryRequest(ctx context.Context, path string, body any) (json.RawMessage, error) {
	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.cfg.BaseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, NewHTTPError("ollama-embeddings", resp.StatusCode, respBody, resp.Header)
	}
	return respBody, nil
}

// ===== 无 key 降级位（词面伪嵌入）=====

// LexicalEmbedder 是「无 key 降级位」：完全本地、确定性的词面统计伪嵌入，
// 不是语义嵌入（Semantic() 恒 false）。用途边界：
//  1. 未配置 AP_EMBEDDINGS_* 时 memory 向量路径的兜底；
//  2. 双线召回基准的回归底档臂（阈值取自实测，见 bench/results/s0-3-recall-*.json）。
//
// 算法（与 TS 侧 sdk/typescript/src/llm/embedding.ts 的 LexicalEmbedder 逐位对齐，
// 不得单侧改动）：
//  1. 小写化后扫描码点：CJK 连续段产出字符 bigram（段长 1 时产出单字），
//     拉丁字母/数字/下划线连续段产出整词 token；其余码点为分隔符；
//  2. FNV-1a 32 位哈希（token 的 UTF-8 字节）映射到 256 维：idx = h & 0xFF，
//     符号 = (h>>8)&1 ? -1 : +1（符号哈希降低碰撞偏置）；
//  3. 词频权重取 sqrt（sublinear TF 阻尼）——刻意不用 ln：sqrt 为 IEEE 正确舍入、
//     双线逐位一致；ln 依赖各平台 libm 舍入，存在跨语言 1ulp 级偏差风险；
//  4. L2 归一化（sqrt 同上）；空文本/零向量返回全零向量。
//
// 注：小写化对 CJK/ASCII 语料双线一致；非 ASCII 拉丁的大小写映射差异（如土耳其语
// İ）不在本降级位的支持范围。浮点累加按 token 首现顺序进行，保证双线求和次序一致。
const (
	lexicalDim       = 256
	lexicalModelName = "lexical-fallback-v1"
	// FNV-1a 32 位参数
	fnvOffset32 uint32 = 0x811c9dc5
	fnvPrime32  uint32 = 0x01000193
)

// LexicalEmbedder 词面伪嵌入（无 key 降级位）。
type LexicalEmbedder struct{}

// NewLexicalEmbedder 创建降级位嵌入器。
func NewLexicalEmbedder() *LexicalEmbedder { return &LexicalEmbedder{} }

// Embeddings 批量词面伪嵌入（纯本地计算，无网络调用）。
func (LexicalEmbedder) Embeddings(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = lexicalEmbedOne(t)
	}
	return out, nil
}

// Dimension 恒为 lexicalDim。
func (LexicalEmbedder) Dimension() int { return lexicalDim }

// Model 固定标识 lexical-fallback-v1。
func (LexicalEmbedder) Model() string { return lexicalModelName }

// Semantic 恒 false：这是降级位，不是语义嵌入。
func (LexicalEmbedder) Semantic() bool { return false }

// lexicalIsCJK 判断码点是否属 CJK 表意文字区（统一表意/扩展A/兼容区）。
func lexicalIsCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK 统一表意文字
		(r >= 0x3400 && r <= 0x4DBF) || // CJK 扩展 A
		(r >= 0xF900 && r <= 0xFAFF) // CJK 兼容表意文字
}

// lexicalTokenize 分词：CJK 字符 bigram + 拉丁整词（见 LexicalEmbedder 注释）。
func lexicalTokenize(s string) []string {
	lower := strings.ToLower(s)
	var (
		tokens []string
		cjk    []rune
		latin  []rune
	)
	flushLatin := func() {
		if len(latin) > 0 {
			tokens = append(tokens, string(latin))
			latin = latin[:0]
		}
	}
	flushCJK := func() {
		switch len(cjk) {
		case 0:
		case 1: // 单字段产出单字 token
			tokens = append(tokens, string(cjk))
		default: // bigram：相邻两字滑窗
			for i := 0; i+1 < len(cjk); i++ {
				tokens = append(tokens, string(cjk[i:i+2]))
			}
		}
		cjk = cjk[:0]
	}
	for _, r := range lower {
		switch {
		case lexicalIsCJK(r):
			flushLatin()
			cjk = append(cjk, r)
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_':
			flushCJK()
			latin = append(latin, r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return tokens
}

// lexicalFnv1a32 FNV-1a 32 位哈希（按 UTF-8 字节序，与 TS 实现逐位一致）。
func lexicalFnv1a32(s string) uint32 {
	h := fnvOffset32
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= fnvPrime32
	}
	return h
}

// lexicalEmbedOne 单条词面伪嵌入（首现顺序累加 + L2 归一化）。
func lexicalEmbedOne(s string) []float32 {
	counts := make(map[string]int)
	var order []string // token 首现顺序：保证双线浮点求和次序一致
	for _, tok := range lexicalTokenize(s) {
		if _, ok := counts[tok]; !ok {
			order = append(order, tok)
		}
		counts[tok]++
	}
	acc := make([]float64, lexicalDim)
	for _, tok := range order {
		h := lexicalFnv1a32(tok)
		idx := int(h & 0xFF)
		w := math.Sqrt(float64(counts[tok])) // sublinear TF（sqrt 阻尼，见类型注释）
		if (h>>8)&1 == 1 {
			w = -w
		}
		acc[idx] += w
	}
	var norm float64
	for _, v := range acc {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	out := make([]float32, lexicalDim)
	if norm == 0 {
		return out // 空文本 → 全零向量
	}
	for i, v := range acc {
		out[i] = float32(v / norm)
	}
	return out
}

// ===== 适配与环境装配 =====

// EmbeddingProviderAdapter 将既有 Embedder 实现（如 *OpenAIProvider /
// *OllamaProvider 的 Embeddings 方法）升格为 EmbeddingProvider。元信息由调用方
// 声明——尤其是 semantic：传 true 即承诺该 Embedder 连接的是真实语义端点，
// 其结果方可计入语义验收数字。
type EmbeddingProviderAdapter struct {
	inner    Embedder
	model    string
	dim      int
	semantic bool
}

// NewEmbeddingProviderAdapter 升格既有 Embedder。
func NewEmbeddingProviderAdapter(inner Embedder, model string, dimension int, semantic bool) *EmbeddingProviderAdapter {
	return &EmbeddingProviderAdapter{inner: inner, model: model, dim: dimension, semantic: semantic}
}

func (a *EmbeddingProviderAdapter) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	return a.inner.Embeddings(ctx, texts)
}

// Dimension 返回声明维度。
func (a *EmbeddingProviderAdapter) Dimension() int { return a.dim }

// Model 返回声明模型名。
func (a *EmbeddingProviderAdapter) Model() string { return a.model }

// Semantic 返回调用方声明的语义性。
func (a *EmbeddingProviderAdapter) Semantic() bool { return a.semantic }

// NewEmbeddingProviderFromEnv 从 AP_EMBEDDINGS_* 环境变量装配语义嵌入 Provider
// （命名沿用 ConfigFromEnv 的 AP_LLM_* 前缀约定）：
//   - AP_EMBEDDINGS_PROVIDER: openai（默认，OpenAI 兼容 /embeddings）| ollama（原生 /api/embed）；
//   - AP_EMBEDDINGS_API_KEY / AP_EMBEDDINGS_BASE_URL / AP_EMBEDDINGS_MODEL；
//   - AP_EMBEDDINGS_DIMENSIONS: 期望维度（可选，OpenAI 兼容实现随请求携带 dimensions）。
//
// 全部未设置时返回 LexicalEmbedder（无 key 降级位）——「伪向量降为无 key 降级位」
// 的装配语义：不配置即词面兜底，绝不把降级位伪装成语义臂。
// provider=openai 而 API key 缺失时返回 ErrAPIKeyRequired 包装错误。
func NewEmbeddingProviderFromEnv() (EmbeddingProvider, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("AP_EMBEDDINGS_PROVIDER")))
	baseURL := strings.TrimSpace(os.Getenv("AP_EMBEDDINGS_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("AP_EMBEDDINGS_MODEL"))
	apiKey := os.Getenv("AP_EMBEDDINGS_API_KEY")
	dims := envInt("AP_EMBEDDINGS_DIMENSIONS", 0)

	if provider == "" && baseURL == "" && model == "" && apiKey == "" && dims == 0 {
		return NewLexicalEmbedder(), nil // 未配置 → 降级位兜底
	}
	if provider == "" {
		provider = "openai"
	}
	cfg := EmbeddingConfig{APIKey: apiKey, BaseURL: baseURL, Model: model, Dimensions: dims}
	switch provider {
	case "openai":
		p, err := NewOpenAIEmbeddingProvider(cfg)
		if err != nil {
			return nil, fmt.Errorf("装配 AP_EMBEDDINGS openai 兼容嵌入 Provider 失败: %w", err)
		}
		return p, nil
	case "ollama":
		p, err := NewOllamaEmbeddingProvider(cfg)
		if err != nil {
			return nil, fmt.Errorf("装配 AP_EMBEDDINGS ollama 原生嵌入 Provider 失败: %w", err)
		}
		return p, nil
	default:
		return nil, fmt.Errorf("未知 AP_EMBEDDINGS_PROVIDER %q（支持 openai|ollama）", provider)
	}
}

// 编译期接口断言：各实现均满足 EmbeddingProvider。
var (
	_ EmbeddingProvider = (*OpenAIEmbeddingProvider)(nil)
	_ EmbeddingProvider = (*OllamaEmbeddingProvider)(nil)
	_ EmbeddingProvider = (*LexicalEmbedder)(nil)
	_ EmbeddingProvider = (*EmbeddingProviderAdapter)(nil)
)
