package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestFileBasedMarket_Create(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("NewFileBasedMarket 失败: %v", err)
	}
	if market.BaseDir() != dir {
		t.Fatalf("BaseDir = %q, want %q", market.BaseDir(), dir)
	}
}

func TestFileBasedMarket_Publish(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	manifest := &PluginManifest{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Description: "A test plugin",
		Author:      "test-author",
		Category:    "data",
		Tags:        []string{"test", "data"},
		MinVersion:  "1.0.0",
	}
	if err := market.Publish(manifest); err != nil {
		t.Fatalf("Publish 失败: %v", err)
	}
	manifestPath := filepath.Join(dir, "test-plugin", "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Fatal("manifest.json 未写入")
	}
}

func TestFileBasedMarket_PublishEmptyName(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	manifest := &PluginManifest{Name: "", Version: "1.0.0"}
	err = market.Publish(manifest)
	if err == nil {
		t.Fatal("空名称应返回错误")
	}
}

func TestFileBasedMarket_GetManifest(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	_ = market.Publish(&PluginManifest{Name: "my-plugin", Version: "2.0.0", Category: "ai"})
	got, err := market.GetManifest("my-plugin")
	if err != nil {
		t.Fatalf("GetManifest 失败: %v", err)
	}
	if got.Name != "my-plugin" {
		t.Fatalf("Name = %q, want %q", got.Name, "my-plugin")
	}
}

func TestFileBasedMarket_GetManifestNotFound(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	_, err = market.GetManifest("nonexistent")
	if err == nil {
		t.Fatal("获取不存在的插件应返回错误")
	}
}

func TestFileBasedMarket_Search(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	_ = market.Publish(&PluginManifest{Name: "data-tool", Version: "1.0.0", Category: "data", Description: "data processing"})
	_ = market.Publish(&PluginManifest{Name: "ai-tool", Version: "1.0.0", Category: "ai", Description: "AI model"})
	_ = market.Publish(&PluginManifest{Name: "devops-tool", Version: "1.0.0", Category: "devops", Description: "deployment"})

	results, err := market.Search("data", "")
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("data 类别搜索结果 = %d, want 1", len(results))
	}

	results, err = market.Search("", "AI")
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("AI 关键词搜索结果 = %d, want 1", len(results))
	}

	results, err = market.Search("", "")
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("全部搜索结果 = %d, want 3", len(results))
	}
}

func TestFileBasedMarket_Download(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	pluginDir := filepath.Join(dir, "test-plugin")
	_ = os.MkdirAll(pluginDir, 0755)
	pluginData := []byte("fake plugin data")
	_ = os.WriteFile(filepath.Join(pluginDir, "1.0.0.tar.gz"), pluginData, 0644)
	sum := sha256.Sum256(pluginData)
	checksum := hex.EncodeToString(sum[:])
	_ = market.Publish(&PluginManifest{Name: "test-plugin", Version: "1.0.0", Category: "data", Checksum: checksum})

	data, err := market.Download(context.Background(), "test-plugin", "1.0.0")
	if err != nil {
		t.Fatalf("Download 失败: %v", err)
	}
	if string(data) != string(pluginData) {
		t.Fatal("下载数据不匹配")
	}
}

func TestFileBasedMarket_DownloadChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	pluginDir := filepath.Join(dir, "test-plugin")
	_ = os.MkdirAll(pluginDir, 0755)
	_ = os.WriteFile(filepath.Join(pluginDir, "1.0.0.tar.gz"), []byte("data"), 0644)
	_ = market.Publish(&PluginManifest{Name: "test-plugin", Version: "1.0.0", Category: "data", Checksum: "invalidchecksum"})

	_, err = market.Download(context.Background(), "test-plugin", "1.0.0")
	if err == nil {
		t.Fatal("校验和不匹配应返回错误")
	}
}

func TestFileBasedMarket_Install(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	_ = market.Publish(&PluginManifest{Name: "test-plugin", Version: "1.0.0", Category: "data"})
	err = market.Install(context.Background(), "test-plugin", "1.0.0")
	if err != nil {
		t.Fatalf("Install 失败: %v", err)
	}
}

func TestFileBasedMarket_InstallDependencyMissing(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	_ = market.Publish(&PluginManifest{
		Name:     "test-plugin",
		Version:  "1.0.0",
		Category: "data",
		Dependencies: []PluginDependency{
			{Name: "missing-dep", Version: "1.0.0"},
		},
	})
	err = market.Install(context.Background(), "test-plugin", "1.0.0")
	if err == nil {
		t.Fatal("缺少依赖应返回错误")
	}
}

func TestFileBasedMarket_List(t *testing.T) {
	dir := t.TempDir()
	market, err := NewFileBasedMarket(dir)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	_ = market.Publish(&PluginManifest{Name: "a", Version: "1.0.0", Category: "data"})
	_ = market.Publish(&PluginManifest{Name: "b", Version: "1.0.0", Category: "ai"})
	list, err := market.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List = %d, want 2", len(list))
	}
}

func TestPluginInstaller_Create(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, err := NewPluginInstaller(dir, market, loader)
	if err != nil {
		t.Fatalf("NewPluginInstaller 失败: %v", err)
	}
	if installer.InstallDir() != dir {
		t.Fatalf("InstallDir = %q, want %q", installer.InstallDir(), dir)
	}
}

func TestPluginInstaller_InstallFromMarket(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	pluginData := []byte("fake plugin binary")
	sum := sha256.Sum256(pluginData)
	checksum := hex.EncodeToString(sum[:])
	pluginDir := filepath.Join(marketDir, "my-plugin")
	_ = os.MkdirAll(pluginDir, 0755)
	_ = os.WriteFile(filepath.Join(pluginDir, "1.0.0.tar.gz"), pluginData, 0644)
	_ = market.Publish(&PluginManifest{
		Name: "my-plugin", Version: "1.0.0", Category: "data", Checksum: checksum,
		Signature: "c2lnbmF0dXJl", PublicKey: "PUBKEY",
	})

	installer, _ := NewPluginInstaller(dir, market, loader)
	installer.WithVerifier(okVerifier)
	err := installer.InstallFromMarket(context.Background(), "my-plugin", "1.0.0")
	if err != nil {
		t.Fatalf("InstallFromMarket 失败: %v", err)
	}
	installed, err := installer.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled 失败: %v", err)
	}
	if len(installed) != 1 || installed[0] != "my-plugin" {
		t.Fatalf("已安装插件 = %v, want [my-plugin]", installed)
	}
}

func TestPluginInstaller_Uninstall(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	installer, _ := NewPluginInstaller(dir, market, loader)
	_ = os.MkdirAll(filepath.Join(dir, "test-plugin"), 0755)
	err := installer.Uninstall("test-plugin")
	if err != nil {
		t.Fatalf("Uninstall 失败: %v", err)
	}
	installed, _ := installer.ListInstalled()
	if len(installed) != 0 {
		t.Fatalf("卸载后应无插件，实际: %v", installed)
	}
}

func TestPluginInstaller_VerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	marketDir := t.TempDir()
	market, _ := NewFileBasedMarket(marketDir)
	loader := NewPluginLoader(NewRegistry())
	pluginData := []byte("test data")
	sum := sha256.Sum256(pluginData)
	checksum := hex.EncodeToString(sum[:])
	pluginDir := filepath.Join(marketDir, "test-plugin")
	_ = os.MkdirAll(pluginDir, 0755)
	_ = os.WriteFile(filepath.Join(pluginDir, "1.0.0.tar.gz"), pluginData, 0644)
	_ = market.Publish(&PluginManifest{
		Name: "test-plugin", Version: "1.0.0", Category: "data", Checksum: checksum,
		Signature: "c2lnbmF0dXJl", PublicKey: "PUBKEY",
	})
	installer, _ := NewPluginInstaller(dir, market, loader)
	installer.WithVerifier(okVerifier)
	if err := installer.InstallFromMarket(context.Background(), "test-plugin", "1.0.0"); err != nil {
		t.Fatalf("InstallFromMarket 失败: %v", err)
	}
	err := installer.VerifyChecksum("test-plugin", "1.0.0")
	if err != nil {
		t.Fatalf("VerifyChecksum 失败: %v", err)
	}
}
