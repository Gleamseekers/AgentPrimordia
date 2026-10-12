# 贡献指南

感谢您对 AgentPrimordia 项目的关注！我们欢迎各种形式的贡献，包括代码、文档、问题报告和社区支持。

## 目录

- [行为准则](#行为准则)
- [如何贡献](#如何贡献)
- [开发环境设置](#开发环境设置)
- [代码规范](#代码规范)
- [提交规范](#提交规范)
- [Pull Request 流程](#pull-request-流程)
- [Issue 报告](#issue-报告)
- [社区支持](#社区支持)

## 行为准则

本项目遵循 [Contributor Covenant](https://www.contributor-covenant.org/) 行为准则。参与贡献即表示您同意遵守此准则，共同维护一个友好、包容的社区环境。

## 如何贡献

### 报告 Bug

1. 首先检查 [现有 Issue](https://github.com/Gleamseekers/AgentPrimordia/issues) 是否已报告该问题
2. 如果没有，创建新的 Issue 并选择 "Bug Report" 模板
3. 提供清晰的描述、复现步骤和环境信息

### 提出新功能

1. 先在 [Discussions](https://github.com/Gleamseekers/AgentPrimordia/discussions) 中讨论您的想法
2. 获得社区反馈后，创建 Feature Request Issue
3. 等待维护者确认方向后再开始开发

### 贡献代码

1. Fork 仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 编写代码和测试
4. 提交更改 (`git commit -m 'feat: add amazing feature'`)
5. 推送到分支 (`git push origin feature/amazing-feature`)
6. 创建 Pull Request

### 改进文档

文档同样重要！您可以帮助：
- 修正拼写和语法错误
- 改进示例代码
- 添加教程和指南
- 翻译文档

## 开发环境设置

### 前置要求

- Go 1.26 或更高版本
- Git
- 推荐的 IDE：VS Code、GoLand

### 克隆和设置

```bash
# 克隆仓库
git clone https://github.com/Gleamseekers/AgentPrimordia.git
cd AgentPrimordia

# 安装依赖
go mod download

# 运行测试（快速回归；完整门见 Makefile 的 test / test-race / test-integration）
make test-short

# 运行示例
go run ./agentprimordia/ecosystem/examples/chain-api/
```

### 项目结构

```
agentprimordia/
├── internal/           # 内部实现
│   ├── agent/         # Agent 核心（ReAct 引擎、工作流、编排、传输、A2A 协议）
│   ├── llm/           # LLM 提供者（OpenAI/Anthropic/Gemini/Ollama/DeepSeek 等）
│   ├── memory/        # 记忆系统（SQLite + FTS5 + 向量检索）
│   ├── tools/         # 工具系统（注册、权限、MCP、插件）
│   ├── pool/          # Agent 池（多 Agent 调度）
│   ├── orchestration/ # 工作流编排（DAG/条件/并行/状态机）
│   ├── persist/       # 状态持久化
│   ├── admin/         # 管理面板
│   ├── debugger/      # 调试工具（Inspector、可视化编辑器）
│   ├── guardrail/     # 安全护栏
│   ├── otel/          # OpenTelemetry 集成
│   ├── metrics/       # 指标导出
│   ├── concurrency/   # 并发原语
│   ├── config/        # 配置管理
│   ├── events/        # 事件系统
│   ├── security/      # 安全工具
│   └── ...            # 其余横向支撑包：audit/chaos/eval/governance/health/jsonutil/logger/marketplace/multi_agent/observability/resilience/self_bootstrap/studio（完整 29 包见 internal/AGENTS.md）
├── pkg/               # 公共 API（类型别名导出）
├── cmd/               # 命令行工具
│   ├── ap/           # CLI 工具
│   ├── admin/        # 管理面板服务
│   └── example/      # 示例程序
├── ecosystem/         # 生态系统
│   ├── plugins/      # 官方插件
│   ├── examples/     # 完整示例
│   └── templates/    # 项目模板
├── operator/          # Kubernetes Operator
└── docs/              # 文档
```

## 代码规范

### Go 代码风格

- 遵循 [Effective Go](https://go.dev/doc/effective_go) 指南
- 使用 `gofmt` 格式化代码
- 使用 `golangci-lint` 检查代码质量

```bash
# 格式化代码
gofmt -w .

# 运行 linter
golangci-lint run
```

### 注释规范

- 所有公共 API 必须有文档注释
- 使用中文注释解释复杂的业务逻辑
- 函数注释应说明功能、参数和返回值

```go
// NewAgent 创建一个新的 Agent 实例（推荐入口）
// 
// 参数:
//   - name: Agent 名称
//   - systemPrompt: 系统提示词
//   - model: LLM Provider
//   - opts: Functional Options（WithMaxTurns、WithToolkit 等）
//
// 返回:
//   - *CapabilityAgent: 配置好的 Agent 实例
//   - error: 配置错误
func NewAgent(name, systemPrompt string, model llm.Provider, opts ...AgentOption) (*CapabilityAgent, error) {
    // ...
}
```

### 测试要求

- 所有新功能必须包含单元测试
- 测试覆盖率门槛：核心包（internal/llm、pkg）65% 以上（`make cover-check` 门禁）；新增代码建议对齐所在包现状
- 使用表驱动测试（table-driven tests）

```go
func TestNewAgent(t *testing.T) {
    tests := []struct {
        name    string
        agentName string
        system   string
        wantErr bool
    }{
        {
            name:      "valid config",
            agentName: "test-agent",
            system:    "you are helpful",
            wantErr:   false,
        },
        // 更多测试用例...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            agent, err := NewAgent(tt.agentName, tt.system, mockLLM)
            // 断言...
        })
    }
}
```

## 提交规范

我们使用 [Conventional Commits](https://www.conventionalcommits.org/) 规范。

### 提交消息格式

```
<type>(<scope>): <subject>

<body>

<footer>
```

### Type 类型

- `feat`: 新功能
- `fix`: Bug 修复
- `docs`: 文档更新
- `style`: 代码格式（不影响代码运行）
- `refactor`: 代码重构
- `perf`: 性能优化
- `test`: 测试相关
- `chore`: 构建过程或辅助工具变动

### 示例

```
feat(agent): 添加流式响应支持

- 实现 StreamRun 方法
- 支持 SSE 协议
- 添加流式测试用例

Closes #123
```

```
fix(memory): 修复并发访问时的竞态条件

使用 sync.RWMutex 保护共享状态，避免数据竞争。

Fixes #456
```

## Pull Request 流程

### 创建 PR

1. 确保所有测试通过：`go test ./...`
2. 确保代码已格式化：`gofmt -w .`
3. 更新相关文档
4. 填写 PR 模板，说明更改内容
5. 关联相关 Issue

### PR 审查

- 至少需要 1 位维护者审查
- 所有 CI 检查必须通过
- 解决所有审查意见

### 合并策略

- 使用 Squash and Merge 保持提交历史清晰
- 合并后删除特性分支

## Issue 报告

### Bug Report

使用 Bug Report 模板，包含：
- 清晰的标题和描述
- 复现步骤
- 期望行为 vs 实际行为
- 环境信息（Go 版本、操作系统等）
- 相关日志或截图

### Feature Request

使用 Feature Request 模板，包含：
- 功能描述
- 使用场景
- 建议的实现方案（可选）
- 替代方案（可选）

## 社区支持

### 讨论区

- [GitHub Discussions](https://github.com/Gleamseekers/AgentPrimordia/discussions)
  - 提问和答疑
  - 功能讨论
  - 展示项目
  -  general 交流

### 即时通讯

- Discord: [加入服务器](https://discord.gg/agentprimordia)
- 微信群: 扫码加入（见文档底部）

### 社交媒体

- Twitter: [@AgentPrimordia](https://twitter.com/AgentPrimordia)
- 知乎: [AgentPrimordia](https://www.zhihu.com/org/agentprimordia)

## 贡献者感谢

所有贡献者都会出现在 [CONTRIBUTORS.md](./CONTRIBUTORS.md) 中。

## 许可证

通过贡献代码，您同意您的贡献将遵循项目的 Apache-2.0 许可证（见仓库 LICENSE 文件）。

---

**有问题？** 欢迎在 [Discussions](https://github.com/Gleamseekers/AgentPrimordia/discussions) 中提问，或联系维护者。
