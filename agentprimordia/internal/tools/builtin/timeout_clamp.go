// timeout_clamp.go — 用户可控 timeout 参数统一 clamp（P1 安全修复）
//
// web / api / http_client / code_execution 四个工具的 timeout 参数均来自
// 调用方（LLM 生成的工具参数，可能被 prompt injection 控制）。旧实现直接
// `int(v)` 后乘 time.Duration，超大值（如 1e18）会溢出 int64 纳秒计数，
// 产生负超时——行为未定义（可能立即取消请求，也可能永不过时）。
//
// 修复：统一 clamp 到 [minTimeoutSec, maxTimeoutSec] 秒。
// maxTimeoutSec 定义于 builtin/shell.go（P0-1 已实施的同一模式），此处
// 仅补充下限与 clamp 函数，供全部内置 HTTP/执行类工具复用。
package builtin

// minTimeoutSec 单次操作超时下限（秒）。防止 0/负值/极小值导致
// context 立即过期或疯狂重试。
const minTimeoutSec = 1

// clampTimeoutSec 将用户可控的 timeout 秒数 clamp 到 [minTimeoutSec, maxTimeoutSec]。
// 同时屏蔽 int(v) 大值溢出 time.Duration 的风险（clamp 后最大值 3600 秒，
// time.Duration(3600)*time.Second 远小于 int64 上限）。
// 比较先于 int 转换：v 超过 int64 的 float（如 1e300）或 NaN 直接归边界，
// 不进入实现定义行为域。
func clampTimeoutSec(v float64) int {
	if !(v >= float64(minTimeoutSec)) { // 低于下限或 NaN
		return minTimeoutSec
	}
	if v >= float64(maxTimeoutSec) { // 高于上限（含 +Inf）
		return maxTimeoutSec
	}
	return int(v)
}
