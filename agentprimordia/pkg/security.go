// Stability: Stable — ACL 与沙箱安全防护。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/security"
)

// ACL 是访问控制列表，管理 Agent 对资源的访问权限
type ACL = security.ACL

// ACLRule 是 ACL 规则，定义 Agent 对特定资源的访问级别
type ACLRule = security.ACLRule

// AccessLevel 是访问级别（none / read / write / execute / all）
type AccessLevel = security.AccessLevel

// Sandbox 是沙箱环境，限制 Agent 可执行的命令和可访问的路径
type Sandbox = security.Sandbox

// PermissionLevel 是统一权限系统的权限级别
type PermissionLevel = security.PermissionLevel

// PermissionRole 是 Agent 在 PermissionManager 中的角色（别名为 security.Role，避免与 pkg/agent.Role 重名）
type PermissionRole = security.PermissionRole

// PermissionScope 是资源范围约束（别名为 security.ScopePolicy，避免与 pkg/tools.ScopePolicy 重名）
type PermissionScope = security.PermissionScope

// Permission 是统一的权限接口
type Permission = security.Permission

// PermissionManager 是 RBAC + Scope 组合权限管理器
type PermissionManager = security.PermissionManager

const (
	// AccessNone 表示无访问权限
	AccessNone = security.AccessNone
	// AccessRead 表示只读权限
	AccessRead = security.AccessRead
	// AccessWrite 表示读写权限
	AccessWrite = security.AccessWrite
	// AccessExecute 表示执行权限
	AccessExecute = security.AccessExecute
	// AccessAll 表示全部权限
	AccessAll = security.AccessAll

	// PermNone 无权限
	PermNone = security.PermNone
	// PermRead 只读权限
	PermRead = security.PermRead
	// PermWrite 写权限
	PermWrite = security.PermWrite
	// PermExecute 执行权限
	PermExecute = security.PermExecute
	// PermAdmin 管理员权限
	PermAdmin = security.PermAdmin
)

// 错误变量
var (
	ErrAgentNotFound      = security.ErrAgentNotFound
	ErrInvalidPermission  = security.ErrInvalidPermission
	ErrEscalateNotAllowed = security.ErrEscalateNotAllowed
)

// NewACL 创建访问控制列表实例
var NewACL = security.NewACL

// NewSandbox 创建沙箱环境实例
var NewSandbox = security.NewSandbox

// NewPermissionManager 创建统一权限管理器
var NewPermissionManager = security.NewPermissionManager
