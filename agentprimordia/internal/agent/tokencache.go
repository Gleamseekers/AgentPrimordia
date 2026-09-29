// tokencache.go — tokencache 子包的函数包装，保持向后兼容
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/tokencache"
)

// ClearTokenCache 清空 token 缓存（测试用 / 内存压力场景）
// 委托到 tokencache 子包，保持向后兼容
func ClearTokenCache() {
	tokencache.ClearTokenCache()
}
