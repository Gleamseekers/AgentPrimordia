// zerocopy.go — zerocopy 子包的类型别名，保持向后兼容
package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/zerocopy"
)

// ZeroCopyMessage 零拷贝消息
// 类型别名保持向后兼容
type ZeroCopyMessage = zerocopy.ZeroCopyMessage

// ZeroCopyPool 零拷贝消息对象池
// 类型别名保持向后兼容
type ZeroCopyPool = zerocopy.ZeroCopyPool

// NewZeroCopyMessage 创建零拷贝消息
// 委托到 zerocopy 子包，保持向后兼容
func NewZeroCopyMessage(role Role, content string) *ZeroCopyMessage {
	return zerocopy.NewZeroCopyMessage(role, content)
}

// BatchConvertToZeroCopy 批量转换为零拷贝消息
// 委托到 zerocopy 子包，保持向后兼容
func BatchConvertToZeroCopy(role Role, contents []string) []*ZeroCopyMessage {
	return zerocopy.BatchConvertToZeroCopy(role, contents)
}

// NewZeroCopyPool 创建消息对象池
// 委托到 zerocopy 子包，保持向后兼容
func NewZeroCopyPool() *ZeroCopyPool {
	return zerocopy.NewZeroCopyPool()
}

// ZeroCopyFromBytes 从字节数组创建零拷贝字符串
// 委托到 zerocopy 子包，保持向后兼容
func ZeroCopyFromBytes(b []byte) string {
	return zerocopy.ZeroCopyFromBytes(b)
}

// BytesFromZeroCopy 从零拷贝字符串获取字节数组
// 委托到 zerocopy 子包，保持向后兼容
func BytesFromZeroCopy(s string) []byte {
	return zerocopy.BytesFromZeroCopy(s)
}
