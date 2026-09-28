// Package studio 提供 Studio Web UI 的后端 HTTP API。
//
// Studio 四面板（Chaos Lab / Cluster / Learning / Marketplace）依赖的
// /api/v1/* 端点在此实现。各面板通过 Service 接口与底层逻辑包解耦：
//
//   - ChaosService       → 可注入 internal/chaos 引擎
//   - ClusterService     → 可注入 internal/agent/cluster 管理器
//   - LearningService    → 可注入 internal/agent/learning 蒸馏器
//   - MarketplaceService → 可注入 internal/agent/marketplace 注册表
//
// 默认使用 demo 实现（demo.go），保证开箱即可演示；真实引擎通过
// WithChaos / WithCluster / WithLearning / WithMarketplace 注入。
//
// 响应类型与 studio/web/src/pages/*.tsx 中前端定义的接口一一对应。
package studio

import (
	"context"
	"net/http"
)

// ===== 前端契约类型（与 studio/web/src/pages/ 中 TS 接口对齐） =====

// Experiment 混沌实验定义。
type Experiment struct {
	Name                string  `json:"name"`
	Description         string  `json:"description"`
	Hypothesis          string  `json:"hypothesis"`
	Status              string  `json:"status"` // pending | running | completed | aborted | failed
	Duration            string  `json:"duration"`
	Faults              []Fault `json:"faults"`
	HypothesisValidated bool    `json:"hypothesisValidated"`
}

// Fault 单个故障注入描述。
type Fault struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// SteadyState 稳态校验结果。
type SteadyState struct {
	Met     bool   `json:"met"`
	Message string `json:"message"`
}

// ExperimentResult 实验执行结果。
type ExperimentResult struct {
	Experiment      Experiment  `json:"experiment"`
	StartTime       string      `json:"startTime"`
	EndTime         string      `json:"endTime"`
	PreSteadyState  SteadyState `json:"preSteadyState"`
	PostSteadyState SteadyState `json:"postSteadyState"`
}

// NodeInfo 集群节点信息。
type NodeInfo struct {
	ID           string   `json:"id"`
	Address      string   `json:"address"`
	Role         string   `json:"role"`   // leader | follower | candidate
	Status       string   `json:"status"` // online | offline | leaving
	Capabilities []string `json:"capabilities"`
	LastSeen     string   `json:"lastSeen"`
	Shards       int      `json:"shards"` // 该节点持有的分片数
}

// ClusterStatus 集群状态快照。
type ClusterStatus struct {
	Nodes        []NodeInfo `json:"nodes"`
	LeaderID     string     `json:"leaderId"`
	HashRingSize int        `json:"hashRingSize"`
	TotalShards  int        `json:"totalShards"`
}

// LearningStats 知识蒸馏统计。
type LearningStats struct {
	TotalInteractions   int `json:"totalInteractions"`
	TotalDistilled      int `json:"totalDistilled"`
	TotalKnowledgeItems int `json:"totalKnowledgeItems"`
}

// CapabilityHistoryPoint 能力分数时间点。
type CapabilityHistoryPoint struct {
	Score      float64 `json:"score"`
	RecordedAt string  `json:"recordedAt"`
}

// CapabilityHistory 单个能力的历史分数序列。
type CapabilityHistory struct {
	Name    string                   `json:"name"`
	History []CapabilityHistoryPoint `json:"history"`
}

// Capability 能力进化条目。
type Capability struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
	TimesTested int     `json:"timesTested"`
	TimesPassed int     `json:"timesPassed"`
}

// PipelineStats 蒸馏管道统计。
type PipelineStats struct {
	TotalProcessed       int    `json:"totalProcessed"`
	TotalFactsWritten    int    `json:"totalFactsWritten"`
	TotalPatternsWritten int    `json:"totalPatternsWritten"`
	TotalRAGQueries      int    `json:"totalRAGQueries"`
	LastProcessTime      string `json:"lastProcessTime"`
}

// AgentTemplate 市场模板。
type AgentTemplate struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Rating      float64  `json:"rating"`
	Downloads   int      `json:"downloads"`
}

// CreateExperimentRequest POST /api/v1/chaos/experiments 请求体。
type CreateExperimentRequest struct {
	Name       string `json:"name"`
	Hypothesis string `json:"hypothesis"`
	FaultType  string `json:"faultType"`
}

// DeployRequest POST /api/v1/marketplace/deploy 请求体。
type DeployRequest struct {
	TemplateID string `json:"template_id"`
}

// Deployment 已部署的 Agent 实例记录。
type Deployment struct {
	ID         string `json:"id"`
	TemplateID string `json:"templateId"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Category   string `json:"category"`
	Status     string `json:"status"` // running | stopped
	DeployedAt string `json:"deployedAt"`
}

// ===== v3.3-v3.6 面板契约类型（与 studio/web/src/pages/*.tsx 对齐） =====

// AutonomyGoal 自治目标条目（AutonomyMonitor.tsx GoalInfo）。
type AutonomyGoal struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	State       string  `json:"state"`
	Priority    int     `json:"priority"`
	Progress    float64 `json:"progress"`
	RetryCount  int     `json:"retryCount"`
	CreatedAt   string  `json:"createdAt"`
}

// AutonomyAlert 自治告警条目（AutonomyMonitor.tsx AlertInfo）。
type AutonomyAlert struct {
	GoalID    string `json:"goalId"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// SkillEntry 技能条目（SkillLibrary.tsx SkillInfo）。
type SkillEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Status      string   `json:"status"`
	UsageCount  int      `json:"usageCount"`
	SuccessRate float64  `json:"successRate"`
	Tags        []string `json:"tags"`
}

// RealtimeSessionInfo 实时会话条目（RealtimeConsole.tsx SessionInfo）。
type RealtimeSessionInfo struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	CreatedAt string `json:"createdAt"`
}

// RealtimeEventInfo 实时事件条目（RealtimeConsole.tsx RealtimeEvent）。
type RealtimeEventInfo struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	Timestamp string `json:"timestamp"`
}

// ===== 服务接口（可注入真实引擎） =====

// ChaosService 混沌实验服务。
type ChaosService interface {
	ListExperiments(ctx context.Context) ([]ExperimentResult, error)
	CreateExperiment(ctx context.Context, req CreateExperimentRequest) error
	AbortExperiment(ctx context.Context, name string) error
}

// ClusterService 集群状态服务。
type ClusterService interface {
	Status(ctx context.Context) (*ClusterStatus, error)
}

// LearningService 学习监控服务。
type LearningService interface {
	Stats(ctx context.Context) (*LearningStats, error)
	Capabilities(ctx context.Context) ([]Capability, error)
	PipelineStats(ctx context.Context) (*PipelineStats, error)
	CapabilityHistory(ctx context.Context) ([]CapabilityHistory, error)
}

// MarketplaceService 模板市场服务。
type MarketplaceService interface {
	SearchTemplates(ctx context.Context, query, category string) ([]AgentTemplate, error)
	Deploy(ctx context.Context, templateID string) (Deployment, error)
	ListDeployments(ctx context.Context) ([]Deployment, error)
	StopDeployment(ctx context.Context, id string) error
	StartDeployment(ctx context.Context, id string) error
}

// AutonomyService 自治监控服务（可注入 internal/agent/autonomy 运行时）。
type AutonomyService interface {
	// Goals 列出自治目标（新→旧）
	Goals(ctx context.Context) ([]AutonomyGoal, error)
	// Alerts 列出最近告警（新→旧）
	Alerts(ctx context.Context) ([]AutonomyAlert, error)
}

// SkillService 技能库服务（可注入 internal/agent/skills 技能库）。
type SkillService interface {
	// List 列出全部技能
	List(ctx context.Context) ([]SkillEntry, error)
}

// RealtimeService 实时会话服务（可注入 internal/agent/realtime 运行时）。
type RealtimeService interface {
	// Sessions 列出活跃实时会话
	Sessions(ctx context.Context) ([]RealtimeSessionInfo, error)
	// Events 列出最近实时事件（新→旧）
	Events(ctx context.Context) ([]RealtimeEventInfo, error)
}

// ===== StudioHandler =====

// StudioHandler 实现 Studio 四面板的 /api/v1/* HTTP 端点。
type StudioHandler struct {
	chaos        ChaosService
	cluster      ClusterService
	learning     LearningService
	marketplace  MarketplaceService
	autonomy     AutonomyService
	skills       SkillService
	realtime     RealtimeService
	autonomyReal bool // 是否注入真实自治运行时（未注入时响应标 X-Data-Source: demo）
	skillsReal   bool
	realtimeReal bool
	// v7.3 整改：基础四面板此前"默认全 demo 但无任何标注"，
	// 补机器可读标识，避免 Studio 被误读为已接入真实引擎。
	chaosReal       bool
	clusterReal     bool
	learningReal    bool
	marketplaceReal bool
	mux             *http.ServeMux
}

// Option 配置 StudioHandler。
type Option func(*StudioHandler)

// WithChaos 注入混沌实验服务（未注入时为 demo 实现，响应标 X-Data-Source: demo）。
func WithChaos(s ChaosService) Option {
	return func(h *StudioHandler) { h.chaos = s; h.chaosReal = true }
}

// WithCluster 注入集群状态服务（未注入时为 demo 实现，响应标 X-Data-Source: demo）。
func WithCluster(s ClusterService) Option {
	return func(h *StudioHandler) { h.cluster = s; h.clusterReal = true }
}

// WithLearning 注入学习监控服务（未注入时为 demo 实现，响应标 X-Data-Source: demo）。
func WithLearning(s LearningService) Option {
	return func(h *StudioHandler) { h.learning = s; h.learningReal = true }
}

// WithMarketplace 注入模板市场服务（未注入时为 demo 实现，响应标 X-Data-Source: demo）。
func WithMarketplace(s MarketplaceService) Option {
	return func(h *StudioHandler) { h.marketplace = s; h.marketplaceReal = true }
}

// WithAutonomy 注入自治监控服务（默认为 demo 空实现，返回空数组并标 X-Data-Source: demo）。
func WithAutonomy(s AutonomyService) Option {
	return func(h *StudioHandler) { h.autonomy = s; h.autonomyReal = true }
}

// WithSkills 注入技能库服务（默认为 demo 空实现，返回空数组并标 X-Data-Source: demo）。
func WithSkills(s SkillService) Option {
	return func(h *StudioHandler) { h.skills = s; h.skillsReal = true }
}

// WithRealtime 注入实时会话服务（默认为 demo 空实现，返回空数组并标 X-Data-Source: demo）。
func WithRealtime(s RealtimeService) Option {
	return func(h *StudioHandler) { h.realtime = s; h.realtimeReal = true }
}

// NewStudioHandler 创建 StudioHandler，默认挂载 demo 实现。
func NewStudioHandler(opts ...Option) *StudioHandler {
	h := &StudioHandler{
		chaos:       newDemoChaos(),
		cluster:     newDemoCluster(),
		learning:    newDemoLearning(),
		marketplace: newDemoMarketplace(),
		autonomy:    newDemoAutonomy(),
		skills:      newDemoSkills(),
		realtime:    newDemoRealtime(),
		mux:         http.NewServeMux(),
	}
	for _, opt := range opts {
		opt(h)
	}
	h.registerRoutes()
	return h
}

// ServeHTTP 实现 http.Handler。
func (h *StudioHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}
