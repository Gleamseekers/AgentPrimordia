package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PluginManifest 插件清单（市场元数据）
type PluginManifest struct {
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	Description  string             `json:"description"`
	Author       string             `json:"author"`
	Category     string             `json:"category"` // "data"/"ai"/"devops"
	Tags         []string           `json:"tags"`
	Capabilities []string           `json:"capabilities"`
	Dependencies []PluginDependency `json:"dependencies"`
	MinVersion   string             `json:"min_version"` // 最低 SDK 版本
	Checksum     string             `json:"checksum"`    // SHA256
	// Signature cosign 对工件的 base64 签名（ECDSA P-256 over SHA-256，
	// 与 marketplace.Manifest 同格式）。P1 起安装前置必填。
	Signature string `json:"signature,omitempty"`
	// PublicKey 发布方公钥（PEM，ECDSA P-256）。P1 起安装前置必填。
	PublicKey string `json:"public_key,omitempty"`
}

// PluginDependency 插件依赖
type PluginDependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// MarketInterface 插件市场接口
type MarketInterface interface {
	Search(category, query string) ([]PluginManifest, error)
	GetManifest(name string) (*PluginManifest, error)
	Download(ctx context.Context, name, version string) ([]byte, error)
	Install(ctx context.Context, name, version string) error
}

// FileBasedMarket 基于本地文件系统的插件市场实现
type FileBasedMarket struct {
	baseDir string
	index   map[string]*PluginManifest
	mu      sync.RWMutex
}

// NewFileBasedMarket 创建基于文件系统的插件市场
func NewFileBasedMarket(baseDir string) (*FileBasedMarket, error) {
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		baseDir = filepath.Join(home, ".agentprimordia", "plugins")
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("tools: cannot create market dir: %w", err)
	}

	m := &FileBasedMarket{
		baseDir: baseDir,
		index:   make(map[string]*PluginManifest),
	}

	m.loadIndex()
	return m, nil
}

func (m *FileBasedMarket) loadIndex() {
	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(m.baseDir, entry.Name(), "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var manifest PluginManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue
		}
		m.index[entry.Name()] = &manifest
	}
}

// Search 根据类别和关键词搜索插件
func (m *FileBasedMarket) Search(category, query string) ([]PluginManifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []PluginManifest
	for _, manifest := range m.index {
		if category != "" && !strings.EqualFold(manifest.Category, category) {
			continue
		}
		if query != "" {
			queryLower := strings.ToLower(query)
			matched := strings.Contains(strings.ToLower(manifest.Name), queryLower) ||
				strings.Contains(strings.ToLower(manifest.Description), queryLower)
			if !matched {
				for _, tag := range manifest.Tags {
					if strings.Contains(strings.ToLower(tag), queryLower) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		results = append(results, *manifest)
	}
	return results, nil
}

// GetManifest 获取插件清单
func (m *FileBasedMarket) GetManifest(name string) (*PluginManifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	manifest, ok := m.index[name]
	if !ok {
		return nil, fmt.Errorf("tools: plugin %q not found in market", name)
	}
	return manifest, nil
}

// Download 下载指定版本的插件
func (m *FileBasedMarket) Download(ctx context.Context, name, version string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pluginFile := filepath.Join(m.baseDir, name, version+".tar.gz")
	data, err := os.ReadFile(pluginFile)
	if err != nil {
		return nil, fmt.Errorf("tools: download %s@%s failed: %w", name, version, err)
	}

	manifest, ok := m.index[name]
	if ok && manifest.Checksum != "" {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != manifest.Checksum {
			return nil, fmt.Errorf("tools: checksum mismatch for %s@%s", name, version)
		}
	}
	_ = ctx
	return data, nil
}

// Install 安装插件到本地
func (m *FileBasedMarket) Install(ctx context.Context, name, version string) error {
	manifest, err := m.GetManifest(name)
	if err != nil {
		return err
	}

	for _, dep := range manifest.Dependencies {
		if _, err := m.GetManifest(dep.Name); err != nil {
			return fmt.Errorf("tools: dependency %q not found", dep.Name)
		}
	}

	if manifest.MinVersion != "" {
		constraint := &SemVerConstraint{MinVersion: manifest.MinVersion}
		if !constraint.IsSatisfied("1.0.0") {
			return fmt.Errorf("tools: plugin %q requires SDK >= %s", name, manifest.MinVersion)
		}
	}
	_ = ctx
	_ = version
	return nil
}

// Publish 向市场发布插件
func (m *FileBasedMarket) Publish(manifest *PluginManifest) error {
	if manifest.Name == "" || manifest.Version == "" {
		return ErrInvalidConfig
	}
	if manifest.Category == "" {
		manifest.Category = "data"
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	pluginDir := filepath.Join(m.baseDir, manifest.Name)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("tools: cannot create plugin dir: %w", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("tools: marshal manifest: %w", err)
	}
	manifestPath := filepath.Join(pluginDir, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("tools: write manifest: %w", err)
	}

	m.index[manifest.Name] = manifest
	return nil
}

// BaseDir 返回市场基础目录
func (m *FileBasedMarket) BaseDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.baseDir
}

// List 列出市场中所有插件
func (m *FileBasedMarket) List() ([]PluginManifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []PluginManifest
	for _, manifest := range m.index {
		results = append(results, *manifest)
	}
	return results, nil
}
