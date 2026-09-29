// embedding_recall_test.go — S0-3 真实 corpus 双线召回基准（Go 臂）。
//
// 口径（与 TS 侧 sdk/typescript/src/llm/__tests__/embedding-recall.test.ts 逐位对齐）：
//   - 语料：docs/evals/embedding-corpus-v1.json（S0-2 题面台账注册，sha256 冻结）；
//     CI 回归只跑 visible 子集（holdout=false），holdout 子集留给 S0-3 终验；
//   - 嵌入：internal/llm.LexicalEmbedder（无 key 降级位，回归底档臂；
//     语义臂见 TestEmbeddingCorpusRecallSemantic）；chunk 向量输入 = title + "\n" + text；
//   - 三档固定种子：seed ∈ {7, 8, 9}（v5.1 口径 7+N），种子只影响 HNSW 构建——
//     插入顺序（mulberry32 Fisher-Yates 洗牌）与层级生成随机源（HNSWConfig.Rand）；
//   - recall@10：|HNSW top-10 ∩ gold| / |gold|（gold ≤ 8，见语料生成器 MAX_GOLD）；
//   - 结果写入 bench/results/s0-3-recall-go.json（仅 AP_WRITE_S03_RESULTS=1 时落盘，
//     避免常规 go test 弄脏工作树）；双线对账：node scripts/dual-line-recall-check.mjs。
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// s03RecallSeeds 三档固定种子（v5.1 口径 7+N）。种子只影响 HNSW 构建
// （插入顺序 + 层级随机源），不影响语料与嵌入——与 v5.1 召回门同口径。
var s03RecallSeeds = []uint32{7, 8, 9}

// s03RecallCorpusItem 语料条目（chunk 与 query 的统一形状）。
type s03RecallCorpusItem struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Source  string   `json:"source"`
	Title   string   `json:"title"`
	Text    string   `json:"text"`
	Term    string   `json:"term"`
	Gold    []string `json:"gold"`
	Holdout bool     `json:"holdout"`
}

// 查询条目的文本与 chunk 同用 "text" 字段（query text 解码进 Text）。

type s03RecallCorpus struct {
	Chunks  []s03RecallCorpusItem
	Queries []s03RecallCorpusItem
}

// loadS03Corpus 加载 visible（holdout=false）子集。
func loadS03Corpus(t *testing.T) *s03RecallCorpus {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法获取当前文件路径")
	}
	// internal/memory/ -> internal/ -> github.com/Gleamseekers/AgentPrimordia/ -> 仓库根
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filename))))
	data, err := os.ReadFile(filepath.Join(repoRoot, "docs", "evals", "embedding-corpus-v1.json"))
	if err != nil {
		t.Fatalf("读取语料失败（是否已运行 scripts/gen-embedding-corpus.py 并注册?）: %v", err)
	}
	var items []s03RecallCorpusItem
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("语料解码失败: %v", err)
	}
	corpus := &s03RecallCorpus{}
	for _, it := range items {
		if it.Holdout {
			continue // CI 回归口径：只跑 visible 子集，holdout 留给终验
		}
		switch it.Type {
		case "chunk":
			corpus.Chunks = append(corpus.Chunks, it)
		case "query":
			corpus.Queries = append(corpus.Queries, it)
		}
	}
	if len(corpus.Chunks) == 0 || len(corpus.Queries) == 0 {
		t.Fatalf("visible 子集为空: chunks=%d queries=%d", len(corpus.Chunks), len(corpus.Queries))
	}
	return corpus
}

// s03ChunkEmbedInput 嵌入输入约定（双线一致）：title + 换行 + 正文。
func s03ChunkEmbedInput(c s03RecallCorpusItem) string { return c.Title + "\n" + c.Text }

// s03ShuffledIndices mulberry32 Fisher-Yates：种子决定 chunk 插入顺序（构建顺序臂）。
func s03ShuffledIndices(n int, seed uint32) []int {
	rng := clMulberry32(seed)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j := int(rng() * float64(i+1))
		if j > i {
			j = i
		}
		idx[i], idx[j] = idx[j], idx[i]
	}
	return idx
}

// runS03Recall 执行单臂（给定 EmbeddingProvider）三档种子召回，返回各档 recall@10。
func runS03Recall(t *testing.T, corpus *s03RecallCorpus, emb llm.EmbeddingProvider) []float64 {
	t.Helper()
	ctx := context.Background()

	chunkTexts := make([]string, len(corpus.Chunks))
	for i, c := range corpus.Chunks {
		chunkTexts[i] = s03ChunkEmbedInput(c)
	}
	chunkVecs, err := emb.Embeddings(ctx, chunkTexts)
	if err != nil {
		t.Fatalf("chunk 嵌入失败: %v", err)
	}
	queryVecs, err := emb.Embeddings(ctx, queryTexts(corpus.Queries))
	if err != nil {
		t.Fatalf("查询嵌入失败: %v", err)
	}

	recalls := make([]float64, len(s03RecallSeeds))
	for tier, seed := range s03RecallSeeds {
		// 构建顺序由种子洗牌决定；层级随机源同种子（与 v5.1 用法一致）
		order := s03ShuffledIndices(len(corpus.Chunks), seed)
		idx := NewHNSWIndex(HNSWConfig{
			MaxConnections: 16,
			EfConstruction: 200,
			EfSearch:       50,
			Rand:           clMulberry32(seed),
		})
		for _, i := range order {
			idx.Insert(ctx, corpus.Chunks[i].ID, chunkVecs[i], nil)
		}

		total := 0.0
		for qi, q := range corpus.Queries {
			// recall@10 以语料 gold 为 ground truth（题面口径，非暴力搜索口径；
			// 图质量稳定性由三档种子结果一致性体现——见 s03LexicalFloor 注释）
			results := idx.Search(ctx, queryVecs[qi], 10)
			goldSet := make(map[string]bool, len(q.Gold))
			for _, g := range q.Gold {
				goldSet[g] = true
			}
			hits := 0
			for _, r := range results {
				if goldSet[r.ID] {
					hits++
				}
			}
			total += float64(hits) / float64(len(q.Gold))
		}
		recalls[tier] = total / float64(len(corpus.Queries))
	}
	return recalls
}

func queryTexts(qs []s03RecallCorpusItem) []string {
	out := make([]string, len(qs))
	for i, q := range qs {
		out[i] = q.Text
	}
	return out
}

// s03RecallJSON 结果落盘形状（与 TS 侧 s0-3-recall-ts.json 对账字段一致）。
type s03RecallJSON struct {
	Arm            string             `json:"arm"`
	Semantic       bool               `json:"semantic"`
	Corpus         string             `json:"corpus"`
	QueryScope     string             `json:"query_scope"`
	Seeds          []uint32           `json:"seeds"`
	TopK           int                `json:"topK"`
	Chunks         int                `json:"chunks"`
	Queries        int                `json:"queries"`
	Tiers          []s03RecallTier    `json:"tiers"`
	MeanRecallAt10 float64            `json:"mean_recall_at_10"`
	CacheBaseline  s03RecallCacheBase `json:"cache_baseline"`
}

type s03RecallTier struct {
	Seed       uint32  `json:"seed"`
	RecallAt10 float64 `json:"recall_at_10"`
}

type s03RecallCacheBase struct {
	Hits    int64   `json:"hits"`
	Misses  int64   `json:"misses"`
	HitRate float64 `json:"hit_rate"`
}

// writeS03RecallJSON AP_WRITE_S03_RESULTS=1 时写结果 JSON（供双线对账脚本读取）。
func writeS03RecallJSON(t *testing.T, res s03RecallJSON) {
	t.Helper()
	if os.Getenv("AP_WRITE_S03_RESULTS") != "1" {
		return
	}
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法获取当前文件路径")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filename))))
	name := "s0-3-recall-go.json"
	if res.Semantic {
		name = "s0-3-recall-go-semantic.json" // 语义臂独立落盘，不覆盖 lexical 底档结果
	}
	path := filepath.Join(repoRoot, "agentprimordia", "bench", "results", name)
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatalf("结果序列化失败: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("结果写入失败: %v", err)
	}
	t.Logf("已写入 %s", path)
}

// s03LexicalFloor lexical 臂回归底档阈值。
//
// 得出方式（先测后断言，禁止倒果为因）：2026-08-31 实测（corpus visible 子集
// 136 chunks / 42 queries，三档种子 recall@10 均 = 0.6254——三档一致说明
// HNSW 图在该规模无损耗（v5.1 修复后），底档数字即词面降级位本身的语义质量）。
// 阈值 = floor(实测均值×100)/100 − 0.05 = 0.62 − 0.05 = 0.57。
// Go/TS 双线使用同一常数；跌破即视为算法漂移或语料变更，须排查而非调低阈值。
const s03LexicalFloor = 0.57

// TestEmbeddingCorpusRecall lexical 臂（回归底档）：三档固定种子 recall@10。
func TestEmbeddingCorpusRecall(t *testing.T) {
	corpus := loadS03Corpus(t)

	// 降级位臂必须显式标记 Semantic()=false：该臂数字不进语义验收
	emb := llm.NewLexicalEmbedder()
	if emb.Semantic() {
		t.Fatal("降级位臂 Semantic() 必须为 false")
	}
	ctx := context.Background()

	recalls := runS03Recall(t, corpus, emb)
	mean := 0.0
	for _, r := range recalls {
		mean += r
	}
	mean /= float64(len(recalls))
	for i, r := range recalls {
		t.Logf("tier seed=%d recall@10 = %.4f", s03RecallSeeds[i], r)
	}
	t.Logf("lexical 臂 mean recall@10 = %.4f（三档均值，corpus visible: %d chunks / %d queries）",
		mean, len(corpus.Chunks), len(corpus.Queries))

	// 回归底档阈值（得出口径见 s03LexicalFloor 注释）
	if mean < s03LexicalFloor {
		t.Errorf("lexical 臂 recall@10 = %.4f 低于回归底档 %.2f（算法漂移或语料变更，须排查而非调低阈值）",
			mean, s03LexicalFloor)
	}

	// 语义缓存命中率基线（冷跑 + 暖跑各一轮查询集 → 精确 0.5 的基线口径）
	cache := llm.NewCachedEmbeddingProvider(llm.NewLexicalEmbedder(), 0)
	qtexts := queryTexts(corpus.Queries)
	if _, err := cache.Embeddings(ctx, qtexts); err != nil {
		t.Fatalf("缓存冷跑失败: %v", err)
	}
	if _, err := cache.Embeddings(ctx, qtexts); err != nil {
		t.Fatalf("缓存暖跑失败: %v", err)
	}
	stats := cache.CacheStats()
	if stats.Hits != int64(len(qtexts)) || stats.Misses != int64(len(qtexts)) || stats.HitRate != 0.5 {
		t.Fatalf("命中率基线异常: hits=%d misses=%d rate=%v, want %d/%d/0.5",
			stats.Hits, stats.Misses, stats.HitRate, len(qtexts), len(qtexts))
	}
	t.Logf("语义缓存命中率基线: hits=%d misses=%d hitRate=%.2f（冷+暖双轮口径）",
		stats.Hits, stats.Misses, stats.HitRate)

	writeS03RecallJSON(t, s03RecallJSON{
		Arm:            "lexical-fallback",
		Semantic:       false,
		Corpus:         "docs/evals/embedding-corpus-v1.json",
		QueryScope:     "visible-only",
		Seeds:          s03RecallSeeds,
		TopK:           10,
		Chunks:         len(corpus.Chunks),
		Queries:        len(corpus.Queries),
		Tiers:          s03Tiers(recalls),
		MeanRecallAt10: mean,
		CacheBaseline:  s03RecallCacheBase{Hits: stats.Hits, Misses: stats.Misses, HitRate: stats.HitRate},
	})
}

// s03Tiers 组装档位明细。
func s03Tiers(recalls []float64) []s03RecallTier {
	tiers := make([]s03RecallTier, len(recalls))
	for i, r := range recalls {
		tiers[i] = s03RecallTier{Seed: s03RecallSeeds[i], RecallAt10: r}
	}
	return tiers
}

// TestEmbeddingCorpusRecall_CacheBaseline 语义缓存命中率基线：同一批查询
// 冷跑（全 miss）+ 暖跑（全 hit）后，命中率应 = 0.5 且计数可观测。
func TestEmbeddingCorpusRecall_CacheBaseline(t *testing.T) {
	corpus := loadS03Corpus(t)
	emb := llm.NewCachedEmbeddingProvider(llm.NewLexicalEmbedder(), 0)
	ctx := context.Background()

	texts := queryTexts(corpus.Queries)
	if _, err := emb.Embeddings(ctx, texts); err != nil { // 冷跑
		t.Fatalf("冷跑失败: %v", err)
	}
	if _, err := emb.Embeddings(ctx, texts); err != nil { // 暖跑
		t.Fatalf("暖跑失败: %v", err)
	}
	stats := emb.CacheStats()
	if stats.Hits != int64(len(texts)) || stats.Misses != int64(len(texts)) {
		t.Fatalf("Hits/Misses = %d/%d, want %d/%d",
			stats.Hits, stats.Misses, len(texts), len(texts))
	}
	if math.Abs(stats.HitRate-0.5) > 1e-9 {
		t.Fatalf("命中率 = %v, want 0.5（冷+暖各一轮的基线口径）", stats.HitRate)
	}
	t.Logf("语义缓存命中率基线: hits=%d misses=%d hitRate=%.4f（冷+暖双轮口径）",
		stats.Hits, stats.Misses, stats.HitRate)

	if os.Getenv("AP_WRITE_S03_RESULTS") == "1" {
		// 命中率基线随 lexical 臂结果一并写入（见 TestEmbeddingCorpusRecall）
		_ = fmt.Sprintf("cache baseline: %d/%d", stats.Hits, stats.Misses)
	}
}

// TestEmbeddingCorpusRecallSemantic 语义臂（需真实端点，S0-3 验收 ≥0.95 臂）。
//
// 无 secrets 时降级豁免：打印 A1 运营依赖提示后 skip——绝不伪造 ≥0.95。
// 端点就位时（AP_EMBEDDINGS_BASE_URL 等），同一语料、同一三档种子口径真跑。
func TestEmbeddingCorpusRecallSemantic(t *testing.T) {
	if os.Getenv("AP_EMBEDDINGS_BASE_URL") == "" && os.Getenv("AP_EMBEDDINGS_PROVIDER") == "" {
		t.Skip("A1 运营依赖未就位 → 降级豁免（docs/V7路线图.md §九）：未设置 AP_EMBEDDINGS_BASE_URL/AP_EMBEDDINGS_PROVIDER，语义臂 recall@10 ≥0.95 待端点就位后真跑")
	}
	corpus := loadS03Corpus(t)
	emb, err := llm.NewEmbeddingProviderFromEnv()
	if err != nil {
		t.Fatalf("语义嵌入 Provider 装配失败: %v", err)
	}
	if !emb.Semantic() {
		t.Fatal("语义臂必须使用 Semantic()=true 的 Provider（降级位不得计入语义验收）")
	}
	t.Logf("语义臂端点: model=%s dimension=%d semantic=%v", emb.Model(), emb.Dimension(), emb.Semantic())

	recalls := runS03Recall(t, corpus, emb)
	mean := 0.0
	for _, r := range recalls {
		mean += r
	}
	mean /= float64(len(recalls))
	for i, r := range recalls {
		t.Logf("tier seed=%d recall@10 = %.4f", s03RecallSeeds[i], r)
	}
	if mean < 0.95 {
		t.Errorf("语义臂 mean recall@10 = %.4f < 0.95（S0-3 验收线）", mean)
	}
	writeS03RecallJSON(t, s03RecallJSON{
		Arm:            "semantic",
		Semantic:       true,
		Corpus:         "docs/evals/embedding-corpus-v1.json",
		QueryScope:     "visible-only",
		Seeds:          s03RecallSeeds,
		TopK:           10,
		Chunks:         len(corpus.Chunks),
		Queries:        len(corpus.Queries),
		Tiers:          s03Tiers(recalls),
		MeanRecallAt10: mean,
	})
}
