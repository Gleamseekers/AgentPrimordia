// failure_store_bench_test.go — v4.2-3 全链路 P95 基准扩展：失败记录存储
//
// 新增关键路径：SQLiteFailureStore Record / Get / List（v4.1 真实接线产物），
// 与 MemoryFailureStore 对比；输出格式与 p95_latency_test.go 一致，
// 可被 bench-regression-check.sh 解析（p50/p95/p99 指标）。
package suite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/persist"
)

// BenchmarkP95FailureSQLite_Record 关键路径四：SQLiteFailureStore Record 延迟分布。
func BenchmarkP95FailureSQLite_Record(b *testing.B) {
	dsn := filepath.Join(b.TempDir(), "failures.db")
	store, err := persist.NewSQLiteFailureStore(dsn)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	rec := &persist.FailureRecord{
		ID: "bench", AgentID: "agent-bench", Phase: persist.PhaseRun,
		Error: "bench error", Turn: 1,
		State: &persist.AgentState{AgentID: "agent-bench", Status: "failed", TurnCount: 1},
	}

	b.ResetTimer()
	var c p95Collector
	for i := 0; i < b.N; i += p95BatchSize {
		n := p95BatchSize
		if i+n > b.N {
			n = b.N - i
		}
		var total int64
		for j := 0; j < n; j++ {
			s := time.Now().UnixNano()
			_ = store.Record(ctx, rec)
			e := time.Now().UnixNano()
			total += e - s
		}
		c.add(time.Duration(total), n)
	}
	c.report(b)
}

// BenchmarkP95FailureSQLite_Get 关键路径五：SQLiteFailureStore Get 延迟分布。
func BenchmarkP95FailureSQLite_Get(b *testing.B) {
	dsn := filepath.Join(b.TempDir(), "failures.db")
	store, err := persist.NewSQLiteFailureStore(dsn)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	rec := &persist.FailureRecord{ID: "bench", AgentID: "agent-bench", Phase: persist.PhaseRun, Error: "e"}
	if err := store.Record(ctx, rec); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	var c p95Collector
	for i := 0; i < b.N; i += p95BatchSize {
		n := p95BatchSize
		if i+n > b.N {
			n = b.N - i
		}
		var total int64
		for j := 0; j < n; j++ {
			s := time.Now().UnixNano()
			_, _ = store.Get(ctx, "bench")
			e := time.Now().UnixNano()
			total += e - s
		}
		c.add(time.Duration(total), n)
	}
	c.report(b)
}

// BenchmarkP95FailureSQLite_List 关键路径六：SQLiteFailureStore List（10 条）延迟分布。
func BenchmarkP95FailureSQLite_List(b *testing.B) {
	dsn := filepath.Join(b.TempDir(), "failures.db")
	store, err := persist.NewSQLiteFailureStore(dsn)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for i := range 10 {
		_ = store.Record(ctx, &persist.FailureRecord{ID: "f" + itoa(i), AgentID: "a", Phase: persist.PhaseRun, Error: "e"})
	}

	b.ResetTimer()
	var c p95Collector
	for i := 0; i < b.N; i += p95BatchSize {
		n := p95BatchSize
		if i+n > b.N {
			n = b.N - i
		}
		var total int64
		for j := 0; j < n; j++ {
			s := time.Now().UnixNano()
			_, _ = store.List(ctx, "")
			e := time.Now().UnixNano()
			total += e - s
		}
		c.add(time.Duration(total), n)
	}
	c.report(b)
}

// BenchmarkP95FailureMemory_Record 对照：MemoryFailureStore Record 延迟分布。
func BenchmarkP95FailureMemory_Record(b *testing.B) {
	store := persist.NewMemoryFailureStore()
	ctx := context.Background()
	rec := &persist.FailureRecord{ID: "bench", AgentID: "a", Phase: persist.PhaseRun, Error: "e"}

	b.ResetTimer()
	var c p95Collector
	for i := 0; i < b.N; i += p95BatchSize {
		n := p95BatchSize
		if i+n > b.N {
			n = b.N - i
		}
		var total int64
		for j := 0; j < n; j++ {
			s := time.Now().UnixNano()
			_ = store.Record(ctx, rec)
			e := time.Now().UnixNano()
			total += e - s
		}
		c.add(time.Duration(total), n)
	}
	c.report(b)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
