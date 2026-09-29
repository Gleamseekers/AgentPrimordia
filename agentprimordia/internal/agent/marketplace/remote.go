// remote.go — 模板目录远程协议（v7.4 接线）
//
// 复用 internal/marketplace 的 cosign 验签（ECDSA P-256 over SHA-256），
// 使模板注册表可以从 HTTPS 端点导入目录；未提供签名/公钥时按纯传输导入，
// 提供任一则强制验签，失败一律拒绝且不产生部分写入。
package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	remotemarket "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/marketplace"
)

// maxCatalogBytes 目录响应体上限，防止超大响应耗尽内存。
const maxCatalogBytes = 4 << 20

// Catalog 远程模板目录（对象形态；亦兼容裸数组）。
type Catalog struct {
	Templates []*AgentTemplate `json:"templates"`
}

// FetchCatalog 从远程端点拉取模板目录并导入注册表，返回导入条数。
//
// 语义：
//   - signatureB64 / publicKeyPEM 任一非空 → 强制 cosign 验签，失败拒绝导入；
//   - 全部模板先校验通过再写入，避免"部分导入"的中间态；
//   - 同 ID 已存在时执行 Update，否则 Register。
func (r *TemplateRegistry) FetchCatalog(ctx context.Context, url, signatureB64, publicKeyPEM string, client *http.Client) (int, error) {
	if url == "" {
		return 0, fmt.Errorf("marketplace: empty catalog url")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("marketplace: new catalog request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("marketplace: fetch catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("marketplace: catalog HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes))
	if err != nil {
		return 0, fmt.Errorf("marketplace: read catalog: %w", err)
	}

	// 验签：以响应体原始字节为签名载荷
	if signatureB64 != "" || publicKeyPEM != "" {
		if err := remotemarket.VerifyCosignSignature(body, signatureB64, publicKeyPEM); err != nil {
			return 0, fmt.Errorf("marketplace: catalog signature verify: %w", err)
		}
	}

	templates, err := parseCatalog(body)
	if err != nil {
		return 0, err
	}
	// 先全部校验，避免部分写入
	for _, tmpl := range templates {
		if tmpl == nil {
			return 0, fmt.Errorf("marketplace: catalog contains nil template")
		}
		if vr := tmpl.Validate(); !vr.Valid {
			return 0, fmt.Errorf("marketplace: catalog template %q invalid: %v", tmpl.ID, vr.Errors)
		}
	}

	imported := 0
	for _, tmpl := range templates {
		if _, exists := r.Get(tmpl.ID); exists {
			if err := r.Update(tmpl); err != nil {
				return imported, err
			}
		} else if err := r.Register(tmpl); err != nil {
			return imported, err
		}
		imported++
	}
	return imported, nil
}

// parseCatalog 解析目录响应：优先对象形态 {"templates":[...]}，兼容裸数组 [...]。
func parseCatalog(body []byte) ([]*AgentTemplate, error) {
	var c Catalog
	if err := json.Unmarshal(body, &c); err == nil && c.Templates != nil {
		return c.Templates, nil
	}
	var arr []*AgentTemplate
	if err := json.Unmarshal(body, &arr); err == nil {
		return arr, nil
	}
	return nil, fmt.Errorf("marketplace: parse catalog: unsupported format")
}
