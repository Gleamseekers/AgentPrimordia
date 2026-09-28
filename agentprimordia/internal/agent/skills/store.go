package skills

import "sync"

// Store 技能库：技能存取（默认内存实现；v7.4 起可注入持久化后端）。
type Store struct {
	mu     sync.RWMutex
	skills map[string]*Skill

	persistStore PersistStore
	persistErr   error
}

// StoreOption 配置 Store。
type StoreOption func(*Store)

// WithPersistence 注入持久化后端：构造时自动加载，Save/Delete 后落盘。
func WithPersistence(p PersistStore) StoreOption {
	return func(s *Store) { s.persistStore = p }
}

// NewStore 创建技能库。未传 WithPersistence 时行为与历史一致（纯内存）。
func NewStore(opts ...StoreOption) *Store {
	s := &Store{skills: make(map[string]*Skill)}
	for _, opt := range opts {
		opt(s)
	}
	if s.persistStore != nil {
		_ = s.Reload()
	}
	return s
}

// PersistError 返回最近一次加载/落盘错误（未注入持久化或成功时为 nil）。
func (s *Store) PersistError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.persistErr
}

// Reload 从持久化后端重新加载技能（覆盖当前内存内容）。
func (s *Store) Reload() error {
	if s.persistStore == nil {
		return nil
	}
	loaded, err := s.persistStore.Load()
	if err != nil {
		s.mu.Lock()
		s.persistErr = err
		s.mu.Unlock()
		return err
	}
	next := make(map[string]*Skill, len(loaded))
	for _, sk := range loaded {
		if sk != nil && sk.ID != "" {
			next[sk.ID] = sk
		}
	}
	s.mu.Lock()
	s.skills = next
	s.persistErr = nil
	s.mu.Unlock()
	return nil
}

// persist 把当前技能快照写回后端（未注入持久化时空操作）；在锁外调用。
func (s *Store) persist() {
	if s.persistStore == nil {
		return
	}
	s.mu.RLock()
	snapshot := make([]*Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		snapshot = append(snapshot, sk)
	}
	s.mu.RUnlock()

	err := s.persistStore.Save(snapshot)
	s.mu.Lock()
	s.persistErr = err
	s.mu.Unlock()
}

// Save 保存技能
func (s *Store) Save(skill *Skill) {
	s.mu.Lock()
	s.skills[skill.ID] = skill
	s.mu.Unlock()
	s.persist()
}

// Get 获取技能
func (s *Store) Get(id string) (*Skill, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sk, ok := s.skills[id]
	return sk, ok
}

// Delete 删除技能
func (s *Store) Delete(id string) {
	s.mu.Lock()
	delete(s.skills, id)
	s.mu.Unlock()
	s.persist()
}

// List 列出所有技能
func (s *Store) List() []*Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		result = append(result, sk)
	}
	return result
}

// ListActive 列出所有活跃技能
func (s *Store) ListActive() []*Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Skill
	for _, sk := range s.skills {
		if sk.Status == SkillActive {
			result = append(result, sk)
		}
	}
	return result
}

// FindByName 按名称查找
func (s *Store) FindByName(name string) (*Skill, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sk := range s.skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return nil, false
}

// Count 返回技能总数
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.skills)
}
