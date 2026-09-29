// context_compress.go — context 子包的类型别名，保持向后兼容
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/context"
)

// CompressConfig 压缩配置
// 类型别名保持向后兼容
type CompressConfig = context.CompressConfig

// CompressStrategy 智能压缩策略
// 类型别名保持向后兼容
type CompressStrategy = context.CompressStrategy

// NewCompressStrategy 创建压缩策略
// 委托到 context 子包，保持向后兼容
func NewCompressStrategy(config CompressConfig) *CompressStrategy {
	return context.NewCompressStrategy(config)
}
