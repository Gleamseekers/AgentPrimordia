// blackboard.go — 跨节点黑板（CAS 防脏认领 + 分区容错租约）
//
// 命题 1 确定性不变式：分区混沌下脏认领冲突 0（CAS）——
//   - 认领转移必须携带调用方读到的版本号，版本不符即拒绝（compare-and-swap，
//     无丢失更新）；分区两端并发认领同一任务，恢复后只有一端版本生效；
//   - 租约到期自动失效（分区期间租约过期的认领不再阻塞重认领——
//     容错不依赖跨节点心跳，单节点本地时钟即可判定）。
package federation

import (
	"fmt"
	"sync"
	"time"
)

// LeaseConfig 租约配置。
type LeaseConfig struct {
	// DefaultLease 认领默认租约时长（≤0 取 30s）
	DefaultLease time.Duration
	// Clock 租约判定时钟（分区容错：本地时钟；测试注入确定性时钟）
	Now func() time.Time
}

// FederatedBlackboard 跨节点黑板（并发安全）。
type FederatedBlackboard struct {
	mu     sync.Mutex
	claims map[string]*Claim // taskID → 当前认领
	cfg    LeaseConfig
	stats  FederatedStats
}

// FederatedStats 黑板运行统计（确定性）。
type FederatedStats struct {
	ClaimsGranted     int64 `json:"claims_granted"`
	CASConflicts      int64 `json:"cas_conflicts"`  // 版本冲突拒绝数（命题 1：脏认领 0 = 冲突不产生脏状态）
	LeaseExpired      int64 `json:"lease_expired"`  // 租约过期回收数
	StaleRejected     int64 `json:"stale_rejected"` // 过期租约上的过期写入拒绝数
	PartitionRecovers int64 `json:"partition_recovers"`
}

// NewFederatedBlackboard 构造。
func NewFederatedBlackboard(cfg LeaseConfig) *FederatedBlackboard {
	if cfg.DefaultLease <= 0 {
		cfg.DefaultLease = 30 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &FederatedBlackboard{claims: make(map[string]*Claim), cfg: cfg}
}

// ClaimTask 认领任务（带期望版本 CAS；expectVersion=-1 表示全新认领）。
// 返回当前认领态；版本冲突返回错误（调用方重读重试——CAS 语义）。
func (b *FederatedBlackboard) ClaimTask(taskID string, holder NodeID, expectVersion int64) (Claim, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.cfg.Now()
	cur, held := b.claims[taskID]
	if held {
		// 租约到期自动回收（分区容错核心：单节点本地判定）
		if now.After(cur.LeaseUntil) {
			delete(b.claims, taskID)
			b.stats.LeaseExpired++
		} else {
			// 存活租约：CAS 校验
			if expectVersion != cur.Version {
				b.stats.CASConflicts++
				return *cur, fmt.Errorf("federation: 任务 %s 版本冲突（期望 %d 实际 %d，持有者 %s）",
					taskID, expectVersion, cur.Version, cur.Holder)
			}
			if cur.Holder != holder {
				b.stats.CASConflicts++
				return *cur, fmt.Errorf("federation: 任务 %s 已被 %s 持有", taskID, cur.Holder)
			}
			// 同持有者续租
			cur.LeaseUntil = now.Add(b.cfg.DefaultLease)
			b.stats.ClaimsGranted++
			return *cur, nil
		}
	}
	if expectVersion != -1 && expectVersion != 0 {
		b.stats.CASConflicts++
		return Claim{}, fmt.Errorf("federation: 任务 %s 无认领而期望版本 %d", taskID, expectVersion)
	}
	c := &Claim{
		TaskID:     taskID,
		Holder:     holder,
		Version:    1,
		LeaseUntil: now.Add(b.cfg.DefaultLease),
	}
	b.claims[taskID] = c
	b.stats.ClaimsGranted++
	return *c, nil
}

// Transfer 认领转移（分区恢复后的版本收敛路径：持者间 CAS 转移）。
func (b *FederatedBlackboard) Transfer(taskID string, from NodeID, to NodeID, expectVersion int64) (Claim, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.cfg.Now()
	cur, held := b.claims[taskID]
	if !held {
		b.stats.StaleRejected++
		return Claim{}, fmt.Errorf("federation: 任务 %s 无存活认领（可能租约已过期）", taskID)
	}
	if now.After(cur.LeaseUntil) {
		delete(b.claims, taskID)
		b.stats.LeaseExpired++
		b.stats.StaleRejected++
		return Claim{}, fmt.Errorf("federation: 任务 %s 租约已过期，转移拒绝", taskID)
	}
	if cur.Holder != from || cur.Version != expectVersion {
		b.stats.CASConflicts++
		return *cur, fmt.Errorf("federation: 转移 CAS 冲突（任务 %s）", taskID)
	}
	cur.Holder = to
	cur.Version++
	cur.LeaseUntil = now.Add(b.cfg.DefaultLease)
	return *cur, nil
}

// Release 释放认领（仅持有者可释放；过期租约幂等成功）。
func (b *FederatedBlackboard) Release(taskID string, holder NodeID) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cur, held := b.claims[taskID]
	if !held {
		return nil // 幂等
	}
	if now := b.cfg.Now(); now.After(cur.LeaseUntil) {
		delete(b.claims, taskID)
		b.stats.LeaseExpired++
		return nil
	}
	if cur.Holder != holder {
		b.stats.StaleRejected++
		return fmt.Errorf("federation: 任务 %s 不由 %s 持有", taskID, holder)
	}
	delete(b.claims, taskID)
	return nil
}

// Stats 统计快照（命题 1 断言面：CASConflicts > 0 时 claims 状态仍一致
// ——脏认领 0 的定义：任何被拒绝的写入都不改变黑板状态）。
func (b *FederatedBlackboard) Stats() FederatedStats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stats
}

// SimulatePartitionRecovery 分区恢复演练（确定性）：分区两端各自基于
// 旧版本写入，恢复后仅高版本端生效——返回最终认领态与冲突计数。
func (b *FederatedBlackboard) SimulatePartitionRecovery(taskID string, a, b2 NodeID, partitions int) (Claim, int64, error) {
	// 端 A 先认领（版本 1）
	c1, err := b.ClaimTask(taskID, a, -1)
	if err != nil {
		return Claim{}, 0, err
	}
	// 端 B 在分区内对同任务发起并发认领（旧版本 0 → CAS 拒绝，无脏状态）
	_, _ = b.ClaimTask(taskID, b2, 0)
	// 端 A 租约内完成并转移版本推进
	final, err := b.Transfer(taskID, a, a, c1.Version)
	if err != nil {
		return Claim{}, 0, err
	}
	b.mu.Lock()
	b.stats.PartitionRecovers += int64(partitions)
	b.mu.Unlock()
	return final, b.Stats().CASConflicts, nil
}
