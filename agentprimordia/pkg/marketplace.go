// Stability: Experimental — v3.0.0 新增 Agent 模板市场能力，API 可能随生态演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/marketplace"
)

// AgentTemplate Agent 模板定义（配置+tool集+系统提示+记忆策略）
type AgentTemplate = marketplace.AgentTemplate

// TemplateValidationResult 模板验证结果
type TemplateValidationResult = marketplace.ValidationResult

// TemplateRegistry Agent 模板注册表，支持注册、搜索、评分
type TemplateRegistry = marketplace.TemplateRegistry

// TemplateDeployer 模板部署器，一键从模板部署运行 Agent
type TemplateDeployer = marketplace.Deployer

// TemplateDeployConfig 部署配置
type TemplateDeployConfig = marketplace.DeployConfig

// TemplateDeployResult 部署结果
type TemplateDeployResult = marketplace.DeployResult

// TemplateStore 模板持久化后端（v7.4）：Save 全量覆盖、Load 缺失时返回空集合。
type TemplateStore = marketplace.TemplateStore

// JSONFileStore 基于 JSON 文件的模板持久化后端（标准库实现，原子替换写入）。
type JSONFileStore = marketplace.JSONFileStore

// MarketplaceCatalog 远程模板目录（对象形态；亦兼容裸数组）。
type MarketplaceCatalog = marketplace.Catalog

var (
	// NewTemplateRegistry 创建模板注册表（v7.4 起支持 WithTemplateStore 持久化）。
	NewTemplateRegistry = marketplace.NewTemplateRegistry
	// NewTemplateDeployer 创建模板部署器
	NewTemplateDeployer = marketplace.NewDeployer
	// WithTemplateStore 注入模板持久化后端：构造自动加载、变更落盘。
	WithTemplateStore = marketplace.WithStore
	// NewJSONFileStore 创建 JSON 文件模板持久化后端。
	NewJSONFileStore = marketplace.NewJSONFileStore
)
