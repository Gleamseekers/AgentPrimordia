// intelligence_bridge.go — IntelligenceHook 桥接 HookManager
// 将 intelligence 包的工具智能 Hook 适配到 ReAct 循环的 HookManager 体系。
// 桥接语义：HookAfterTool → AfterToolCall（画像记录），HookAfterTurn → OnTurnEnd（缺口检测）。
package agent

import (
	"context"

	"agentprimordia/internal/tools/intelligence"
)

// ToolIntelligenceConfig 工具智能配置
// 持有构建 IntelligenceHook 所需的三个接口实现。
// 通过 ReActConfig.ToolIntelligence 注入；非 nil 时自动桥接到 HookManager。
type ToolIntelligenceConfig struct {
	Profiler intelligence.ToolProfiler
	Detector intelligence.GapDetector
	Creator  intelligence.ToolCreator
}

// setupIntelligenceBridge 将 IntelligenceHook 适配到 HookManager（幂等）。
//
// 通过 RemoveByID + RegisterConditional 实现幂等：每次 Run 调用时先移除旧注册
// 再重新注册，确保桥接函数闭包捕获的 agent 引用始终正确。
// runMu 锁保证同一时刻只有一个 Run 在执行，因此不存在竞态。
//
// 注册两个 HookFunc：
//   - HookAfterTool：从 HookContext 提取工具名/参数/结果/错误/耗时，调用 AfterToolCall
//   - HookAfterTurn：调用 OnTurnEnd 触发缺口检测与自动工具创建
//
// 桥接函数通过 a.capCache 间接引用 IntelligenceHook，因此每次 Run 创建的新实例
// 自动生效。所有调用均为 fire-and-forget 语义（始终返回 nil），
// 画像记录或缺口检测失败不阻塞主 ReAct 循环。
func (a *ReActAgent) setupIntelligenceBridge() {
	if a.config.ToolIntelligence == nil || a.hooks == nil {
		return
	}

	// 幂等：先移除旧注册（首次调用时无效果），再重新注册
	a.hooks.RemoveByID("intelligence_bridge_tool")
	a.hooks.RemoveByID("intelligence_bridge_turn")

	// HookAfterTool → AfterToolCall 适配
	a.hooks.RegisterConditional(
		HookAfterTool,
		func(_ context.Context, hctx *HookContext) error {
			if a.capCache == nil || a.capCache.intelligenceHook == nil {
				return nil
			}
			// 从 HookContext 提取工具调用数据
			var toolName, args, result string
			if hctx.ToolCall != nil {
				toolName = hctx.ToolCall.Name
				args = hctx.ToolCall.Args
			}
			if hctx.ToolResult != nil {
				result = hctx.ToolResult.Content
			}
			a.capCache.intelligenceHook.AfterToolCall(
				context.Background(), toolName, args, result, hctx.Error, hctx.Duration,
			)
			return nil
		},
		0, Always, "intelligence_bridge_tool",
	)

	// HookAfterTurn → OnTurnEnd 适配
	a.hooks.RegisterConditional(
		HookAfterTurn,
		func(_ context.Context, _ *HookContext) error {
			if a.capCache == nil || a.capCache.intelligenceHook == nil {
				return nil
			}
			a.capCache.intelligenceHook.OnTurnEnd(context.Background())
			return nil
		},
		0, Always, "intelligence_bridge_turn",
	)
}
