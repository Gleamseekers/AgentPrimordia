// Package ebpf 是 procfs 的 deprecated alias（评估报告 §四.5 修复）。
//
// v6.x 重命名：原包名 `ebpf` 名实不符——实际是 /proc/[pid]/io 轮询，
// 并非真正的 eBPF syscall 追踪。包重命名为 procfs。
//
// 此文件保留 `otel/ebpf` 路径作为过渡，让现有调用方（外部/内部）继续
// 编译。本包所有公开符号均为 procfs 的 re-export（type alias）。
//
// 移除计划：v7.x。
package ebpf

import "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/observability/export/otel/procfs"

// Deprecated: 自 v6.x 起，请使用 github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/observability/export/otel/procfs。
// Removed in v7.0. 当前符号作为 alias 保留至 v7.x。

type (
	Tracer       = procfs.Tracer
	TracerConfig = procfs.TracerConfig
	SyscallEvent = procfs.SyscallEvent
)

var (
	ErrNotSupported = procfs.ErrNotSupported
)

func NewTracer(config TracerConfig) Tracer { return procfs.NewTracer(config) }
