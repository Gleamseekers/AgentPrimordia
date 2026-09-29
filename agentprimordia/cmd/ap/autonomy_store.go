// autonomy_store.go — 自治目标的持久化检查点存储（v7.4 接线）
//
// autonomy.CheckpointStore 此前没有任何生产实现（仅接口 + ResumeManager 包装），
// 本文件提供 JSON 文件实现，使 ap autonomy 的目标状态可跨进程恢复。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/autonomy"
)

// jsonCheckpointStore 基于 JSON 文件的目标检查点存储。
// 每个目标一个文件：<dir>/<goalID>.json，写入采用临时文件 + rename 原子替换。
type jsonCheckpointStore struct {
	dir string
}

func newJSONCheckpointStore(dir string) *jsonCheckpointStore {
	return &jsonCheckpointStore{dir: dir}
}

func (s *jsonCheckpointStore) path(goalID string) (string, error) {
	if strings.ContainsAny(goalID, `/\`) || goalID == "" || goalID == "." || goalID == ".." {
		return "", fmt.Errorf("autonomy: 非法目标 ID %q", goalID)
	}
	return filepath.Join(s.dir, goalID+".json"), nil
}

// SaveCheckpoint 原子写入检查点。
func (s *jsonCheckpointStore) SaveCheckpoint(_ context.Context, cp *autonomy.Checkpoint) error {
	if cp == nil {
		return fmt.Errorf("autonomy: nil checkpoint")
	}
	path, err := s.path(cp.GoalID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("autonomy: 创建检查点目录失败: %w", err)
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("autonomy: 序列化检查点失败: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("autonomy: 写入检查点失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("autonomy: 提交检查点失败: %w", err)
	}
	return nil
}

// LoadCheckpoint 读取指定目标的检查点。
func (s *jsonCheckpointStore) LoadCheckpoint(_ context.Context, goalID string) (*autonomy.Checkpoint, error) {
	path, err := s.path(goalID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("autonomy: 读取检查点 %s 失败: %w", goalID, err)
	}
	var cp autonomy.Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("autonomy: 解析检查点 %s 失败: %w", goalID, err)
	}
	return &cp, nil
}

// ListIncomplete 列出所有未完成目标的检查点。
func (s *jsonCheckpointStore) ListIncomplete(ctx context.Context) ([]*autonomy.Checkpoint, error) {
	all, err := s.listAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*autonomy.Checkpoint, 0, len(all))
	for _, cp := range all {
		if !cp.Completed {
			out = append(out, cp)
		}
	}
	return out, nil
}

// listAll 列出全部检查点（按目标 ID 排序，供 CLI list 使用）。
func (s *jsonCheckpointStore) listAll(_ context.Context) ([]*autonomy.Checkpoint, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*autonomy.Checkpoint{}, nil
		}
		return nil, fmt.Errorf("autonomy: 读取检查点目录失败: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(ids)

	out := make([]*autonomy.Checkpoint, 0, len(ids))
	for _, id := range ids {
		cp, err := s.LoadCheckpoint(context.Background(), id)
		if err != nil {
			continue // 跳过损坏文件，不阻断整体列举
		}
		out = append(out, cp)
	}
	return out, nil
}
