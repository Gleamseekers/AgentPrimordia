package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentmarket "github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/marketplace"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/marketplace"
)

// validateTemplateID 验证模板 ID 安全性
// 防止路径遍历：仅允许字母、数字、连字符、下划线、点
func validateTemplateID(id string) error {
	if id == "" {
		return fmt.Errorf("template ID cannot be empty")
	}
	if strings.Contains(id, "..") || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return fmt.Errorf("invalid template ID %q: path separators and '..' not allowed", id)
	}
	for _, ch := range id {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.') {
			return fmt.Errorf("invalid template ID %q: only alphanumeric, '-', '_', '.' allowed", id)
		}
	}
	return nil
}

// safeTemplatePath 构建安全的模板文件路径
func safeTemplatePath(templateID string) (string, error) {
	if err := validateTemplateID(templateID); err != nil {
		return "", err
	}
	p := filepath.Join(marketplaceDir, templateID+".json")
	// 确保最终路径在 marketplaceDir 内
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	baseAbs, _ := filepath.Abs(marketplaceDir)
	if !strings.HasPrefix(abs, baseAbs) {
		return "", fmt.Errorf("invalid template path: escapes base directory")
	}
	return p, nil
}

// Phase 3.2: CLI 市场管理
//
// 命令：
//   ap marketplace search <query>   搜索 Agent 模板
//   ap marketplace install <id>     安装模板
//   ap marketplace publish          发布模板到市场
//   ap marketplace run <id>         从模板一键启动 Agent

func runMarketplace(args []string) error {
	if len(args) == 0 {
		printMarketplaceHelp()
		return nil
	}

	subcmd := args[0]
	switch subcmd {
	case "search":
		return runMarketplaceSearch(args[1:])
	case "install":
		return runMarketplaceInstall(args[1:])
	case "publish":
		return runMarketplacePublish(args[1:])
	case "run":
		return runMarketplaceRun(args[1:])
	case "list":
		return runMarketplaceList(args[1:])
	case "catalog":
		return runMarketplaceCatalog(args[1:])
	case "--help", "-h", "help":
		printMarketplaceHelp()
		return nil
	default:
		return fmt.Errorf("unknown marketplace subcommand %q, run %s for help", subcmd, bold("ap marketplace --help"))
	}
}

func printMarketplaceHelp() {
	fmt.Print(`ap marketplace — manage Agent templates

Usage:
  ap marketplace <command> [arguments]

Commands:
  search <query>    search agent templates by keyword
  install <id>      install a template locally
  publish           publish a template to the marketplace
  run <id>          deploy and run an agent from template
  list              list installed templates
  catalog add <url>   从远程目录导入模板（可选 --signature/--public-key 验签）
  catalog list        列出本地模板目录（持久化）
  catalog remove <id> 从本地模板目录移除

Options:
  --category CAT    filter by category (research/coding/analysis/chat/automation)
  --json            output in JSON format

Examples:
  ap marketplace search "code review"
  ap marketplace search --category coding
  ap marketplace install tmpl-code-review
  ap marketplace publish
  ap marketplace run tmpl-code-review
  ap marketplace catalog add https://example.com/templates.json
  ap marketplace catalog list
`)
}

// ===== 模板存储 =====

var marketplaceDir = ".ap-templates"

// templateMeta 本地模板元数据
type templateMeta struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Version      string   `json:"version"`
	Author       string   `json:"author"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags,omitempty"`
	SystemPrompt string   `json:"system_prompt"`
	InstalledAt  string   `json:"installed_at"`
}

// ===== 子命令实现 =====

func runMarketplaceSearch(args []string) error {
	query := ""
	category := ""
	jsonOutput := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--category":
			i++
			if i >= len(args) {
				return fmt.Errorf("--category requires a value")
			}
			category = args[i]
		case "--json":
			jsonOutput = true
		default:
			if !strings.HasPrefix(args[i], "-") {
				query = args[i]
			}
		}
	}

	// 从本地模板库搜索
	templates := loadLocalTemplates()

	// 过滤
	var results []templateMeta
	for _, tmpl := range templates {
		if category != "" && tmpl.Category != category {
			continue
		}
		if query != "" {
			queryLower := strings.ToLower(query)
			if !strings.Contains(strings.ToLower(tmpl.Name), queryLower) &&
				!strings.Contains(strings.ToLower(tmpl.Description), queryLower) {
				continue
			}
		}
		results = append(results, tmpl)
	}

	if jsonOutput {
		out, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	if len(results) == 0 {
		fmt.Println("未找到匹配的模板")
		if query != "" {
			fmt.Printf("  搜索关键词: %q\n", query)
		}
		if category != "" {
			fmt.Printf("  分类过滤: %s\n", category)
		}
		return nil
	}

	fmt.Printf("找到 %d 个模板：\n\n", len(results))
	for _, tmpl := range results {
		fmt.Printf("  %s %s\n", bold(tmpl.ID), tmpl.Name)
		fmt.Printf("    %s\n", tmpl.Description)
		fmt.Printf("    分类: %s | 版本: %s | 作者: %s\n\n", tmpl.Category, tmpl.Version, tmpl.Author)
	}

	return nil
}

func runMarketplaceInstall(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ap marketplace install <template-id|manifest-url>")
	}

	target := args[0]

	// v4.4-3 在线安装：支持远程清单 URL（https://... → 拉取 + 验签 + 落盘）
	if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") {
		return installRemoteTemplate(target)
	}

	templateID := target

	// 验证模板 ID 安全性
	targetPath, err := safeTemplatePath(templateID)
	if err != nil {
		return err
	}

	// 确保模板目录存在
	if err := os.MkdirAll(marketplaceDir, 0755); err != nil {
		return fmt.Errorf("create templates dir: %w", err)
	}

	// 检查是否已安装
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("template %q already installed", templateID)
	}

	// 创建模板占位（本地模式：无远程清单时生成默认模板）
	tmpl := templateMeta{
		ID:           templateID,
		Name:         templateID,
		Description:  "Installed template",
		Version:      "1.0.0",
		Author:       "unknown",
		Category:     "chat",
		SystemPrompt: "You are a helpful assistant.",
		InstalledAt:  time.Now().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal template: %w", err)
	}

	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return fmt.Errorf("write template: %w", err)
	}

	infof("模板 %s 安装成功", bold(templateID))
	fmt.Printf("  路径: %s\n", targetPath)
	fmt.Printf("\n使用以下命令运行：\n")
	fmt.Printf("  %s\n", bold("ap marketplace run "+templateID))

	return nil
}

// remoteTemplate 远程模板清单（v4.4-3 在线安装协议）。
type remoteTemplate struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Version      string `json:"version"`
	Author       string `json:"author"`
	Category     string `json:"category"`
	SystemPrompt string `json:"system_prompt"`
	// Signature 对 Files JSON 的 base64 签名（ECDSA P-256 over SHA-256，可选）
	Signature string `json:"signature,omitempty"`
	// PublicKey 发布方公钥（PEM，ECDSA P-256）
	PublicKey string `json:"public_key,omitempty"`
	// Files 模板附带文件（文件名 → 内容），落盘到模板目录
	Files map[string]string `json:"files,omitempty"`
}

// installRemoteTemplate 从远程清单 URL 安装模板：拉取 → 验签 → 落盘。
func installRemoteTemplate(url string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("拉取模板清单失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("模板清单端点 %s: %s", resp.Status, http.StatusText(resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("读取模板清单失败: %w", err)
	}

	var tmpl remoteTemplate
	if err := json.Unmarshal(body, &tmpl); err != nil {
		return fmt.Errorf("解析模板清单失败: %w", err)
	}
	if tmpl.ID == "" {
		return fmt.Errorf("模板清单缺少 id 字段")
	}
	if err := validateTemplateID(tmpl.ID); err != nil {
		return err
	}

	// 验签（提供签名时强制校验；未签名清单标记警告）
	if tmpl.Signature != "" && tmpl.PublicKey != "" {
		filesJSON, _ := json.Marshal(tmpl.Files)
		if err := marketplace.VerifyCosignSignature(filesJSON, tmpl.Signature, tmpl.PublicKey); err != nil {
			return fmt.Errorf("模板验签失败（可能被篡改）: %w", err)
		}
	} else if tmpl.Signature != "" || tmpl.PublicKey != "" {
		return fmt.Errorf("模板清单签名配置不完整（signature 与 public_key 需同时提供）")
	} else {
		warnf("模板 %s 未签名（仅限可信源使用）", tmpl.ID)
	}

	// 落盘：元数据 + 附带文件
	if err := os.MkdirAll(marketplaceDir, 0755); err != nil {
		return fmt.Errorf("create templates dir: %w", err)
	}
	metaPath, err := safeTemplatePath(tmpl.ID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(metaPath); err == nil {
		return fmt.Errorf("template %q already installed", tmpl.ID)
	}
	meta := templateMeta{
		ID:           tmpl.ID,
		Name:         tmpl.Name,
		Description:  tmpl.Description,
		Version:      tmpl.Version,
		Author:       tmpl.Author,
		Category:     tmpl.Category,
		SystemPrompt: tmpl.SystemPrompt,
		InstalledAt:  time.Now().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal template: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("write template: %w", err)
	}

	for name, content := range tmpl.Files {
		if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
			return fmt.Errorf("模板附带文件非法名 %q", name)
		}
		filePath := filepath.Join(marketplaceDir, tmpl.ID+"-files", name)
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("写模板文件 %s: %w", name, err)
		}
	}

	infof("模板 %s 远程安装成功（v4.4-3 在线安装）", bold(tmpl.ID))
	fmt.Printf("  来源: %s\n", url)
	fmt.Printf("  路径: %s\n", metaPath)
	if len(tmpl.Files) > 0 {
		fmt.Printf("  附带文件: %d 个\n", len(tmpl.Files))
	}
	fmt.Printf("\n使用以下命令运行：\n")
	fmt.Printf("  %s\n", bold("ap marketplace run "+tmpl.ID))
	return nil
}

func runMarketplacePublish(args []string) error {
	// 查找当前目录的 agent.yaml 或 .ap.yaml
	configFile := ""
	for _, name := range []string{"agent.yaml", ".ap.yaml", "ap.json"} {
		if _, err := os.Stat(name); err == nil {
			configFile = name
			break
		}
	}

	if configFile == "" {
		return fmt.Errorf("no agent config found in current directory (expected agent.yaml or .ap.yaml)")
	}

	fmt.Printf("%s\n\n", bold("发布模板"))
	fmt.Printf("  配置文件:  %s\n", configFile)
	fmt.Printf("  状态:      %s\n", bold("准备发布"))
	fmt.Println()
	fmt.Println("  注意：模板发布功能需要连接到 AgentPrimordia 市场服务。")
	fmt.Println("  当前为本地模式，模板将保存到本地目录。")

	return nil
}

func runMarketplaceRun(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ap marketplace run <template-id>")
	}

	templateID := args[0]

	// 验证模板 ID 安全性
	targetPath, err := safeTemplatePath(templateID)
	if err != nil {
		return err
	}

	// 查找已安装的模板
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return fmt.Errorf("template %q not installed, run 'ap marketplace install %s' first", templateID, templateID)
	}

	var tmpl templateMeta
	if err := json.Unmarshal(data, &tmpl); err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	fmt.Printf("%s\n\n", bold("从模板启动 Agent"))
	fmt.Printf("  模板 ID:     %s\n", tmpl.ID)
	fmt.Printf("  名称:        %s\n", tmpl.Name)
	fmt.Printf("  分类:        %s\n", tmpl.Category)
	fmt.Printf("  系统提示:    %s\n", truncateStr(tmpl.SystemPrompt, 60))
	fmt.Println()
	fmt.Printf("  状态: %s\n", bold("就绪"))
	fmt.Printf("\n  提示：实际运行需要配置 LLM Provider（API Key 等）。\n")
	fmt.Printf("  使用 %s 初始化完整项目。\n", bold("ap init --template "+templateID))

	return nil
}

func runMarketplaceList(args []string) error {
	templates := loadLocalTemplates()

	jsonOutput := false
	for _, a := range args {
		if a == "--json" {
			jsonOutput = true
		}
	}

	if jsonOutput {
		out, _ := json.MarshalIndent(templates, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	if len(templates) == 0 {
		fmt.Println("未安装任何模板")
		fmt.Printf("使用 %s 搜索可用模板\n", bold("ap marketplace search <query>"))
		return nil
	}

	fmt.Printf("已安装 %d 个模板：\n\n", len(templates))
	for _, tmpl := range templates {
		fmt.Printf("  %s  %s (v%s)\n", bold(tmpl.ID), tmpl.Name, tmpl.Version)
		fmt.Printf("    %s | %s\n\n", tmpl.Category, tmpl.Description)
	}

	return nil
}

// ===== 辅助函数 =====

// loadLocalTemplates 加载本地已安装的模板
func loadLocalTemplates() []templateMeta {
	var templates []templateMeta

	entries, err := os.ReadDir(marketplaceDir)
	if err != nil {
		return templates
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(marketplaceDir + "/" + entry.Name())
		if err != nil {
			continue
		}
		var tmpl templateMeta
		if err := json.Unmarshal(data, &tmpl); err != nil {
			continue
		}
		templates = append(templates, tmpl)
	}

	return templates
}

// truncateStr 截断字符串
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ===== 模板目录（catalog）：远程协议 + 持久化（v7.4 接线）=====

// marketplaceRegistryPath 本地模板目录的持久化文件。
func marketplaceRegistryPath() string {
	return filepath.Join(marketplaceDir, "registry.json")
}

// newMarketplaceRegistry 构造注入 JSON 文件持久化的模板注册表。
// 该注册表在进程重启后自动恢复，是 agent/marketplace 持久化能力的生产构造点。
func newMarketplaceRegistry() *agentmarket.TemplateRegistry {
	return agentmarket.NewTemplateRegistry(
		agentmarket.WithStore(agentmarket.NewJSONFileStore(marketplaceRegistryPath())),
	)
}

// runMarketplaceCatalog 处理 ap marketplace catalog <add|list|remove>。
func runMarketplaceCatalog(args []string) error {
	if len(args) == 0 {
		printMarketplaceCatalogHelp()
		return nil
	}
	switch args[0] {
	case "add":
		return runMarketplaceCatalogAdd(args[1:])
	case "list":
		return runMarketplaceCatalogList(args[1:])
	case "remove":
		return runMarketplaceCatalogRemove(args[1:])
	case "--help", "-h", "help":
		printMarketplaceCatalogHelp()
		return nil
	default:
		return fmt.Errorf("unknown catalog subcommand %q, run %s for help", args[0], bold("ap marketplace catalog --help"))
	}
}

func printMarketplaceCatalogHelp() {
	fmt.Print(`ap marketplace catalog — 远程模板目录与本地持久化

Usage:
  ap marketplace catalog <subcommand> [arguments]

Subcommands:
  add <url> [--signature B64 --public-key PEM]   从远程目录导入模板
  list                                           列出本地模板目录
  remove <id>                                    从本地模板目录移除

说明：
  - 提供 --signature 与 --public-key 任一即强制 cosign（ECDSA P-256）验签，失败拒绝导入；
  - 目录响应支持 {"templates":[...]} 或裸数组 [...];
  - 导入结果持久化到 <marketplaceDir>/registry.json，进程重启后自动恢复。
`)
}

func runMarketplaceCatalogAdd(args []string) error {
	var url, signature, publicKey string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--signature":
			i++
			if i >= len(args) {
				return fmt.Errorf("--signature 需要指定 base64 值")
			}
			signature = args[i]
		case "--public-key":
			i++
			if i >= len(args) {
				return fmt.Errorf("--public-key 需要指定 PEM 文件路径或内容")
			}
			publicKey = args[i]
		case "--help", "-h":
			printMarketplaceCatalogHelp()
			return nil
		default:
			if url == "" {
				url = args[i]
			}
		}
	}
	if url == "" {
		return fmt.Errorf("用法: ap marketplace catalog add <url> [--signature B64 --public-key PEM]")
	}
	if err := os.MkdirAll(marketplaceDir, 0o755); err != nil {
		return fmt.Errorf("创建模板目录失败: %w", err)
	}

	reg := newMarketplaceRegistry()
	if err := reg.LoadError(); err != nil {
		infof("本地模板目录加载失败（将重新导入）: %v", err)
	}
	n, err := reg.FetchCatalog(context.Background(), url, signature, publicKey, nil)
	if err != nil {
		return fmt.Errorf("拉取模板目录失败: %w", err)
	}
	successf("已导入 %d 个模板（持久化于 %s）", n, marketplaceRegistryPath())
	return nil
}

func runMarketplaceCatalogList(args []string) error {
	_ = args
	reg := newMarketplaceRegistry()
	if err := reg.LoadError(); err != nil {
		return fmt.Errorf("加载本地模板目录失败: %w", err)
	}
	list := reg.List()
	if len(list) == 0 {
		infof("本地模板目录为空（用 ap marketplace catalog add <url> 导入）")
		return nil
	}
	fmt.Println("本地模板目录:")
	for _, tmpl := range list {
		fmt.Printf("  %-28s v%-10s %s\n", tmpl.ID, tmpl.Version, tmpl.Name)
	}
	return nil
}

func runMarketplaceCatalogRemove(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: ap marketplace catalog remove <id>")
	}
	reg := newMarketplaceRegistry()
	if err := reg.Unregister(args[0]); err != nil {
		return fmt.Errorf("移除模板失败: %w", err)
	}
	successf("已移除模板 %s", args[0])
	return nil
}
