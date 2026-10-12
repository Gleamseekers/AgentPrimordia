package main

import (
	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

const (
	// demoAgentName server 对外声明的 Agent 名称（Agent Card name）。
	demoAgentName = "ap-handoff-agent"

	// demoPrompt client 委托用的提示词。
	// 选词依据：命中 DemoProvider 的能力清单分支（关键词 "能做什么"），
	// 响应内容确定性且与 A2A 主题相关（含 "A2A 协议通信"），
	// 不依赖任何外部 LLM，CI 可稳定断言。
	demoPrompt = "你能做什么？简单介绍一下 A2A 协议"
)

// buildCard 构造 server 对外暴露的开放协议 Agent Card。
// server 与 report 两个子命令共用同一构造函数，保证入库的符合性报告
// 与在线发现到的 card 完全一致。
func buildCard(addr string) ap.OpenAgentCard {
	return ap.OpenAgentCard{
		Name:        demoAgentName,
		Description: "经开放 A2A 协议提供问答与任务委托能力的 ap Agent（Demo 模式，零凭据可跑）",
		URL:         "http://" + addr,
		Version:     ap.Version,
		Capabilities: ap.OpenCapabilities{
			Streaming:              true,
			StateTransitionHistory: true,
		},
		Skills: []ap.OpenSkillDecl{
			{
				ID:          "a2a-explainer",
				Name:        "A2A 协议讲解",
				Description: "介绍 A2A 开放协议与多 Agent 互操作要点",
				Tags:        []string{"a2a", "interop"},
			},
		},
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}
}
