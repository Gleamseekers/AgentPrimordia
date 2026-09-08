# AgentPrimordia Demo 视频脚本

> 时长：约 8-10 分钟  
> 目标：展示"越用越强"的 Go Agent 框架核心体验

---

## 开场（30 秒）

**画面**：终端 + 代码编辑器并排

**旁白**：
" Hermes Agent 用 Python 做到了 242K stars，核心卖点是'越用越强'。今天展示 AgentPrimordia 如何用 Go 实现同样的体验，同时保持 Go 的性能优势：单二进制、零 GC 停顿、原生并发。"

---

## 第一幕：零门槛启动（2 分钟）

### 场景 1.1：`ap start` 一键创建

**操作**：
```bash
# 清空演示目录
rm -rf ~/demo && mkdir ~/demo && cd ~/demo

# 一键启动 agent
ap start my-first-agent
```

**画面**：终端输出
```
创建项目 "my-first-agent" ...
✓ 项目已创建 (Templates: quickstart)
✓ 已添加到 go.work
✓ 依赖安装完成

未检测到 API key，将使用 Demo 模式
提示: 设置 AP_LLM_API_KEY 启用真实 LLM

编译 my-first-agent-agent ...
✓ 编译完成，启动 my-first-agent-agent

AgentPrimordia 快速入门
=========================
Agent已创建
   名称: QuickStartAgent

开始对话...
```

**旁白**：
" 从零到一个可运行的 agent，只需要一条命令。没有 API key？没关系，Demo 模式让你立即体验基本功能。"

### 场景 1.2：与 agent 对话

**操作**：
```
> 你好，请介绍一下自己

我是 QuickStartAgent，一个基于 AgentPrimordia 框架构建的 AI 助手。
我可以帮助你完成代码审查、数据分析、文件处理等任务。

> 帮我审查这段代码：
  func add(a, b int) int {
      return a + b
  }

代码审查结果：
✓ 函数签名清晰，参数类型明确
✓ 返回值类型正确
✓ 无副作用，纯函数设计
建议：可以考虑添加单元测试验证边界情况

> 谢谢！

不客气！有任何问题随时找我。
```

**旁白**：
" Demo 模式基于关键词匹配提供有意义的响应，让你立即看到 agent 的工作方式。设置 API key 后，会自动切换到真实 LLM。"

---

## 第二幕：学习闭环体感（3 分钟）

### 场景 2.1：多轮对话积累学习数据

**操作**：快速进行 5-10 轮对话，涵盖不同领域
```
> 审查这段 Go 代码...
> 分析这个 CSV 文件...
> 帮我写一个 SQL 查询...
> 审查另一个函数...
> 再分析一个数据集...
```

**画面**：终端快速滚动对话内容（加速播放）

**旁白**：
" 每次对话，agent 都在学习。它记录哪些任务做得好，哪些需要改进。这些数据存储在本地的 SQLite 数据库中。"

### 场景 2.2：`ap profile` 查看成长报告

**操作**：
```bash
# 新开一个终端
cd ~/demo/my-first-agent
ap profile
```

**画面**：终端输出
```
Agent Growth Profile
====================
Overall: 78% success (14/18 tasks), avg 3.2 turns

Top Capabilities:
  1. code-review-go     92% (11/12)  improving ↑
  2. data-analysis      85% (17/20)  stable →
  
Weak Areas:
  1. sql-query          45% (5/11)   declining ↓

Memory: 234 episodic / 18 semantic / 42 forgotten
Tool Learning: best filesystem.read(98%), worst shell.exec(67%)

Recent Growth Events:
  [09-08 15:30] code-review-go unlocked (success > 70%)
  [09-08 15:28] 10th semantic experience distilled
  [09-08 15:25] code-review-go mastered (success > 90%)
```

**旁白**：
" 这就是'越用越强'的体感。你可以清晰看到 agent 在哪些领域变强了，哪些还需要改进。成长事件记录了关键的里程碑时刻。"

### 场景 2.3：Studio 可视化面板

**操作**：
```bash
# 启动 Studio
ap studio
```

**画面**：浏览器打开 http://localhost:8080，展示：
- 能力雷达图（代码审查、数据分析、SQL 查询等维度）
- 成长曲线（成功率随时间上升）
- 工具使用热力图
- 记忆统计饼图

**旁白**：
" Studio 面板让学习数据可视化。雷达图展示能力分布，成长曲线显示进步趋势。这些都不是 demo 数据，是 agent 真实的学习记录。"

---

## 第三幕：A2A 协议互联（2 分钟）

### 场景 3.1：启动两个 agent

**操作**：
```bash
# 终端 1：启动数学 agent
cd ~/demo
ap start math-agent --template with-tools

# 终端 2：启动编排 agent
cd ~/demo
ap start orchestrator-agent
```

**画面**：两个终端分别启动

### 场景 3.2：Agent 间通信

**操作**：在 orchestrator-agent 中
```
> 计算 2 + 3 * 4

[Orchestrator] 发现 math-agent @ http://localhost:9100
[Orchestrator] 发送任务到 math-agent...
[Math Agent] 接收任务，执行计算...
[Math Agent] 返回结果: 14
[Orchestrator] 收到结果，整理响应...

计算结果：14

（math-agent 负责计算，orchestrator 负责编排）
```

**画面**：两个终端的日志交替显示

**旁白**：
" 通过 A2A 协议，agent 可以发现彼此、分配任务、协同工作。这是构建复杂 agent 系统的基础。"

### 场景 3.3：互操作合规报告

**操作**：
```bash
# 运行 A2A 示例
go run ./ecosystem/examples/a2a-connect/
```

**画面**：
```
A2A Connect — 两个 Agent 通过开放协议通信
==========================================

[Agent A] 启动数学计算服务...
[Agent A] 监听 :9100

[Agent B] 发现 Agent A...
[Agent B] 发现: math-agent (提供数学计算能力的 Agent)
[Agent B] 技能: 数学计算

[Agent B] 发送计算任务...
[Agent B] 任务已创建: task-xxx (状态: completed)

[Agent B] 任务完成! 结果:
  Echo: 计算 2 + 3 * 4

[Agent B] 生成互操作合规报告...
[Agent B] 合规评分: 100%
  [PASS] agent_card.name
  [PASS] agent_card.url
  [PASS] capabilities.streaming
  ...

A2A 通信演示完成!
```

**旁白**：
" 完整的 A2A 协议实现，包括 Agent Card 发现、任务管理、SSE 流式推送。合规报告确保互操作性。"

---

## 收尾（30 秒）

**画面**：回到终端，显示项目结构

**旁白**：
" 从零门槛启动，到学习闭环体感，再到 A2A 协议互联。AgentPrimordia 用 Go 实现了完整的 agent 开发体验。单二进制部署、零 GC 停顿、原生并发支持。"

**画面**：显示 GitHub 仓库地址

**旁白**：
" 开源地址：github.com/your-org/agentprimordia。欢迎 star、fork、贡献。让我们一起用 Go 构建下一代 AI agent 生态。"

---

## 附录：演示准备清单

### 环境要求
- Go 1.26+
- AgentPrimordia 源码（本地编译 `ap` 命令）
- 终端（推荐 iTerm2 或 Terminal）
- 浏览器（Studio 面板）

### 预演步骤
1. 清空演示目录：`rm -rf ~/demo`
2. 编译 `ap` 命令：`cd agentprimordia && go build -o /usr/local/bin/ap ./cmd/ap`
3. 测试 `ap start` 流程
4. 准备 5-10 轮对话内容
5. 测试 `ap profile` 输出
6. 启动 Studio 确认端口可用
7. 测试 A2A 示例运行

### 录制技巧
- 使用 `asciinema` 或 `terminalizer` 录制终端
- 对话部分可以预录制，现场播放
- Studio 面板用屏幕录制
- 加速播放冗长部分（如编译）

### 关键数据点
- 启动时间：< 5 秒（从 `ap start` 到可对话）
- 内存占用：< 50MB（空 agent）
- 学习收敛：10 轮对话后显示明显成长
- A2A 延迟：< 100ms（本地通信）
