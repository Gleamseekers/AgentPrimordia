// creator_llm.go — LLM 驱动的工具生成器
package create

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"agentprimordia/internal/tools/intelligence"
)

// LLMCompleter 最小化 LLM 补全接口（避免与 llm 包的导入循环）
type LLMCompleter interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// LLMCreator 基于 LLM 的工具生成器
// 当 LLM 调用失败时自动降级到 LifecycleCreator 的硬编码模板
type LLMCreator struct {
	llm    LLMCompleter
	fallback *LifecycleCreator
}

// NewLLMCreator 构造 LLM 驱动的工具生成器
func NewLLMCreator(llm LLMCompleter) *LLMCreator {
	return &LLMCreator{
		llm:      llm,
		fallback: NewLifecycleCreator(),
	}
}

// Create 使用 LLM 生成 shell 脚本工具
// LLM 失败时自动降级到硬编码模板
func (c *LLMCreator) Create(ctx context.Context, gap intelligence.GapCandidate) (*intelligence.ToolArtifact, error) {
	if gap.Key == "" {
		return nil, fmt.Errorf("缺口键为空")
	}

	// 尝试 LLM 生成
	artifact, err := c.createViaLLM(ctx, gap)
	if err == nil {
		return artifact, nil
	}

	// LLM 失败，降级到硬编码模板
	return c.fallback.Create(ctx, gap)
}

// createViaLLM 调用 LLM 生成工具脚本
func (c *LLMCreator) createViaLLM(ctx context.Context, gap intelligence.GapCandidate) (*intelligence.ToolArtifact, error) {
	prompt := buildPrompt(gap)
	result, err := c.llm.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM 补全失败: %w", err)
	}

	script := extractScript(result)
	if script == "" {
		return nil, fmt.Errorf("LLM 返回空脚本")
	}

	artifact := []byte(script)
	description := fmt.Sprintf("LLM 自动生成的工具：%s（来自缺口检测：%s）", gap.Key, gap.SampleError)
	sum := sha256.Sum256(artifact)

	return &intelligence.ToolArtifact{
		ID:          fmt.Sprintf("auto-%s", gap.Key),
		Name:        gap.Key,
		Description: description,
		ArtifactSHA: hex.EncodeToString(sum[:]),
		Artifact:    artifact,
	}, nil
}

// buildPrompt 构造提示词，指导 LLM 生成 shell 脚本
func buildPrompt(gap intelligence.GapCandidate) string {
	var b strings.Builder
	b.WriteString("你是一个工具生成助手。请根据以下缺口信息生成一个可执行的 POSIX shell 脚本。\n\n")
	b.WriteString(fmt.Sprintf("缺口名称: %s\n", gap.Key))
	b.WriteString(fmt.Sprintf("缺口类型: %s\n", gap.Kind))
	b.WriteString(fmt.Sprintf("出现次数: %d\n", gap.Count))
	if gap.SampleError != "" {
		b.WriteString(fmt.Sprintf("示例错误: %s\n", gap.SampleError))
	}
	b.WriteString("\n要求：\n")
	b.WriteString("1. 脚本以 #!/bin/sh 开头\n")
	b.WriteString("2. 脚本必须能独立执行，所有异常用 || echo 兜底\n")
	b.WriteString("3. 只输出脚本代码，不要解释，不要 markdown 代码块\n")
	return b.String()
}

// extractScript 从 LLM 输出中提取 shell 脚本
// 去除可能的 markdown 代码块包裹
func extractScript(raw string) string {
	s := strings.TrimSpace(raw)

	// 去除 ```sh ... ``` 或 ```bash ... ``` 包裹
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		// 去掉首行 ```sh 和末行 ```
		start := 1
		end := len(lines)
		if end > 0 && strings.TrimSpace(lines[end-1]) == "```" {
			end--
		}
		if start < end {
			s = strings.Join(lines[start:end], "\n")
		}
	}

	return strings.TrimSpace(s)
}
