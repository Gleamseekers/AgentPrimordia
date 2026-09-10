package memory

import (
	"testing"
	"time"
)

func TestSelfModel_Snapshot_Empty(t *testing.T) {
	sm := NewSelfModel()
	snap := sm.Snapshot()

	if snap.TotalTasks != 0 {
		t.Errorf("TotalTasks = %d, want 0", snap.TotalTasks)
	}
	if snap.SuccessRate != 0 {
		t.Errorf("SuccessRate = %f, want 0", snap.SuccessRate)
	}
	if len(snap.Capabilities) != 0 {
		t.Errorf("Capabilities len = %d, want 0", len(snap.Capabilities))
	}
}

func TestSelfModel_Snapshot_WithData(t *testing.T) {
	sm := NewSelfModel()
	sm.RecordOutcome("code-review", true, 3, "")
	sm.RecordOutcome("code-review", true, 2, "")
	sm.RecordOutcome("code-review", false, 5, "syntax_error")
	sm.RecordOutcome("sql-migration", true, 4, "")
	sm.RecordOutcome("sql-migration", false, 6, "timeout")
	sm.RecordOutcome("sql-migration", false, 7, "timeout")

	snap := sm.Snapshot()

	if snap.TotalTasks != 6 {
		t.Errorf("TotalTasks = %d, want 6", snap.TotalTasks)
	}
	if len(snap.Capabilities) != 2 {
		t.Errorf("Capabilities len = %d, want 2", len(snap.Capabilities))
	}
	// 排序：code-review (67%) 应在 sql-migration (33%) 前面
	if snap.Capabilities[0].Domain != "code-review" {
		t.Errorf("first capability = %q, want code-review", snap.Capabilities[0].Domain)
	}
	// 67% < 70% 阈值，趋势为 stable
	if snap.Capabilities[0].Trend != "stable" {
		t.Errorf("code-review trend = %q, want stable (67%% < 70%% threshold)", snap.Capabilities[0].Trend)
	}
	if len(snap.TopFailures) != 2 {
		t.Errorf("TopFailures len = %d, want 2", len(snap.TopFailures))
	}
	if snap.TopFailures[0].Signature != "timeout" {
		t.Errorf("top failure = %q, want timeout", snap.TopFailures[0].Signature)
	}
}

func TestSelfModel_TopCapabilities(t *testing.T) {
	sm := NewSelfModel()
	sm.RecordOutcome("a", true, 1, "")
	sm.RecordOutcome("b", true, 1, "")
	sm.RecordOutcome("b", true, 1, "")
	sm.RecordOutcome("c", false, 1, "err")

	top := sm.TopCapabilities(2)
	if len(top) != 2 {
		t.Fatalf("TopCapabilities(2) len = %d, want 2", len(top))
	}
	if top[0].Domain != "b" {
		t.Errorf("top[0] = %q, want b", top[0].Domain)
	}
}

func TestSelfModel_GrowthMetrics(t *testing.T) {
	sm := NewSelfModel()
	sm.RecordOutcome("go", true, 1, "")
	sm.RecordOutcome("go", true, 1, "")
	sm.RecordOutcome("python", false, 1, "err")

	metrics := sm.GrowthMetrics()
	if metrics["go"] != 1.0 {
		t.Errorf("go metric = %f, want 1.0", metrics["go"])
	}
	if metrics["python"] != 0.0 {
		t.Errorf("python metric = %f, want 0.0", metrics["python"])
	}
}

func TestSelfModel_Snapshot_Concurrent(t *testing.T) {
	sm := NewSelfModel()
	done := make(chan struct{})

	go func() {
		for i := 0; i < 100; i++ {
			sm.RecordOutcome("concurrent", i%2 == 0, 1, "")
		}
		close(done)
	}()

	for i := 0; i < 50; i++ {
		snap := sm.Snapshot()
		if snap == nil {
			t.Fatal("Snapshot() returned nil during concurrent writes")
		}
	}
	<-done
}

func TestSelfModel_Snapshot_TrendThreshold(t *testing.T) {
	sm := NewSelfModel()
	// 样本 <= 3 时应为 stable
	sm.RecordOutcome("low-sample", true, 1, "")
	sm.RecordOutcome("low-sample", true, 1, "")

	snap := sm.Snapshot()
	if snap.Capabilities[0].Trend != "stable" {
		t.Errorf("low-sample trend = %q, want stable (sample <= 3)", snap.Capabilities[0].Trend)
	}

	// 样本 > 3 且成功率 > 70% 应为 improving
	for i := 0; i < 3; i++ {
		sm.RecordOutcome("high-rate", true, 1, "")
	}
	snap = sm.Snapshot()
	for _, c := range snap.Capabilities {
		if c.Domain == "high-rate" && c.Trend != "improving" {
			t.Errorf("high-rate trend = %q, want improving", c.Trend)
		}
	}
}

func TestSelfModel_Snapshot_UpdatedAt(t *testing.T) {
	sm := NewSelfModel()
	before := time.Now()
	sm.RecordOutcome("test", true, 1, "")
	after := time.Now()

	snap := sm.Snapshot()
	if snap.UpdatedAt.Before(before) || snap.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt %v not in range [%v, %v]", snap.UpdatedAt, before, after)
	}
}
