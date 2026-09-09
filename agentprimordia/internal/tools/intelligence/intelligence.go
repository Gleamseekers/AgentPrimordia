// intelligence.go — 统一工具智能入口
package intelligence

import (
	"context"
	"time"
)

// ToolIntelligence 统一工具智能入口
type ToolIntelligence struct {
	detector GapDetector
	creator  ToolCreator
	profiler ToolProfiler
	tuner    ToolTuner
	selector ToolSelector
	catalog  *ToolCatalog
}

// GapDetector 缺口检测器
type GapDetector interface {
	Detect(ctx context.Context, trace []ToolCallRecord) ([]GapCandidate, error)
}

// ToolCreator 工具生成器
type ToolCreator interface {
	Create(ctx context.Context, gap GapCandidate) (*ToolArtifact, error)
}

// ToolProfiler 工具性能画像
type ToolProfiler interface {
	Record(ctx context.Context, usage ToolUsageRecord) error
	Profile(ctx context.Context, toolName string) (*ToolProfile, error)
	AllProfiles(ctx context.Context) (map[string]*ToolProfile, error)
}

// ToolTuner 参数调优器
type ToolTuner interface {
	SuggestTuning(ctx context.Context, toolName string, profile *ToolProfile) (*TuningSuggestion, error)
	ApplyTuning(ctx context.Context, toolName string, suggestion *TuningSuggestion) error
}

// ToolSelector 工具选择优化
type ToolSelector interface {
	Select(ctx context.Context, task string, candidates []string) (string, error)
	RecordOutcome(ctx context.Context, toolName string, success bool) error
}

// ToolCatalog 工具目录（简单内存实现）
type ToolCatalog struct {
	tools map[string]ToolEntry
}

// ToolEntry 工具目录条目
type ToolEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Domain      string `json:"domain,omitempty"`
}

// NewToolCatalog 创建工具目录
func NewToolCatalog() *ToolCatalog {
	return &ToolCatalog{tools: make(map[string]ToolEntry)}
}

// TaskMatcher 任务-工具匹配器接口
type TaskMatcher interface {
	Match(task string, tools []ToolEntry) ToolEntry
}

// 数据类型

// GapCandidate 缺口候选
type GapCandidate struct {
	Kind        string    `json:"kind"`
	Key         string    `json:"key"`
	Count       int       `json:"count"`
	SampleError string    `json:"sample_error,omitempty"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// ToolArtifact 工具产物
type ToolArtifact struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ArtifactSHA string `json:"artifact_sha"`
	Artifact    []byte `json:"artifact"`
}

// ToolCallRecord 工具调用记录
type ToolCallRecord struct {
	ToolName  string        `json:"tool_name"`
	Args      string        `json:"args"`
	Result    string        `json:"result"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	Success   bool          `json:"success"`
	Timestamp time.Time     `json:"timestamp"`
}

// ToolUsageRecord 工具使用记录
type ToolUsageRecord struct {
	ToolName string
	Success  bool
	Duration time.Duration
	Tokens   int
}

// ToolProfile 工具性能画像
type ToolProfile struct {
	ToolName    string        `json:"tool_name"`
	TotalCalls  int           `json:"total_calls"`
	SuccessRate float64       `json:"success_rate"`
	AvgDuration time.Duration `json:"avg_duration"`
	P95Duration time.Duration `json:"p95_duration"`
	AvgTokens   int           `json:"avg_tokens"`
	LastUsed    time.Time     `json:"last_used"`
}

// TuningSuggestion 调优建议
type TuningSuggestion struct {
	ToolName     string  `json:"tool_name"`
	Parameter    string  `json:"parameter"`
	CurrentVal   string  `json:"current_val"`
	SuggestedVal string  `json:"suggested_val"`
	Confidence   float64 `json:"confidence"`
	Reason       string  `json:"reason"`
}

// NewToolIntelligence 构造器
func NewToolIntelligence(detector GapDetector, creator ToolCreator, profiler ToolProfiler, tuner ToolTuner, selector ToolSelector) *ToolIntelligence {
	return &ToolIntelligence{
		detector: detector,
		creator:  creator,
		profiler: profiler,
		tuner:    tuner,
		selector: selector,
		catalog:  NewToolCatalog(),
	}
}
