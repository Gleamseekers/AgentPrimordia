package marketplace

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
)

func genTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	return key
}

func signTestBlob(t *testing.T, key *ecdsa.PrivateKey, payload []byte) string {
	t.Helper()
	digest := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("SignASN1 失败: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func exportTestPubPEM(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("序列化公钥失败: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func catalogBody() string {
	return `{"templates":[{"id":"t1","name":"T1","version":"1.0.0","author":"a","system_prompt":"p"}]}`
}

// TestFetchCatalogRegistersTemplates 无签名时按目录直接导入。
func TestFetchCatalogRegistersTemplates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(catalogBody()))
	}))
	defer srv.Close()

	r := NewTemplateRegistry()
	n, err := r.FetchCatalog(context.Background(), srv.URL, "", "", nil)
	if err != nil {
		t.Fatalf("FetchCatalog 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("导入数 = %d, want 1", n)
	}
	if tmpl, ok := r.Get("t1"); !ok || tmpl.Name != "T1" {
		t.Errorf("模板未正确导入: %+v", tmpl)
	}
}

// TestFetchCatalogAcceptBareArray 兼容裸数组目录格式。
func TestFetchCatalogAcceptBareArray(t *testing.T) {
	body := `[{"id":"t2","name":"T2","version":"1.0.0","author":"a","system_prompt":"p"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	r := NewTemplateRegistry()
	n, err := r.FetchCatalog(context.Background(), srv.URL, "", "", nil)
	if err != nil {
		t.Fatalf("FetchCatalog 失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("导入数 = %d, want 1", n)
	}
}

// TestFetchCatalogValidSignature cosign 验签通过时导入。
func TestFetchCatalogValidSignature(t *testing.T) {
	body := catalogBody()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	key := genTestKey(t)
	sig := signTestBlob(t, key, []byte(body))

	r := NewTemplateRegistry()
	n, err := r.FetchCatalog(context.Background(), srv.URL, sig, exportTestPubPEM(t, key), nil)
	if err != nil {
		t.Fatalf("验签应通过，实际失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("导入数 = %d, want 1", n)
	}
}

// TestFetchCatalogRejectsTamperedSignature 验签失败必须拒绝导入（不得部分写入）。
func TestFetchCatalogRejectsTamperedSignature(t *testing.T) {
	body := catalogBody()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	key := genTestKey(t)
	other := genTestKey(t)
	sig := signTestBlob(t, other, []byte(body)) // 用错误的密钥签名

	r := NewTemplateRegistry()
	if _, err := r.FetchCatalog(context.Background(), srv.URL, sig, exportTestPubPEM(t, key), nil); err == nil {
		t.Fatal("错误密钥签名应被拒绝")
	}
	if len(r.List()) != 0 {
		t.Errorf("验签失败后不得写入任何模板，实际 %d", len(r.List()))
	}
}

// TestFetchCatalogNon200 非 200 响应返回错误。
func TestFetchCatalogNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := NewTemplateRegistry()
	if _, err := r.FetchCatalog(context.Background(), srv.URL, "", "", nil); err == nil {
		t.Fatal("HTTP 500 应返回错误")
	}
}
