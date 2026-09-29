package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Phase 3.3: CLI Edge 脚手架
//
// 命令：
//   ap create-edge-agent <name>                  从 Edge 模板生成项目
//   ap create-edge-agent <name> --platform deno  指定运行时平台

func runCreateEdgeAgent(args []string) error {
	if len(args) == 0 {
		printEdgeHelp()
		return nil
	}

	name := ""
	platform := "cloudflare" // 默认平台
	outputDir := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--platform", "-p":
			i++
			if i >= len(args) {
				return fmt.Errorf("--platform requires a value (cloudflare/deno/bun)")
			}
			platform = args[i]
		case "--output", "-o":
			i++
			if i >= len(args) {
				return fmt.Errorf("--output requires a path")
			}
			outputDir = args[i]
		case "--help", "-h":
			printEdgeHelp()
			return nil
		default:
			if !strings.HasPrefix(args[i], "-") && name == "" {
				name = args[i]
			}
		}
	}

	if name == "" {
		return fmt.Errorf("project name is required: ap create-edge-agent <name>")
	}

	// 验证平台
	validPlatforms := map[string]bool{"cloudflare": true, "deno": true, "bun": true}
	if !validPlatforms[platform] {
		return fmt.Errorf("invalid platform %q: must be cloudflare, deno, or bun", platform)
	}

	// 验证项目名称
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("invalid project name %q", name)
	}

	// 确定输出目录
	if outputDir == "" {
		outputDir = name
	}

	// 验证输出目录安全性：解析绝对路径并确保不包含路径遍历
	absOutput, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	if strings.Contains(absOutput, "..") {
		return fmt.Errorf("invalid output directory %q: path traversal not allowed", outputDir)
	}

	// 检查目录是否已存在
	if _, err := os.Stat(absOutput); err == nil {
		return fmt.Errorf("directory %q already exists", outputDir)
	}

	// 生成文件
	files := generateEdgeProject(name, platform)

	// 写入文件
	for relPath, content := range files {
		fullPath := filepath.Join(absOutput, filepath.Clean(relPath))
		// 确保最终路径在输出目录内
		if !strings.HasPrefix(fullPath, absOutput) {
			return fmt.Errorf("invalid file path %q: escapes output directory", relPath)
		}
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write file %s: %w", fullPath, err)
		}
	}

	infof("Edge Agent 项目创建成功")
	fmt.Printf("  名称:    %s\n", bold(name))
	fmt.Printf("  平台:    %s\n", bold(platform))
	fmt.Printf("  目录:    %s\n", outputDir)
	fmt.Printf("  文件数:  %d\n", len(files))
	fmt.Println()
	fmt.Println("下一步：")
	switch platform {
	case "cloudflare":
		fmt.Printf("  cd %s && npm install && npx wrangler dev\n", outputDir)
	case "deno":
		fmt.Printf("  cd %s && deno run --allow-net src/index.ts\n", outputDir)
	case "bun":
		fmt.Printf("  cd %s && bun install && bun run src/index.ts\n", outputDir)
	}

	return nil
}

func printEdgeHelp() {
	fmt.Print(`ap create-edge-agent — create an Edge Agent project

Usage:
  ap create-edge-agent <name> [options]

Options:
  --platform, -p PLATFORM   runtime platform (cloudflare|deno|bun, default: cloudflare)
  --output, -o DIR          output directory (default: <name>)

Platforms:
  cloudflare    Cloudflare Workers (Durable Objects + AI Gateway)
  deno          Deno Deploy (native TypeScript, edge-optimized)
  bun           Bun runtime (fast startup, built-in bundler)

Examples:
  ap create-edge-agent my-agent
  ap create-edge-agent my-agent --platform deno
  ap create-edge-agent my-agent -p bun -o ./projects/my-agent
`)
}

// ===== 文件生成 =====

func generateEdgeProject(name, platform string) map[string]string {
	files := make(map[string]string)

	// 入口文件
	files["src/index.ts"] = generateEntryFile(name, platform)

	// package.json（cloudflare/bun）
	if platform != "deno" {
		files["package.json"] = generatePackageJSON(name, platform)
	}

	// tsconfig.json
	files["tsconfig.json"] = generateTSConfig(platform)

	// 平台特定配置
	switch platform {
	case "cloudflare":
		files["wrangler.toml"] = generateWranglerToml(name)
	case "deno":
		files["deno.json"] = generateDenoJSON(name)
	case "bun":
		files["bunfig.toml"] = generateBunfigToml()
	}

	// Agent 配置
	files["agent.config.ts"] = generateAgentConfig(name)

	// .gitignore
	files[".gitignore"] = "node_modules/\ndist/\n.wrangler/\n.env\n*.log\n"

	return files
}

func generateEntryFile(name, platform string) string {
	switch platform {
	case "cloudflare":
		return fmt.Sprintf(`/**
 * %s — Edge Agent (Cloudflare Workers)
 *
 * 使用 Durable Objects 实现有状态 Agent 会话。
 */
import { AgentPrimordiaEdge } from '@github.com/Gleamseekers/AgentPrimordia/agentprimordia/edge';

export default {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === '/chat' && request.method === 'POST') {
      const body = await request.json<{ message: string; session_id?: string }>();
      const sessionId = body.session_id ?? crypto.randomUUID();

      // 获取或创建 Agent Durable Object
      const id = env.AGENT.idFromName(sessionId);
      const agent = env.AGENT.get(id);

      return agent.fetch(request);
    }

    if (url.pathname === '/health') {
      return Response.json({ status: 'ok', agent: '%s' });
    }

    return new Response('Not Found', { status: 404 });
  },
};

export class AgentSession {
  private agent: AgentPrimordiaEdge;

  constructor(private state: DurableObjectState, private env: Env) {
    this.agent = new AgentPrimordiaEdge({
      provider: 'cloudflare-ai',
      model: '@cf/meta/llama-3.1-8b-instruct',
      systemPrompt: 'You are a helpful edge agent.',
      maxTurns: 10,
    });
  }

  async fetch(request: Request): Promise<Response> {
    const body = await request.json<{ message: string }>();
    const response = await this.agent.chat(body.message);
    return Response.json({ response, session_id: this.state.id.toString() });
  }
}

interface Env {
  AGENT: DurableObjectNamespace;
  AI: Ai;
}
`, name, name)

	case "deno":
		return fmt.Sprintf(`/**
 * %s — Edge Agent (Deno Deploy)
 *
 * 原生 TypeScript，无需构建步骤。
 */
import { AgentPrimordiaEdge } from 'npm:@github.com/Gleamseekers/AgentPrimordia/agentprimordia/edge';

const agent = new AgentPrimordiaEdge({
  provider: 'openai',
  model: 'gpt-4o-mini',
  systemPrompt: 'You are a helpful edge agent running on Deno Deploy.',
  maxTurns: 10,
});

Deno.serve(async (req: Request) => {
  const url = new URL(req.url);

  if (url.pathname === '/chat' && req.method === 'POST') {
    const body = await req.json();
    const response = await agent.chat(body.message);
    return Response.json({ response });
  }

  if (url.pathname === '/health') {
    return Response.json({ status: 'ok', agent: '%s', runtime: 'deno' });
  }

  return new Response('Not Found', { status: 404 });
});
`, name, name)

	default: // bun
		return fmt.Sprintf(`/**
 * %s — Edge Agent (Bun)
 *
 * 极速冷启动，内置 TypeScript 支持。
 */
import { AgentPrimordiaEdge } from '@github.com/Gleamseekers/AgentPrimordia/agentprimordia/edge';

const agent = new AgentPrimordiaEdge({
  provider: 'anthropic',
  model: 'claude-sonnet-4-20250514',
  systemPrompt: 'You are a helpful edge agent running on Bun.',
  maxTurns: 10,
});

const server = Bun.serve({
  port: 3000,
  async fetch(req: Request) {
    const url = new URL(req.url);

    if (url.pathname === '/chat' && req.method === 'POST') {
      const body = await req.json();
      const response = await agent.chat(body.message);
      return Response.json({ response });
    }

    if (url.pathname === '/health') {
      return Response.json({ status: 'ok', agent: '%s', runtime: 'bun' });
    }

    return new Response('Not Found', { status: 404 });
  },
});

console.log("%s listening on http://localhost:" + server.port);
`, name, name, name)
	}
}

func generatePackageJSON(name, platform string) string {
	deps := `"@github.com/Gleamseekers/AgentPrimordia/agentprimordia/edge": "^0.1.0"`
	devDeps := `"typescript": "^5.5.0"`
	scripts := `"dev": "wrangler dev",\n    "deploy": "wrangler deploy"`

	if platform == "bun" {
		scripts = `"dev": "bun --watch src/index.ts",\n    "start": "bun src/index.ts"`
	}

	return fmt.Sprintf(`{
  "name": "%s",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "scripts": {
    %s
  },
  "dependencies": {
    %s
  },
  "devDependencies": {
    %s
  }
}
`, name, scripts, deps, devDeps)
}

func generateTSConfig(platform string) string {
	libs := `"ES2022", "DOM"`
	if platform == "deno" {
		libs = `"ES2022", "DOM", "DOM.Iterable"`
	}

	return fmt.Sprintf(`{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "lib": [%s],
    "strict": true,
    "skipLibCheck": true,
    "outDir": "dist",
    "rootDir": "src",
    "declaration": true
  },
  "include": ["src/**/*"]
}
`, libs)
}

func generateWranglerToml(name string) string {
	return fmt.Sprintf(`name = "%s"
main = "src/index.ts"
compatibility_date = "2024-09-01"
compatibility_flags = ["nodejs_compat"]

[[durable_objects.bindings]]
name = "AGENT"
class_name = "AgentSession"

[[migrations]]
tag = "v1"
new_classes = ["AgentSession"]

[ai]
binding = "AI"
`, name)
}

func generateDenoJSON(name string) string {
	return fmt.Sprintf(`{
  "name": "%s",
  "tasks": {
    "dev": "deno run --watch --allow-net --allow-env src/index.ts",
    "deploy": "deployctl deploy --project=%s src/index.ts"
  },
  "imports": {
    "@github.com/Gleamseekers/AgentPrimordia/agentprimordia/edge": "npm:@github.com/Gleamseekers/AgentPrimordia/agentprimordia/edge@^0.1.0"
  },
  "compilerOptions": {
    "strict": true
  }
}
`, name, name)
}

func generateBunfigToml() string {
	return `[install]
peer = false

[run]
watch = true
`
}

func generateAgentConfig(name string) string {
	return fmt.Sprintf(`/**
 * %s — Agent 配置
 */
import type { AgentConfig } from '@github.com/Gleamseekers/AgentPrimordia/agentprimordia/edge';

export const config: AgentConfig = {
  name: '%s',
  systemPrompt: 'You are a helpful edge agent.',
  maxTurns: 10,
  temperature: 0.7,
  tools: [],
  memory: {
    strategy: 'conversation',
    maxHistory: 50,
  },
};
`, name, name)
}
