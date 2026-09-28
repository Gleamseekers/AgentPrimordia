// persistence.go — 技能库持久化后端（v7.4 接线）
//
// 背景：Store 原为纯进程内存 map（store.go），进程退出即丢失；
// 技能经 acquisition/CLI 习得后无法跨会话复用。
//
// 设计取舍：internal/persist 无通用 KV/文档存储，故用标准库 JSON 文件实现，
// 写入采用"临时文件 + rename"原子替换，避免半写损坏。
package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PersistStore 技能库持久化后端。
type PersistStore interface {
	// Save 全量覆盖保存技能集合。
	Save(skills []*Skill) error
	// Load 加载技能集合；后端不存在时返回空集合而非错误。
	Load() ([]*Skill, error)
}

// JSONFileStore 基于 JSON 文件的技能持久化后端（标准库实现）。
type JSONFileStore struct {
	path string
	mu   sync.Mutex
}

// NewJSONFileStore 创建 JSON 文件后端。path 为空时 Save 报错、Load 返回空。
func NewJSONFileStore(path string) *JSONFileStore {
	return &JSONFileStore{path: path}
}

// Save 原子写入技能集合（临时文件 + rename）。
func (s *JSONFileStore) Save(list []*Skill) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.path == "" {
		return fmt.Errorf("skills: empty persist store path")
	}
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("skills: create store dir: %w", err)
		}
	}
	if list == nil {
		list = []*Skill{}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("skills: marshal skills: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("skills: write temp store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("skills: commit store: %w", err)
	}
	return nil
}

// Load 读取技能集合；文件不存在视为空集合。
func (s *JSONFileStore) Load() ([]*Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.path == "" {
		return []*Skill{}, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Skill{}, nil
		}
		return nil, fmt.Errorf("skills: read store: %w", err)
	}
	if len(data) == 0 {
		return []*Skill{}, nil
	}
	var out []*Skill
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("skills: parse store: %w", err)
	}
	return out, nil
}
