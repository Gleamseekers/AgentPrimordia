// Stability: Stable — v3.0.0 新增分布式集群协调能力，经充分测试验证，API 已冻结。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/cluster"
)

// ===== 集群管理 =====

// ClusterManager 集群管理器，管理节点加入/离开/心跳/选举
type ClusterManager = cluster.ClusterManager

// ClusterConfig 集群配置
type ClusterConfig = cluster.ClusterConfig

// ClusterNodeInfo 节点信息
type ClusterNodeInfo = cluster.NodeInfo

// ClusterNodeRole 节点角色
type ClusterNodeRole = cluster.NodeRole

// ClusterNodeStatus 节点状态
type ClusterNodeStatus = cluster.NodeStatus

const (
	// ClusterRoleFollower 跟随者角色
	ClusterRoleFollower = cluster.RoleFollower
	// ClusterRoleLeader 领导者角色
	ClusterRoleLeader = cluster.RoleLeader
	// ClusterRoleCandidate 候选者角色
	ClusterRoleCandidate = cluster.RoleCandidate

	// ClusterStatusOnline 节点在线
	ClusterStatusOnline = cluster.StatusOnline
	// ClusterStatusOffline 节点离线
	ClusterStatusOffline = cluster.StatusOffline
	// ClusterStatusLeaving 节点正在离开
	ClusterStatusLeaving = cluster.StatusLeaving
)

var (
	// NewClusterManager 创建集群管理器
	NewClusterManager = cluster.NewClusterManager
	// ClusterConfigWithDefaults 填充集群配置默认值
	ClusterConfigWithDefaults = cluster.ClusterConfigWithDefaults
)

// ===== 一致性哈希 =====

// ConsistentHash 一致性哈希环，用于将 Agent/任务分片到不同节点
type ConsistentHash = cluster.ConsistentHash

var (
	// NewConsistentHash 创建一致性哈希环
	NewConsistentHash = cluster.NewConsistentHash
)

// ===== 分布式状态 =====

// DistributedState 分布式 KV 状态存储，提供带 TTL 的状态管理
type DistributedState = cluster.DistributedState

// ClusterRemoteEntry 远程状态条目（用于节点间同步）
type ClusterRemoteEntry = cluster.RemoteEntry

var (
	// NewDistributedState 创建分布式状态存储
	NewDistributedState = cluster.NewDistributedState
)

// ===== 分布式服务发现 =====

// KVStore 分布式 KV 存储接口（支撑分布式服务发现）
type KVStore = cluster.KVStore

// KVEvent KV 变化事件
type KVEvent = cluster.KVEvent

// MemKVStore 内存 KV 存储（测试和单节点模式用）
type MemKVStore = cluster.MemKVStore

// DistributedDiscovery 分布式服务发现（基于 KV 存储）
type DistributedDiscovery = cluster.DistributedDiscovery

// DistributedDiscoveryConfig 分布式发现配置
type DistributedDiscoveryConfig = cluster.DistributedDiscoveryConfig

var (
	// NewMemKVStore 创建内存 KV 存储
	NewMemKVStore = cluster.NewMemKVStore
	// NewDistributedDiscovery 创建分布式服务发现
	NewDistributedDiscovery = cluster.NewDistributedDiscovery
)

// ===== 跨节点消息总线 =====

// RemoteNode 远程节点连接
type RemoteNode = cluster.RemoteNode

// RemoteMessageBus 跨节点消息总线
type RemoteMessageBus = cluster.RemoteMessageBus

// RemoteBusConfig 远程消息总线配置
type RemoteBusConfig = cluster.RemoteBusConfig

// RemoteBusStats 远程消息总线统计（内部使用，含 atomic 字段）
type RemoteBusStats = cluster.RemoteBusStats

// RemoteBusStatsSnapshot 远程消息总线统计快照（值安全，GetStats 返回此类型）
type RemoteBusStatsSnapshot = cluster.RemoteBusStatsSnapshot

var (
	// NewRemoteNode 创建远程节点
	NewRemoteNode = cluster.NewRemoteNode
	// NewRemoteMessageBus 创建跨节点消息总线
	NewRemoteMessageBus = cluster.NewRemoteMessageBus
)
