// persistence.go — 模板注册表持久化后端（v7.4 接线）
//
// 背景：TemplateRegistry 原为进程内存 map（template.go），重启即丢；
// 且没有任何落盘/远程来源。本文件提供可注入的持久化后端，
// 远程目录导入见 remote.go。
//
// 设计取舍：internal/persist 未提供通用 KV/文档存储（只有 Checkpoint/
// Failure 等专用接口），故此处用标准库 JSON 文件实现，避免为持久化引入
// 不必要耦合；写入采用"临时文件 + rename"原子替换，避免半写损坏。
package marketplace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// TemplateStore 模板持久化后端。
type TemplateStore interface {
	// Save 全量覆盖保存模板集合。
	Save(templates []*AgentTemplate) error
	// Load 加载模板集合；后端不存在时返回空集合而非错误。
	Load() ([]*AgentTemplate, error)
}

// JSONFileStore 基于 JSON 文件的模板持久化后端（标准库实现，无第三方依赖）。
type JSONFileStore struct {
	path string
	mu   sync.Mutex
}

// NewJSONFileStore 创建 JSON 文件后端。path 为空时 Save 报错、Load 返回空。
func NewJSONFileStore(path string) *JSONFileStore {
	return &JSONFileStore{path: path}
}

// Save 原子写入模板集合（临时文件 + rename）。
func (s *JSONFileStore) Save(templates []*AgentTemplate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.path == "" {
		return fmt.Errorf("marketplace: empty template store path")
	}
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("marketplace: create store dir: %w", err)
		}
	}
	if templates == nil {
		templates = []*AgentTemplate{}
	}
	data, err := json.MarshalIndent(templates, "", "  ")
	if err != nil {
		return fmt.Errorf("marketplace: marshal templates: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("marketplace: write temp store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("marketplace: commit store: %w", err)
	}
	return nil
}

// Load 读取模板集合；文件不存在视为空集合。
func (s *JSONFileStore) Load() ([]*AgentTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.path == "" {
		return []*AgentTemplate{}, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []*AgentTemplate{}, nil
		}
		return nil, fmt.Errorf("marketplace: read store: %w", err)
	}
	if len(data) == 0 {
		return []*AgentTemplate{}, nil
	}
	var out []*AgentTemplate
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("marketplace: parse store: %w", err)
	}
	return out, nil
}
