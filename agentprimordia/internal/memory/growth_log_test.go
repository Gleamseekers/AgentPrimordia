package memory

import (
	"testing"
	"time"
)

func TestGrowthEventLog_RecordAndRecent(t *testing.T) {
	log, err := NewGrowthEventLog(":memory:")
	if err != nil {
		t.Fatalf("NewGrowthEventLog() error = %v", err)
	}
	defer log.Close()

	if err := log.Record("capability_unlocked", "go_testing 解锁", "成功率 70%", map[string]string{"domain": "go_testing"}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := log.Record("capability_mastered", "go_testing 精通", "成功率 95%", nil); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	events, err := log.Recent(10)
	if err != nil {
		t.Fatalf("Recent() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Recent() len = %d, want 2", len(events))
	}
	// 新→旧排序
	if events[0].Category != "capability_mastered" {
		t.Errorf("events[0].Category = %q, want capability_mastered", events[0].Category)
	}
	if events[1].Category != "capability_unlocked" {
		t.Errorf("events[1].Category = %q, want capability_unlocked", events[1].Category)
	}
}

func TestGrowthEventLog_Since(t *testing.T) {
	log, err := NewGrowthEventLog(":memory:")
	if err != nil {
		t.Fatalf("NewGrowthEventLog() error = %v", err)
	}
	defer log.Close()

	_ = log.Record("test", "event1", "", nil)

	// 使用未来时间确保只匹配第二条
	future := time.Now().Add(1 * time.Hour)
	_ = log.Record("test", "event2", "", nil)

	// Since(future) 应该返回 0 条（所有事件都在 future 之前）
	events, err := log.Since(future)
	if err != nil {
		t.Fatalf("Since() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("Since(future) len = %d, want 0", len(events))
	}

	// Since(zero) 应该返回所有事件
	events, err = log.Since(time.Time{})
	if err != nil {
		t.Fatalf("Since() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Since(zero) len = %d, want 2", len(events))
	}
}

func TestGrowthEventLog_ByCategory(t *testing.T) {
	log, err := NewGrowthEventLog(":memory:")
	if err != nil {
		t.Fatalf("NewGrowthEventLog() error = %v", err)
	}
	defer log.Close()

	_ = log.Record("a", "a1", "", nil)
	_ = log.Record("b", "b1", "", nil)
	_ = log.Record("a", "a2", "", nil)

	events, err := log.ByCategory("a", 10)
	if err != nil {
		t.Fatalf("ByCategory() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ByCategory() len = %d, want 2", len(events))
	}
}

func TestGrowthEventLog_Count(t *testing.T) {
	log, err := NewGrowthEventLog(":memory:")
	if err != nil {
		t.Fatalf("NewGrowthEventLog() error = %v", err)
	}
	defer log.Close()

	count, err := log.Count()
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 0 {
		t.Errorf("Count() = %d, want 0", count)
	}

	_ = log.Record("test", "event", "", nil)
	count, _ = log.Count()
	if count != 1 {
		t.Errorf("Count() = %d, want 1", count)
	}
}

func TestDetectMilestones(t *testing.T) {
	log, err := NewGrowthEventLog(":memory:")
	if err != nil {
		t.Fatalf("NewGrowthEventLog() error = %v", err)
	}
	defer log.Close()

	sm := NewSelfModel()
	// 3 次成功 → 100% 成功率，应触发 unlocked
	sm.RecordOutcome("test-domain", true, 1, "")
	sm.RecordOutcome("test-domain", true, 1, "")
	sm.RecordOutcome("test-domain", true, 1, "")

	snap := sm.Snapshot()
	DetectMilestones(snap, log)

	count, _ := log.Count()
	if count < 1 {
		t.Errorf("DetectMilestones() recorded %d events, want >= 1", count)
	}

	events, _ := log.Recent(10)
	found := false
	for _, e := range events {
		if e.Category == "capability_unlocked" {
			found = true
			break
		}
	}
	if !found {
		t.Error("DetectMilestones() did not record capability_unlocked event")
	}
}
