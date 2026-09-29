package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/cmd/example/demo"
	ap "github.com/Gleamseekers/AgentPrimordia/pkg"
)

func main() {
	fmt.Println("💾 Memory 多后端支持示例")
	fmt.Println("=" + string(make([]byte, 50)))
	fmt.Println()

	demonstrateInMemoryBackend()
	demonstrateSQLiteBackend()
	demonstrateFactoryPattern()
}

func demonstrateInMemoryBackend() {
	fmt.Println("🧠 InMemory 后端演示")
	fmt.Println("-" + string(make([]byte, 40)))

	store := ap.NewInMemoryStore()
	defer store.Close()

	ctx := context.Background()

	episode1 := &ap.Episode{
		ID:        "mem-001",
		SessionID: "session-test",
		Role:      "user",
		Content:   "用户的第一条消息",
		CreatedAt: time.Now().Format(time.RFC3339),
	}

	if err := store.Add(ctx, episode1); err != nil {
		log.Printf("❌ 添加失败: %v", err)
		return
	}
	fmt.Println("✅ 成功添加记忆到 InMemory 后端")

	retrieved, err := store.Get(ctx, "mem-001")
	if err != nil {
		log.Printf("❌ 获取失败: %v", err)
		return
	}
	fmt.Printf("   📖 获取内容: %s\n", retrieved.Content)

	count, _ := store.Count(ctx, "")
	fmt.Printf("   📊 总记忆数: %d\n", count)
	fmt.Println()
}

func demonstrateSQLiteBackend() {
	fmt.Println("🗄️  SQLite 后端演示")
	fmt.Println("-" + string(make([]byte, 40)))

	tmpDir := "./temp_memory_test"
	store, err := ap.NewSQLiteStore(tmpDir + "/test.db")
	if err != nil {
		log.Printf("❌ 创建 SQLite 后端失败: %v", err)
		return
	}
	defer store.Close()

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		episode := &ap.Episode{
			ID:        fmt.Sprintf("sqlite-%03d", i+1),
			SessionID: fmt.Sprintf("session-%d", i%2+1),
			Role:      "user",
			Content:   fmt.Sprintf("这是第 %d 条测试消息", i+1),
			Topics:    "测试,示例",
			CreatedAt: time.Now().Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
		}

		if err := store.Add(ctx, episode); err != nil {
			log.Printf("❌ 添加第 %d 条失败: %v", i+1, err)
			continue
		}
	}
	fmt.Println("✅ 成功添加 5 条记忆到 SQLite 后端")

	stats, _ := store.Stats(ctx)
	fmt.Printf("   📊 统计信息:\n")
	fmt.Printf("      总条目数: %d\n", stats.TotalEpisodes)
	fmt.Printf("      会话数: %d\n", stats.TotalSessions)

	results, _ := store.Search(ctx, "测试", &ap.SearchOptions{Limit: 3})
	fmt.Printf("   🔍 搜索 '测试' 找到 %d 条结果\n", len(results))

	timeline, _ := store.GetMemoryTimeline(ctx, 7)
	fmt.Printf("   📅 最近7天时间线: %d 个分组\n", len(timeline))
	fmt.Println()
}

func demonstrateFactoryPattern() {
	fmt.Println("🏭 工厂模式演示（推荐）")
	fmt.Println("-" + string(make([]byte, 40)))

	configs := []struct {
		name     string
		cfg      ap.StoreConfig
		expected string
	}{
		{
			name: "SQLite 后端",
			cfg: ap.StoreConfig{
				Type: ap.BackendSQLite,
				Path: "./factory_test.db",
			},
			expected: "*memory.SQLiteStore",
		},
		{
			name: "InMemory 后端",
			cfg: ap.StoreConfig{
				Type: ap.BackendMemory,
			},
			expected: "*memory.InMemoryStore",
		},
	}

	for _, tc := range configs {
		mem, err := ap.NewMemory(tc.cfg)
		if err != nil {
			log.Printf("❌ 创建 %s 失败: %v", tc.name, err)
			continue
		}
		defer mem.Close()

		actualType := fmt.Sprintf("%T", mem)
		status := "✅"
		if actualType != tc.expected {
			status = "⚠️ "
		}
		fmt.Printf("%s %-15s → 类型: %s\n", status, tc.name, actualType)
	}

	fmt.Println()
	fmt.Println("💡 使用建议:")
	fmt.Println("   - 开发/测试环境: 使用 InMemory（快速、无需文件）")
	fmt.Println("   - 生产环境: 使用 SQLite（持久化、支持 FTS5 搜索）")
	fmt.Println("   - 通过工厂函数统一创建，便于切换")
	fmt.Println()

	demonstrateAgentWithMemory()
}

func demonstrateAgentWithMemory() {
	fmt.Println("🤖 Agent 集成 Memory 示例")
	fmt.Println("-" + string(make([]byte, 40)))

	memStore, _ := ap.NewMemory(ap.StoreConfig{
		Type: ap.BackendMemory,
	})
	defer memStore.Close()

	ctx := context.Background()

	preloadMemories(memStore)

	demoLLM := demo.NewDemoLLM(
		"根据你的记忆，我们之前讨论过 Go 语言和 AI Agent 的开发。",
	)

	memoryAgent, err := ap.NewAgent("MemoryBot", "你拥有长期记忆，可以记住对话历史。请参考记忆回答问题。", demoLLM)
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}
	memoryAgent = memoryAgent.WithMemory(memStore)

	resp, _ := memoryAgent.Run(ctx, ap.UserMessage("我们之前讨论过什么？"))
	fmt.Printf("🤖 Agent 回复: %s\n", resp.Content)

	stats, _ := memStore.Stats(ctx)
	fmt.Printf("📊 Memory 状态: %d 条记忆\n", stats.TotalEpisodes)

	data, _ := memStore.ExportMemories(ctx, "", "json")
	fmt.Printf("💾 导出数据大小: %d bytes\n", len(data))

	var episodes []map[string]interface{}
	json.Unmarshal(data, &episodes)
	fmt.Printf("📋 导出记录数: %d\n", len(episodes))
	fmt.Println()
}

func preloadMemories(store ap.Memory) {
	ctx := context.Background()

	memories := []struct {
		content string
		topics  string
	}{
		{"Go 语言是一种高效的编程语言，特别适合并发编程", "Go,编程语言"},
		{"AgentPrimordia 是一个生产级 AI Agent 开发框架", "AgentPrimordia,AI"},
		{"ReAct Loop 是 Agent 的核心推理循环", "Agent,架构"},
		{"Memory 系统让 Agent 能够记住对话历史", "Memory,功能"},
	}

	for i, mem := range memories {
		ep := &ap.Episode{
			ID:        fmt.Sprintf("preload-%03d", i+1),
			SessionID: "preloaded-session",
			Role:      "assistant",
			Content:   mem.content,
			Topics:    mem.topics,
			CreatedAt: time.Now().Add(-time.Duration(i+1) * time.Hour).Format(time.RFC3339),
		}
		store.Add(ctx, ep)
	}
}
