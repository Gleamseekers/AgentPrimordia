# 安全策略

## 报告安全漏洞

如果您发现 AgentPrimordia 项目中的安全漏洞，请**不要**在 GitHub 上公开报告。

相反，请通过以下方式私下报告：

- 📧 发送邮件到：security@agentprimordia.dev
- 🔒 使用 GitHub 的 [Private Vulnerability Reporting](https://github.com/Gleamseekers/AgentPrimordia/security/advisories/new)

## 报告流程

1. **提交报告**：提供详细的漏洞描述、影响范围和复现步骤
2. **确认接收**：我们将在 48 小时内确认收到您的报告
3. **评估分析**：我们的安全团队将评估漏洞的严重性和影响
4. **修复开发**：开发修复补丁
5. **协调发布**：与报告者协调发布时间和方式
6. **公开披露**：在修复发布后公开披露漏洞信息

## 响应时间

- **初始响应**：48 小时内
- **评估完成**：7 天内
- **修复发布**：根据严重程度，通常在 30 天内

## 漏洞严重程度

我们使用以下标准评估漏洞严重程度：

### 严重（Critical）
- 远程代码执行
- 认证绕过
- 敏感数据泄露（如 API 密钥、用户数据）
- 权限提升

### 高（High）
- 拒绝服务攻击
- 信息泄露
- 跨站脚本（XSS）
- 跨站请求伪造（CSRF）

### 中（Medium）
- 需要用户交互的漏洞
- 有限的信息泄露
- 配置问题

### 低（Low）
- 信息性发现
- 最佳实践建议
- 不影响安全的 Bug

## 安全最佳实践

### 对于用户

1. **保持更新**：始终使用最新版本的 AgentPrimordia
2. **API 密钥安全**：
   - 使用环境变量存储 API 密钥
   - 不要将密钥硬编码在代码中
   - 不要提交包含密钥的文件到版本控制
3. **最小权限**：为 Agent 配置最小必要的权限
4. **输入验证**：对用户输入进行验证和清理
5. **监控日志**：定期检查 Agent 的执行日志

### 对于开发者

1. **依赖安全**：定期更新依赖项，使用 `go mod audit`
2. **代码审查**：所有代码更改必须经过安全审查
3. **安全测试**：在 CI/CD 中包含安全测试
4. **最小暴露**：只暴露必要的 API 和端口
5. **加密通信**：使用 TLS/SSL 加密所有网络通信

## 安全更新

安全更新将通过以下方式发布：

- GitHub Security Advisories
- 项目 Release Notes
- 邮件列表通知（订阅：security-updates@agentprimordia.dev）

## 致谢

我们感谢所有负责任地报告安全漏洞的研究人员。如果您同意，我们将在安全公告中致谢。

## 联系信息

- **安全问题**：security@agentprimordia.dev
- **一般问题**：support@agentprimordia.dev
- **GitHub Issues**：https://github.com/Gleamseekers/AgentPrimordia/issues

---

**最后更新**：2026-08-09（安全策略复核，内容未变更）
