# Security Model

AgentPrimordia makes a narrow, deliberately boring promise about code safety — and
backs it with machine-checked assertions rather than policy prose:

> **INV-0** — the host process never writes, compiles, or loads any agent-generated
> code at runtime. The only legitimate execution location for agent-generated code
> is the wazero WASM sandbox, and only after signature verification may it be
> registered as a tool.

This page explains what that invariant covers, how each part of it is verified in
CI, where the signature gate sits, and — just as important — what the promise does
**not** cover. Read [Honest boundaries](#honest-boundaries-read-this-first) before
trusting the page title.

## What the host refuses to do

Three host-side actions are permanently off the table for agent-generated code.
Each is enforced by a deterministic assertion (A1–A8, below), not by convention:

| Host-side action | Status | Enforced by |
|---|---|---|
| Write agent-generated code into the host source tree, `go.mod`, or config directories | Never | A8 — host working tree hash must be unchanged across tool execution |
| Compile, link, or dynamically load agent-generated code (`go build`, `plugin.Open`, cgo) | Never | A2 — source scan + symbol scan of the built CLI binary |
| Spawn agent-generated code as a host process (`os/exec`, including write-then-run) | Never | A3 — host code-execution tool is default-off and its switch is unreachable from any agent-writable config surface |

The built-in `code_execution` tool deserves a special mention because it is
frequently mistaken for a sandbox. Its own description states it plainly:
*"This is NOT a security sandbox. Code runs directly on the host with the
privileges of the AgentPrimordia process."* It is disabled by default
(`AP_ALLOW_CODE_EXECUTION`), and assertion A3 additionally proves no production
code path can set that variable — an agent that "writes a config and restarts"
cannot open it.

The host command layer outside the sandbox (`internal/security`: `CanExecute`,
`CanAccess`, `ValidatePath`) is a command whitelist plus ACL and path validation —
**not** a syscall sandbox. That is precisely why agent-generated code is never
allowed onto the host path: a whitelist is not strong enough to contain
machine-written code, a WASM sandbox with explicit resource limits is.

## Verifiable by construction: assertions A1–A8

The invariant is defined as eight deterministic assertions (proposal
[code-layer sandbox controlled release](../../../docs/提案-code层沙箱受控释放.md)
§2.2). All of them run in CI on every change; a failure is red, not a warning.
"Exhaustive" below means the checked surface is finite and machine-enumerable —
the test walks the whole surface, it does not sample.

| # | Assertion | What it checks | Test location |
|---|---|---|---|
| A1 | wazero dependency boundary | The set of packages importing `github.com/tetratelabs/wazero` is confined to `agentprimordia/wasm` and the root `wasm/` module — verified by enumerating the full import graph (`go list`) of both modules | `internal/security/host_boundary_test.go` |
| A2 | Zero dynamic loading | No `plugin.Open` anywhere in the source tree; the CLI binary built with `CGO_ENABLED=0` contains no `plugin.Open` symbol | `internal/security/host_boundary_test.go` |
| A3 | Zero host-derived execution | `code_execution` refuses to run when `AP_ALLOW_CODE_EXECUTION` is unset (and names the switch in its error); no production `os.Setenv` writes that variable | `internal/security/host_boundary_test.go` |
| A4 | Sandbox resource caps | Every sandbox instance carries a memory-page limit and an execution timeout; a zero-value config falls back to the safe defaults (256 pages / 30s) instead of "unlimited" | `wasm/sandbox_test.go` |
| A5 | CPU infinite loops are terminable | With close-on-context-done enabled, a deliberately infinite WASM loop is really terminated inside the execution timeout | `wasm/sandbox_boundary_test.go` |
| A6 | Signature before registration | A tool with a missing, malformed, or tampered signature is refused **before** its module is compiled or loaded (byte-flip table over the artifact) | `wasm/sandbox_boundary_test.go` |
| A7 | Import-section whitelist | A module's imported functions are enumerated after compilation and compared against the allowlist; unapproved host imports (including WASI when not allowlisted) are refused | `wasm/sandbox_boundary_test.go` |
| A8 | Host file system zero writes | The hash of the host working tree (Go sources + config dirs) is identical before and after tool execution; generated artifacts land only in the sandbox data area | `wasm/sandbox_boundary_test.go` |

On top of A1–A8, a red-team regression suite replays 700 frozen adversarial
samples (routed per attack family: prompt-injection, skill-card-poison,
bad-tool-package, adapter-tamper, reputation-gaming, benign-control) on every run,
holdout subset included — `internal/security/redteam_regression_test.go`, over a
question set frozen with a sha256 manifest under `docs/evals/`.

### Why "0 violations" is an honest number here

Project rule R3 (V7 roadmap §1) forbids bare 100%/0-tolerance claims for *quality*
metrics — a task success rate of 100% is always a small-sample observation and must
be reported as point estimate + Wilson bound. The security invariant is explicitly
exempt, and the reason is structural: the judgments are **algorithms, not
samples**. Import-graph comparison, symbol scanning, Ed25519 verification, and
import-section enumeration are deterministic functions over finite, enumerable
surfaces. "0 violations" is a precise statement about algorithm output, not a
statistical boast. If a future platform change makes any assertion
non-exhaustive, the correct response is a formal security-promise change
(maintainer written approval, proposal §7) — never a quiet downgrade to sampling.

## The signature gate: ed25519 + public-key pinning

Signing covers the WASM bytecode itself: an Ed25519 signature over
`SHA-256(wasmBytes)` (`agentprimordia/wasm/signing.go`). The gate is
**fail-closed** and sits in front of everything else:

1. `WASMToolAdapter.RegisterTool` refuses any tool whose metadata lacks a
   signature or public key, or whose bytes fail `VerifySignature` — before
   compilation, before loading into the sandbox.
2. The trust anchor is an operator-held public key. The matching key fingerprint
   is what an auditor records; an agent must not be able to mint its own key and
   self-sign into the registry.
3. One layer up, the generated-tool registration path
   (`internal/tools/intelligence.RegisteringCreator`) applies the same default:
   with no verifier injected it refuses every artifact.

A minimal program that exercises the gate — unsigned and tampered artifacts are
refused, a properly signed artifact passes:

```go
package main

import (
	"context"
	"encoding/json"
	"log"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

func main() {
	ctx := context.Background()

	sandbox := ap.NewWASMSandbox(ap.DefaultWASMSandboxConfig())
	defer sandbox.Close()
	adapter := ap.NewWASMToolAdapter(sandbox)

	// The artifact you built and signed offline. A real tool module exports
	// alloc + memory plus its execute function; these 8 bytes are just the
	// WASM magic and version.
	wasmBytes := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

	// 1) Unsigned artifact: refused before anything is compiled or loaded.
	if err := adapter.RegisterTool(ctx, ap.WASMToolMetadata{
		Name:        "unsigned-tool",
		ExecuteFunc: "run",
	}, wasmBytes); err != nil {
		log.Printf("unsigned artifact refused: %v", err)
	}

	// 2) Operator-side signing. The private key stays on the developer
	// workstation; the public key is pinned as the trust anchor.
	privateKey, _, err := ap.WASMGenerateKeyPair()
	if err != nil {
		log.Fatalf("generate key pair: %v", err)
	}
	signature, publicKey, err := ap.WASMSignWASM(wasmBytes, privateKey)
	if err != nil {
		log.Fatalf("sign artifact: %v", err)
	}
	if err := ap.WASMVerifySignature(wasmBytes, signature, publicKey); err != nil {
		log.Fatalf("verify artifact: %v", err)
	}

	// 3) Signed + pinned: the gate passes and the module proceeds to
	// compilation, import-whitelist check, and registration.
	signed := ap.WASMToolMetadata{
		Name:        "calculator",
		Description: "Evaluate an arithmetic expression",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"expression":{"type":"string"}}}`),
		ExecuteFunc: "calculate",
		Version:     "1.0.0",
		Signature:   signature,
		PublicKey:   publicKey,
	}
	if err := adapter.RegisterTool(ctx, signed, wasmBytes); err != nil {
		log.Fatalf("register signed tool: %v", err)
	}

	// 4) One flipped bit with the old signature: the Ed25519 check fails and
	// the tampered artifact never reaches the sandbox.
	tampered := append([]byte(nil), wasmBytes...)
	tampered[len(tampered)-1] ^= 0xff
	if err := adapter.RegisterTool(ctx, signed, tampered); err != nil {
		log.Printf("tampered artifact refused: %v", err)
	}
}
```

The WASM tool surface itself is **Experimental** (`pkg/wasm.go`): the security
invariants around it are deterministic promises, but the API shape may still
change between minor versions.

## The learning feedback channel is unchanged

The self-improvement feedback loop still refuses code-layer suggestions
outright. `learning.FeedbackLoop.Propose` rejects any suggestion with
`ScopeCode` (`ErrImprovementScopeViolation`: code-layer changes are forbidden by
the sandbox and require human code review), and the applicable self-improvement
scopes remain prompt / config / skill. The sandbox release added a new,
auditable channel; it did not weaken the old one.

## Honest boundaries (read this first)

- **There is no production tool generator.** The six-stage lifecycle framework
  (`internal/tools/lifecycle`) and its autonomous closed loop
  (gap detection → generation → rehearsal → adversarial testing → signed
  registration) exist and are covered by end-to-end tests — but every
  `Generator` implementation in the tree is a test double. Generation quality
  (turning a capability gap into correct WASM) is a research surface and is
  registered as research-blocked. **Agents do not write and run their own tools
  in production today.**
- **The correct positioning of the sandbox pipeline is a secure supply chain for
  developer-supplied WASM artifacts**: you bring the `.wasm`, you sign it with a
  pinned key, the agent consumes it inside the sandbox. Nothing on this page
  should be read as "the agent authors its own code".
- **CPU quota is time-based only.** The `MaxFuel` field is forward-compatible
  plumbing; enforcement today is the execution timeout (A5). CPU fairness across
  concurrent modules in one process is not guaranteed.
- **WASI is an allowlist entry, not a hard ban.** The default allowlist contains
  `wasi_snapshot_preview1`; A7 is what refuses anything not explicitly approved.
  Who may widen the allowlist belongs to runtime policy (governance), not to the
  agent.
- **Sandbox execution is Go-only.** The TypeScript side has protocol parity
  (signing, tool-package format, registration client) but no wazero equivalent;
  TS delegates to a gateway node whose isolation is outside this workspace's
  verified scope.
- **"Verified" means machine-checked, not vulnerability-free.** The claim is that
  every boundary statement on this page corresponds to a deterministic test in
  CI. It is not a claim that the sandbox, wazero, or the host OS has no bugs.

## See also

- [Multi-Agent Interop (A2A)](multi-agent-interop.md) — the two protocol faces and their verified limitations
- [AP Development Handbook (EN)](../AP-开发手册-EN.md) — terminology baseline
- [Guardrails](安全护栏.md) — input/output guardrails wired into the ReAct loop
- [Five-minute quickstart](../getting-started/five-minutes.md) — get an agent running first
- [Experimental capabilities registry](../../../docs/实验性能力清单.md) — what is wired, partial, or reserved
- Proposal: [code-layer sandbox controlled release](../../../docs/提案-code层沙箱受控释放.md) — the full policy text in which INV-0 and A1–A8 are defined
