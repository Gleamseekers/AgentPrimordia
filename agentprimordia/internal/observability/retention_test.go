package observability

import "testing"

// TestCorrelationStoreRetentionEvictsOldest 有界保留：超过上限时淘汰最旧记录。
// 背景（v7.4）：CorrelationStore 原为无界 map，长驻进程会持续增长；
// 默认构造进入 Agent 前必须先有 retention。
func TestCorrelationStoreRetentionEvictsOldest(t *testing.T) {
	s := NewCorrelationStore(WithMaxRecords(3))
	for _, id := range []string{"t1", "t2", "t3", "t4", "t5"} {
		s.Start(id, "agent", "sess")
	}

	if got := s.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3（应有界）", got)
	}
	if s.Get("t1") != nil || s.Get("t2") != nil {
		t.Error("最旧的 t1/t2 应被淘汰")
	}
	if s.Get("t5") == nil {
		t.Error("最新的 t5 应保留")
	}
	if got := len(s.List(10)); got != 3 {
		t.Errorf("List() 长度 = %d, want 3（order 与 records 必须同步）", got)
	}
	if got := s.List(10)[0].TraceID; got != "t3" {
		t.Errorf("List 首元素 = %s, want t3（保持插入顺序）", got)
	}
}

// TestCorrelationStoreDefaultRetentionBounded 默认必须有上限（避免无界增长）。
func TestCorrelationStoreDefaultRetentionBounded(t *testing.T) {
	s := NewCorrelationStore()
	if s.maxRecords != defaultMaxRecords {
		t.Errorf("默认 maxRecords = %d, want %d", s.maxRecords, defaultMaxRecords)
	}
	if defaultMaxRecords <= 0 {
		t.Fatal("defaultMaxRecords 必须为正")
	}
}

// TestCorrelationStoreWithMaxRecordsNonPositiveFallsBack 非法上限回退默认值而非变为无界。
func TestCorrelationStoreWithMaxRecordsNonPositiveFallsBack(t *testing.T) {
	for _, n := range []int{0, -1} {
		s := NewCorrelationStore(WithMaxRecords(n))
		if s.maxRecords != defaultMaxRecords {
			t.Errorf("WithMaxRecords(%d) → maxRecords=%d, want 默认 %d", n, s.maxRecords, defaultMaxRecords)
		}
	}
}

// TestCorrelationStoreRetentionHandlesRefresh 同一 traceID 重复 Start 不占额外槽位。
func TestCorrelationStoreRetentionHandlesRefresh(t *testing.T) {
	s := NewCorrelationStore(WithMaxRecords(2))
	s.Start("a", "", "")
	s.Start("b", "", "")
	s.Start("a", "", "") // 刷新 a，不应重复入 order
	s.Start("c", "", "")

	if s.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", s.Len())
	}
	if s.Get("b") == nil || s.Get("c") == nil {
		t.Error("刷新后应淘汰最旧的 a，保留 b/c")
	}
	if s.Get("a") != nil {
		t.Error("a 已被淘汰")
	}
}
