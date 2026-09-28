package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestMarketplaceCatalogAddPersistsAndLists 生产路径回归：
// CLI 必须真实构造带持久化的 TemplateRegistry 并落盘，
// 使 agent/marketplace 的持久化能力不再"有实现无构造点"。
func TestMarketplaceCatalogAddPersistsAndLists(t *testing.T) {
	oldDir := marketplaceDir
	marketplaceDir = t.TempDir()
	defer func() { marketplaceDir = oldDir }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"templates":[{"id":"t1","name":"T1","version":"1.0.0","author":"a","system_prompt":"p"}]}`))
	}))
	defer srv.Close()

	if err := runMarketplaceCatalog([]string{"add", srv.URL}); err != nil {
		t.Fatalf("catalog add 失败: %v", err)
	}

	// 落盘校验
	regPath := filepath.Join(marketplaceDir, "registry.json")
	if _, err := os.Stat(regPath); err != nil {
		t.Fatalf("模板目录未持久化到 %s: %v", regPath, err)
	}

	// 新实例（模拟重启）应能加载
	reg := newMarketplaceRegistry()
	if err := reg.LoadError(); err != nil {
		t.Fatalf("重启加载失败: %v", err)
	}
	if tmpl, ok := reg.Get("t1"); !ok || tmpl.Name != "T1" {
		t.Fatalf("重启后未恢复模板: %+v", tmpl)
	}

	// list 子命令不应报错
	if err := runMarketplaceCatalog([]string{"list"}); err != nil {
		t.Fatalf("catalog list 失败: %v", err)
	}

	// remove 应持久化
	if err := runMarketplaceCatalog([]string{"remove", "t1"}); err != nil {
		t.Fatalf("catalog remove 失败: %v", err)
	}
	reg2 := newMarketplaceRegistry()
	if _, ok := reg2.Get("t1"); ok {
		t.Error("移除后重启仍能读到模板——未持久化")
	}
}

// TestMarketplaceCatalogRejectsBadSignature 验签失败时 CLI 必须报错且不落盘模板。
func TestMarketplaceCatalogRejectsBadSignature(t *testing.T) {
	oldDir := marketplaceDir
	marketplaceDir = t.TempDir()
	defer func() { marketplaceDir = oldDir }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"templates":[{"id":"t1","name":"T1","version":"1.0.0","author":"a","system_prompt":"p"}]}`))
	}))
	defer srv.Close()

	err := runMarketplaceCatalog([]string{"add", srv.URL, "--signature", "AAAA", "--public-key", "not-a-pem"})
	if err == nil {
		t.Fatal("无效验签参数应导致导入失败")
	}
	reg := newMarketplaceRegistry()
	if _, ok := reg.Get("t1"); ok {
		t.Error("验签失败不应写入模板")
	}
}
