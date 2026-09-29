// Stability: Stable — 多租户治理、配额限流与策略执行。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/governance"
)

// ===== 租户管理 =====

// Tenant 租户实体，包含 ID、名称、计划、配额和状态
type Tenant = governance.Tenant

// TenantPlan 租户付费计划（free / pro / enterprise）
type TenantPlan = governance.TenantPlan

// TenantStatus 租户状态（active / disabled / archived）
type TenantStatus = governance.TenantStatus

// TenantQuota 租户配额，包含最大 Agent 数、会话数、每日 Token 上限、存储和 QPS
type TenantQuota = governance.TenantQuota

// TenantManager 租户生命周期管理器（创建/查询/更新/删除/API Key 绑定）
type TenantManager = governance.TenantManager

const (
	// PlanFree 免费计划
	PlanFree = governance.PlanFree
	// PlanPro 专业计划
	PlanPro = governance.PlanPro
	// PlanEnterprise 企业计划
	PlanEnterprise = governance.PlanEnterprise
)

const (
	// TenantActive 租户活跃
	TenantActive = governance.TenantActive
	// TenantDisabled 租户已禁用
	TenantDisabled = governance.TenantDisabled
	// TenantArchived 租户已归档
	TenantArchived = governance.TenantArchived
)

// NewTenantManager 创建租户管理器
var NewTenantManager = governance.NewTenantManager

// DefaultQuota 根据计划返回默认配额
var DefaultQuota = governance.DefaultQuota

// ===== 配额与限流 =====

// TokenBucket 令牌桶限流器（QPS 控制）
type TokenBucket = governance.TokenBucket

// QuotaManager 单租户配额管理器（QPS/Token/Agent/Session 配额跟踪）
type QuotaManager = governance.QuotaManager

// ResourceManager 多租户配额统一管理器
type ResourceManager = governance.ResourceManager

// NewTokenBucket 创建令牌桶（rate=每秒允许请求数，burst=最大突发量）
var NewTokenBucket = governance.NewTokenBucket

// NewQuotaManager 为指定租户创建配额管理器
var NewQuotaManager = governance.NewQuotaManager

// NewResourceManager 创建多租户资源管理器
var NewResourceManager = governance.NewResourceManager

// ===== 策略执行 =====

// Policy 策略定义（YAML 描述 → 运行时强制执行）
type Policy = governance.Policy

// PolicyMetadata 策略元信息
type PolicyMetadata = governance.PolicyMetadata

// PolicySpec 策略规约（tool限制/成本限制/输出护栏/行为约束）
type PolicySpec = governance.PolicySpec

// ToolRestriction 单tool限制规则
type ToolRestriction = governance.ToolRestriction

// CostLimits 成本限制（美元）
type CostLimits = governance.CostLimits

// OutputGuardrail 输出护栏配置
type OutputGuardrail = governance.OutputGuardrail

// BehaviorConstraints 行为约束配置
type BehaviorConstraints = governance.BehaviorConstraints

// Enforcer 策略执行接口（由 agent 包依赖）
type Enforcer = governance.Enforcer

// EnforcerSnapshot 执行器运行时快照
type EnforcerSnapshot = governance.EnforcerSnapshot

// PolicyEnforcer 策略执行器（运行时强制）
type PolicyEnforcer = governance.PolicyEnforcer

// NewPolicyEnforcer 创建策略执行器
var NewPolicyEnforcer = governance.NewPolicyEnforcer

// NewPolicyEnforcerWithMetrics 创建带可观测性指标的策略执行器
var NewPolicyEnforcerWithMetrics = governance.NewPolicyEnforcerWithMetrics

// NewPolicyEnforcerWithAudit 创建带审计日志的策略执行器
var NewPolicyEnforcerWithAudit = governance.NewPolicyEnforcerWithAudit

// LoadPolicy 从 YAML 字节流加载策略
var LoadPolicy = governance.LoadPolicy

// LoadPolicyFile 从文件路径加载策略
var LoadPolicyFile = governance.LoadPolicyFile

// ===== 审计日志 =====

// AuditLogger 审计日志接口
type AuditLogger = governance.AuditLogger

// AuditEvent 审计日志事件
type AuditEvent = governance.AuditEvent

// AuditEventType 审计事件类型
type AuditEventType = governance.AuditEventType

// AuditQuery 审计日志查询条件
type AuditQuery = governance.AuditQuery

// AlertCallback 告警回调函数
type AlertCallback = governance.AlertCallback

// FileAuditLogger 文件审计日志（JSONL 格式 + 轮转）
type FileAuditLogger = governance.FileAuditLogger

const (
	// AuditToolCallBlocked tool调用被拦截
	AuditToolCallBlocked = governance.AuditToolCallBlocked
	// AuditCostExceeded 成本超限
	AuditCostExceeded = governance.AuditCostExceeded
	// AuditOutputBlocked 输出被拦截
	AuditOutputBlocked = governance.AuditOutputBlocked
	// AuditPIIDetected PII 检测触发
	AuditPIIDetected = governance.AuditPIIDetected
	// AuditPolicyViolation 策略违规
	AuditPolicyViolation = governance.AuditPolicyViolation
	// AuditPolicyLoaded 策略加载
	AuditPolicyLoaded = governance.AuditPolicyLoaded
	// AuditPolicyHotSwapped 策略热替换
	AuditPolicyHotSwapped = governance.AuditPolicyHotSwapped
)

// NewFileAuditLogger 创建文件审计日志
var NewFileAuditLogger = governance.NewFileAuditLogger

// ===== 可观测性指标 =====

// GovernanceMetrics Governance 可观测性指标（线程安全）
type GovernanceMetrics = governance.GovernanceMetrics

// NewGovernanceMetrics 创建 Governance 指标收集器
var NewGovernanceMetrics = governance.NewGovernanceMetrics

// ===== 租户隔离（Context 注入） =====

// WithTenant 将 tenantID 注入 context
var WithTenant = governance.WithTenant

// TenantFromContext 从 context 中提取 tenantID
var TenantFromContext = governance.TenantFromContext

// RequireTenant 从 context 中提取 tenantID，不存在则返回错误
var RequireTenant = governance.RequireTenant

// ===== 错误变量 =====

var (
	// ErrToolCallLimitExceeded tool调用次数超过上限
	ErrToolCallLimitExceeded = governance.ErrToolCallLimitExceeded
	// ErrCostLimitExceeded 成本超过策略上限
	ErrCostLimitExceeded = governance.ErrCostLimitExceeded
	// ErrBlockedArgument tool参数命中禁止模式
	ErrBlockedArgument = governance.ErrBlockedArgument
	// ErrOutputTooLong 输出长度超过策略上限
	ErrOutputTooLong = governance.ErrOutputTooLong
	// ErrPIIDetected 输出触发 strict PII 过滤
	ErrPIIDetected = governance.ErrPIIDetected
	// ErrPolicyNotFound 未加载策略
	ErrPolicyNotFound = governance.ErrPolicyNotFound
	// ErrNoTenantInContext context 中未携带租户信息
	ErrNoTenantInContext = governance.ErrNoTenantInContext
)
