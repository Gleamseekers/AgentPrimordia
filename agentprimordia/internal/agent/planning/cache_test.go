package planning

import (
	"sync"
	"testing"
	"time"
)

// TestPlanCache_HitOnSimilarTask 验证相似任务描述能命中缓存
func TestPlanCache_HitOnSimilarTask(t *testing.T) {
	cache := NewPlanCache(0.3)

	plan := &Plan{
		Goal: "编写单元测试",
		SubTasks: []SubTask{
			{ID: "1", Description: "分析代码结构", Status: TaskPending},
			{ID: "2", Description: "编写测试用例", Status: TaskPending, DependsOn: []string{"1"}},
		},
		CreatedAt: time.Now(),
	}

	cache.Put("编写单元测试", plan)

	// 用相似（不完全相同）的任务描述查询，应命中
	result := cache.Get("编写单元测试代码")
	if result == nil {
		t.Fatal("期望命中缓存，但返回 nil")
	}
	if result.Goal != "编写单元测试" {
		t.Errorf("期望 Goal=%q，实际=%q", "编写单元测试", result.Goal)
	}
}

// TestPlanCache_MissOnDissimilarTask 验证不相似的任务描述不会命中缓存
func TestPlanCache_MissOnDissimilarTask(t *testing.T) {
	cache := NewPlanCache(0.5)

	plan := &Plan{
		Goal:     "编写单元测试",
		SubTasks: []SubTask{{ID: "1", Description: "分析代码", Status: TaskPending}},
		CreatedAt: time.Now(),
	}

	cache.Put("编写单元测试", plan)

	// 完全不相关的任务描述，应未命中
	result := cache.Get("部署到生产环境并监控性能")
	if result != nil {
		t.Fatal("期望未命中缓存，但返回了非 nil 结果")
	}
}

// TestPlanCache_EmptyCache 验证空缓存查询返回 nil
func TestPlanCache_EmptyCache(t *testing.T) {
	cache := NewPlanCache(0.3)

	result := cache.Get("任意任务描述")
	if result != nil {
		t.Fatal("空缓存应返回 nil")
	}
}

// TestPlanCache_MultipleEntries 验证多条缓存时返回最佳匹配
func TestPlanCache_MultipleEntries(t *testing.T) {
	cache := NewPlanCache(0.2)

	planA := &Plan{
		Goal:      "数据分析报告",
		SubTasks:  []SubTask{{ID: "a1", Description: "收集数据", Status: TaskPending}},
		CreatedAt: time.Now(),
	}
	planB := &Plan{
		Goal:      "单元测试编写",
		SubTasks:  []SubTask{{ID: "b1", Description: "分析覆盖率", Status: TaskPending}},
		CreatedAt: time.Now(),
	}
	planC := &Plan{
		Goal:      "前端页面开发",
		SubTasks:  []SubTask{{ID: "c1", Description: "设计 UI", Status: TaskPending}},
		CreatedAt: time.Now(),
	}

	cache.Put("数据分析报告生成", planA)
	cache.Put("单元测试编写与执行", planB)
	cache.Put("前端页面开发与部署", planC)

	// 查询与 planA 最相似的任务
	result := cache.Get("数据分析与报告")
	if result == nil {
		t.Fatal("期望命中缓存，但返回 nil")
	}
	if result.Goal != "数据分析报告" {
		t.Errorf("期望匹配 planA（Goal=%q），实际匹配 Goal=%q", "数据分析报告", result.Goal)
	}

	// 查询与 planB 最相似的任务
	result2 := cache.Get("单元测试编写")
	if result2 == nil {
		t.Fatal("期望命中缓存，但返回 nil")
	}
	if result2.Goal != "单元测试编写" {
		t.Errorf("期望匹配 planB（Goal=%q），实际匹配 Goal=%q", "单元测试编写", result2.Goal)
	}
}

// TestPlanCache_ConcurrentAccess 验证并发读写不会触发 data race
func TestPlanCache_ConcurrentAccess(t *testing.T) {
	cache := NewPlanCache(0.3)

	plans := []*Plan{
		{Goal: "任务A", SubTasks: []SubTask{{ID: "a1", Description: "步骤A", Status: TaskPending}}, CreatedAt: time.Now()},
		{Goal: "任务B", SubTasks: []SubTask{{ID: "b1", Description: "步骤B", Status: TaskPending}}, CreatedAt: time.Now()},
		{Goal: "任务C", SubTasks: []SubTask{{ID: "c1", Description: "步骤C", Status: TaskPending}}, CreatedAt: time.Now()},
	}

	var wg sync.WaitGroup
	iterations := 100

	// 并发写入
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := plans[idx%len(plans)]
			cache.Put(p.Goal, p)
		}(i)
	}

	// 并发读取
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cache.Get("任务A")
			_ = cache.Get("无关任务描述")
		}()
	}

	wg.Wait()
	// 若无 panic 或 race condition 即通过
}

// TestTokenize 验证分词器的基本行为
func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  int // 仅验证 token 数量非零
	}{
		{"hello world", 2},
		{"数据分析报告", 6}, // 每个汉字独立为一个 token
		{"编写 单元测试，覆盖核心逻辑", 12}, // 每个 CJK 汉字独立为一个 token，逗号为分隔符
		{"", 0},
	}

	for _, tt := range tests {
		tokens := tokenize(tt.input)
		if len(tokens) != tt.want {
			t.Errorf("tokenize(%q): 期望 %d 个 token，实际 %d 个 (%v)", tt.input, tt.want, len(tokens), tokens)
		}
	}
}

// TestTokenOverlap 验证 Jaccard 系数计算
func TestTokenOverlap(t *testing.T) {
	// 完全相同
	score := tokenOverlap("hello world", "hello world")
	if score != 1.0 {
		t.Errorf("完全相同字符串的 overlap 应为 1.0，实际 %f", score)
	}

	// 完全不同
	score = tokenOverlap("hello world", "foo bar")
	if score != 0.0 {
		t.Errorf("完全不同字符串的 overlap 应为 0.0，实际 %f", score)
	}

	// 部分重叠：Jaccard("hello world", "hello foo") = 1/3
	score = tokenOverlap("hello world", "hello foo")
	expected := 1.0 / 3.0
	if abs(score-expected) > 0.01 {
		t.Errorf("部分重叠的 overlap 应约为 %f，实际 %f", expected, score)
	}

	// 两个空字符串
	score = tokenOverlap("", "")
	if score != 1.0 {
		t.Errorf("两个空字符串的 overlap 应为 1.0，实际 %f", score)
	}
}

// abs 返回浮点数的绝对值
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
