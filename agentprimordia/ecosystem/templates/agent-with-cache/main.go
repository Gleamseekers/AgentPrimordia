// {{.ProjectName}} — Agent with cache template
// 展示如何在 Agent 上启用 LLM 响应缓存，避免重复 LLM 调用。
//
// 前置条件：
//
//	set AP_LLM_API_KEY=sk-xxx
//
// 跑法：
//
//	cd {{.ProjectName}}
//	go mod tidy
//	go run .
package main

import (
	"context"
	"fmt"
	"log"

	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func main() {
	cfg := ap.ConfigFromEnv("")
	if cfg.APIKey == "" {
		log.Fatal("set AP_LLM_API_KEY env var")
	}
	provider, err := ap.NewOpenAIProvider(cfg)
	if err != nil {
		log.Fatalf("create provider failed: %v", err)
	}

	// 启用 LLM 缓存（简化配置：仅 maxSize + minScore，无 embedding）
	cache := ap.NewInMemoryCacheWithFullConfig(ap.InMemoryCacheFullConfig{
		MaxSize:  1000,
		MinScore: 0.8,
	})

	agent, err := ap.NewAgent("{{.ProjectName}}", "you are a helpful assistant", provider,
		ap.WithMaxTurns(10),
		ap.WithCache(cache),
	)
	if err != nil {
		log.Fatalf("create agent failed: %v", err)
	}

	// 第一次调用 — 真实请求 LLM
	fmt.Println("=== First call ===")
	resp1, err := agent.Run(context.Background(),
		ap.UserMessage("用一句话介绍 Go 语言"))
	if err != nil {
		log.Fatalf("call failed: %v", err)
	}
	fmt.Printf("Reply: %s\n", resp1.Content)

	// 第二次相同调用 — 应命中缓存
	fmt.Println("\n=== Second call ===")
	resp2, err := agent.Run(context.Background(),
		ap.UserMessage("用一句话介绍 Go 语言"))
	if err != nil {
		log.Fatalf("call failed: %v", err)
	}
	fmt.Printf("Reply: %s\n", resp2.Content)

	// 打印缓存统计
	stats := cache.Stats(context.Background())
	fmt.Printf("\nCache stats: queries=%d, hits=%d, misses=%d, hit_rate=%.1f%%\n",
		stats.TotalQueries, stats.CacheHits, stats.CacheMisses,
		stats.HitRate*100)
}
