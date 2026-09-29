// types.go — v6.5「结社」联邦层核心类型（V7 路线图 §七）
//
// 本质跃迁：常驻个体联结为社会——跨节点组织（Blackboard × 节点总线 ×
// 租约）+ 联邦进化（流通蒸馏 adapter、签名工具、世界模型断言摘要——
// 流通学习成果而非原始数据）+ 社会信任层（贡献声誉，投毒对抗）。
//
// 工程地板边界：本包实现**协议与信任机制**（确定性可测）；节点间真实
// 传输复用既有 cluster/grpc_bus 与 A2A 面（B3 三节点集群为运营依赖，
// 命题 1 数字降级豁免中）。
package federation

import "time"

// NodeID 节点标识。
type NodeID string

// AssetKind 联邦资产三形态（路线图 §七工程地板）。
type AssetKind string

const (
	// AssetSkillCard 技能卡（轨迹蒸馏出的窄域能力卡）。
	AssetSkillCard AssetKind = "skill-card"
	// AssetToolPackage 签名工具包（生命周期 signed_registered 产物的流通形态）。
	AssetToolPackage AssetKind = "tool-package"
	// AssetModelAdapter 蒸馏模型 adapter（v6.2 管道产物的流通形态——
	// 流通学习成果而非原始数据：只传 adapter 引用与指标，不传轨迹）。
	AssetModelAdapter AssetKind = "model-adapter"
)

// AssetEnvelope 联邦资产信封（流通学习成果的签名载体）。
type AssetEnvelope struct {
	Kind        AssetKind `json:"kind"`
	AssetID     string    `json:"asset_id"`      // 资产 ID（kind 内唯一）
	OriginNode  NodeID    `json:"origin_node"`   // 产出节点
	PayloadSHA  string    `json:"payload_sha"`   // 载荷 sha256（完整性锚定）
	Signature   string    `json:"signature"`     // 产出节点签名（cosign 同款口径）
	SignerKeyID string    `json:"signer_key_id"` // 签名钥指纹（信任层判定）
	Provenance  []NodeID  `json:"provenance"`    // 溯源链（origin → 中继节点序）
	Version     int       `json:"version"`       // 资产版本（演进追踪）
	CreatedAt   time.Time `json:"created_at"`
}

// Claim 认领记录（跨节点黑板；CAS 版本防脏认领）。
type Claim struct {
	TaskID     string    `json:"task_id"`
	Holder     NodeID    `json:"holder"`
	Version    int64     `json:"version"`     // CAS 版本（每次转移 +1）
	LeaseUntil time.Time `json:"lease_until"` // 租约到期（分区容错：到期自动可重认领）
}

// TrustEvent 信任层事件（贡献/违规）。
type TrustEvent struct {
	Node   NodeID    `json:"node"`
	Kind   string    `json:"kind"` // contribute / asset_rejected / poison_attempt / forgery_attempt / false_positive（隔离区节点投递的合格资产——误拦）
	Detail string    `json:"detail"`
	Weight float64   `json:"weight"` // 事件权重（正负皆可）
	At     time.Time `json:"at"`
}
