# Cookbook: WASM 沙箱工具执行

本指南演示如何使用 AgentPrimordia 的 WASM 沙箱安全执行第三方工具代码。

## 核心概念

WASM 沙箱提供隔离的代码执行环境：

- **内存隔离**：每个 WASM 实例拥有独立的线性内存
- **资源限制**：CPU 时间、内存上限、执行超时
- **文件系统虚拟化**：通过 VirtualFS 提供受限的文件访问
- **WASI 兼容**：支持标准 WASI 接口（fd/env/args）

## 快速开始

### 基础 WASM 工具执行

```go
package main

import (
    "context"
    "fmt"

    "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/wasm"
)

func main() {
    // 创建 WASM 运行时
    runtime, err := wasm.NewRuntime(wasm.RuntimeConfig{
        MaxMemoryMB:  64,
        TimeoutMS:    5000,
        AllowNetwork: false,
    })
    if err != nil {
        panic(err)
    }
    defer runtime.Close()

    // 加载 WASM 模块
    module, err := runtime.LoadModule("tool.wasm")
    if err != nil {
        panic(err)
    }

    // 执行工具
    result, err := module.Execute(context.Background(), wasm.ExecRequest{
        Function: "run",
        Input:    []byte(`{"query": "hello"}`),
    })
    if err != nil {
        panic(err)
    }

    fmt.Printf("输出: %s\n", result.Output)
}
```

### 虚拟文件系统

```go
// 为 WASM 实例预挂载文件
vfs := wasm.NewVirtualFS()
vfs.Mount("/data/input.json", []byte(`{"key": "value"}`))
vfs.Mount("/config/settings.yaml", configBytes)

// WASM 代码内部可通过标准文件 API 访问
// 写入的文件会被捕获到 VFS 中
result := module.ExecuteWithFS(ctx, req, vfs)

// 读取 WASM 产生的输出文件
output := vfs.Read("/output/result.json")
```

### 资源限制配置

```go
runtime, _ := wasm.NewRuntime(wasm.RuntimeConfig{
    MaxMemoryMB:   128,    // 最大内存 128MB
    TimeoutMS:     10000,  // 执行超时 10s
    MaxFuel:       100000, // 指令计数限制
    AllowNetwork:  false,  // 禁止网络访问
    AllowFS:       true,   // 允许虚拟文件系统
    MaxOutputSize: 1 << 20, // 输出上限 1MB
})
```

## TypeScript SDK（浏览器端）

```typescript
import { CodeSandboxV2 } from '@agentprimordia/sdk';

const sandbox = new CodeSandboxV2({
  memoryLimitMB: 64,
  timeoutMs: 5000,
});

// 执行 WASM 代码
const result = await sandbox.execute(wasmBytes, {
  function: 'run',
  input: JSON.stringify({ query: 'hello' }),
});

console.log(result.output);
sandbox.terminate();
```

## 安全最佳实践

- 始终设置 `TimeoutMS` 防止无限循环
- 生产环境禁用 `AllowNetwork`
- 使用 `MaxFuel` 限制指令数防止 CPU 密集型攻击
- 输出大小限制（`MaxOutputSize`）防止内存耗尽
- 每次执行使用独立的 VirtualFS 实例，防止数据泄露
