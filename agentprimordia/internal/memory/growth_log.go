package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// GrowthEvent 成长里程碑事件。
type GrowthEvent struct {
	ID        int64             `json:"id"`
	Category  string            `json:"category"`  // "capability_unlocked" | "capability_mastered" | "failure_mitigated" | "consolidation"
	Title     string            `json:"title"`      // 人类可读标题
	Detail    string            `json:"detail"`     // 详细描述
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// GrowthEventLog 成长事件日志（append-only）。
type GrowthEventLog struct {
	mu sync.Mutex
	db *sql.DB
}

// NewGrowthEventLog 创建成长事件日志。
// dsn 为 SQLite 连接串（如 "file:growth.db?_pragma=journal_mode(WAL)" 或 ":memory:"）。
func NewGrowthEventLog(dsn string) (*GrowthEventLog, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开成长事件数据库失败: %w", err)
	}

	createSQL := `CREATE TABLE IF NOT EXISTS growth_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		category TEXT NOT NULL,
		title TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT '',
		metadata TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_growth_events_category ON growth_events(category);
	CREATE INDEX IF NOT EXISTS idx_growth_events_created_at ON growth_events(created_at);`

	if _, err := db.Exec(createSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("创建成长事件表失败: %w", err)
	}

	return &GrowthEventLog{db: db}, nil
}

// Record 记录一条成长事件。
func (g *GrowthEventLog) Record(category, title, detail string, metadata map[string]string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		metaJSON = []byte("{}")
	}

	_, err = g.db.Exec(
		"INSERT INTO growth_events (category, title, detail, metadata, created_at) VALUES (?, ?, ?, ?, ?)",
		category, title, detail, string(metaJSON), time.Now().Format(time.RFC3339),
	)
	return err
}

// Recent 返回最近 n 条事件（新→旧）。
func (g *GrowthEventLog) Recent(n int) ([]GrowthEvent, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	rows, err := g.db.Query(
		"SELECT id, category, title, detail, metadata, created_at FROM growth_events ORDER BY id DESC LIMIT ?", n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

// Since 返回指定时间之后的事件（新→旧）。
func (g *GrowthEventLog) Since(since time.Time) ([]GrowthEvent, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	rows, err := g.db.Query(
		"SELECT id, category, title, detail, metadata, created_at FROM growth_events WHERE created_at >= ? ORDER BY id DESC",
		since.Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

// ByCategory 返回指定类别的最近 n 条事件。
func (g *GrowthEventLog) ByCategory(category string, n int) ([]GrowthEvent, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	rows, err := g.db.Query(
		"SELECT id, category, title, detail, metadata, created_at FROM growth_events WHERE category = ? ORDER BY id DESC LIMIT ?",
		category, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

// Count 返回事件总数。
func (g *GrowthEventLog) Count() (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	var count int
	err := g.db.QueryRow("SELECT COUNT(*) FROM growth_events").Scan(&count)
	return count, err
}

// Close 关闭数据库连接。
func (g *GrowthEventLog) Close() error {
	return g.db.Close()
}

func scanEvents(rows *sql.Rows) ([]GrowthEvent, error) {
	var events []GrowthEvent
	for rows.Next() {
		var e GrowthEvent
		var metaStr, createdAtStr string
		if err := rows.Scan(&e.ID, &e.Category, &e.Title, &e.Detail, &metaStr, &createdAtStr); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(metaStr), &e.Metadata)
		if e.Metadata == nil {
			e.Metadata = make(map[string]string)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
		events = append(events, e)
	}
	return events, rows.Err()
}

// DetectMilestones 检查 SelfModel 快照中的里程碑并记录事件。
// 应在 RecordOutcome 后调用。
func DetectMilestones(snap *SelfModelSnapshot, log *GrowthEventLog) {
	for _, cs := range snap.Capabilities {
		total := cs.Successes + cs.Failures
		rate := cs.SuccessRate

		// 能力解锁：首次达到 70% 成功率且样本 >= 3
		if rate >= 0.7 && total >= 3 && total <= 5 {
			_ = log.Record("capability_unlocked",
				fmt.Sprintf("%s 能力解锁", cs.Domain),
				fmt.Sprintf("%s 成功率 %.0f%% (%d 次)", cs.Domain, rate*100, total),
				map[string]string{"domain": cs.Domain, "rate": fmt.Sprintf("%.2f", rate)},
			)
		}

		// 能力精通：成功率 >= 90% 且样本 >= 5
		if rate >= 0.9 && total >= 5 && total <= 7 {
			_ = log.Record("capability_mastered",
				fmt.Sprintf("%s 能力精通", cs.Domain),
				fmt.Sprintf("%s 成功率 %.0f%% (%d 次)", cs.Domain, rate*100, total),
				map[string]string{"domain": cs.Domain, "rate": fmt.Sprintf("%.2f", rate)},
			)
		}
	}
}
