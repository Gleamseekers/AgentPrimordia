#!/usr/bin/env node
/**
 * doc-version-consistency.mjs — 版本与文档一致性门（V7.3 整改）
 *
 * 背景：项目长期存在多个"版本第二真值"（VERSION 文件、TS package.json、Helm values/chart、
 * 各文档的"当前版本"声明），靠人工批量修复，反复漂移。本脚本把版本真值收敛为**唯一来源**，
 * 并用显式锚点对账，漂移即红。
 *
 * 唯一真值（Single Source of Truth）：
 *   agentprimordia/pkg/agent.go  ->  const Version = "x.y.z"
 *
 * 检查项：
 *   1. 第二真值文件必须等于真值：
 *        agentprimordia/VERSION、sdk/typescript/package.json、
 *        helm values.yaml (image.tag)、helm Chart.yaml (appVersion)
 *   2. 在"当前版本"语义的文档锚点上，捕获到的版本必须等于真值（历史表格/发布说明不在扫描范围）。
 *   3. 统计数字必须与实测一致（README 的测试文件数 / Go 文件数 / 包数 / agent 子包数 /
 *      internal 顶层包数，internal/AGENTS.md 的顶层子包数与 agent 子树 Go 包数，
 *      根 AGENTS.md / CONTRIBUTING.md 的「完整 N 包」）。
 *      —— 2026-10-17 补：此前 "29 包 / 32 子包" 只在人工批次里改，长期漂移，现纳入门禁。
 *
 * 用法：node scripts/doc-version-consistency.mjs
 * 退出码：0 通过；1 存在漂移。
 *
 * 维护约定：新增"当前版本"声明时，在此文件的 ANCHORS 中加入一行锚点；
 * 纯历史记录（发布说明、CHANGELOG、V*路线图、migration、archive）**不要**加入。
 */

import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { resolve, dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const errors = [];
const notes = [];

/** 读取仓库相对路径文本，缺失时抛错 */
function read(rel) {
  return readFileSync(resolve(root, rel), 'utf-8');
}

/** 记录失败 */
function fail(msg) {
  errors.push(msg);
  console.error(`::error::${msg}`);
}

// ===== 1. 提取唯一真值 =====
let truth;
try {
  const m = read('agentprimordia/pkg/agent.go').match(/const\s+Version\s*=\s*"([^"]+)"/);
  if (!m) throw new Error('未匹配 const Version');
  truth = m[1];
} catch (e) {
  console.error(`::error::无法从 agentprimordia/pkg/agent.go 提取版本真值: ${e.message}`);
  process.exit(1);
}

console.log('========================================');
console.log('  版本文档一致性检查（唯一真值对账）');
console.log('========================================');
console.log(`  真值 (pkg/agent.go const Version): ${truth}`);
console.log('');

// ===== 2. 第二真值文件 =====
const SECOND_TRUTHS = [
  ['agentprimordia/VERSION', () => read('agentprimordia/VERSION').trim()],
  ['sdk/typescript/package.json', () => JSON.parse(read('sdk/typescript/package.json')).version],
  [
    'agentprimordia/deploy/helm/agentprimordia/values.yaml (image.tag)',
    () => {
      const m = read('agentprimordia/deploy/helm/agentprimordia/values.yaml').match(/tag:\s*"v?([\d.]+)"/);
      return m ? m[1] : null;
    },
  ],
  [
    'agentprimordia/deploy/helm/agentprimordia/Chart.yaml (appVersion)',
    () => {
      const m = read('agentprimordia/deploy/helm/agentprimordia/Chart.yaml').match(/appVersion:\s*"v?([\d.]+)"/);
      return m ? m[1] : null;
    },
  ],
];

for (const [label, getter] of SECOND_TRUTHS) {
  let got;
  try {
    got = getter();
  } catch (e) {
    fail(`${label} 读取失败: ${e.message}`);
    continue;
  }
  if (got === truth) {
    console.log(`OK: ${label} = ${got}`);
  } else {
    fail(`${label} = ${got ?? '(未匹配)'}，应等于真值 ${truth}`);
  }
}

// ===== 3. CHANGELOG 文件名与真值对齐 =====
// 约定：每个 minor 版本一份 agentprimordia/docs/CHANGELOG-v<major>.<minor>.md
const changelogRel = `agentprimordia/docs/CHANGELOG-v${truth.split('.').slice(0, 2).join('.')}.md`;
if (existsSync(resolve(root, changelogRel))) {
  console.log(`OK: ${changelogRel} 存在`);
} else {
  fail(`缺少 ${changelogRel}（版本真值 ${truth} 应有对应 CHANGELOG）`);
}

// ===== 4. "当前版本"文档锚点 =====// 每条：[文件, 锚点正则（第一个捕获组即版本）]
const ANCHORS = [
  ['README.md', /badge\/version-(\d+\.\d+\.\d+)-/],
  ['agentprimordia/README.md', /badge\/version-v(\d+\.\d+\.\d+)-/],
  ['docs/路线图.md', /\*\*当前版本\*\*：Go SDK v(\d+\.\d+\.\d+)/],
  ['docs/API参考.md', /ap\.Version`\s*=\s*`"(\d+\.\d+\.\d+)"/],
  ['docs/部署指南.md', /镜像 tag v(\d+\.\d+\.\d+)/],
  ['agentprimordia/docs/版本规范.md', /当前版本：`(\d+\.\d+\.\d+)`/],
  ['agentprimordia/docs/版本规范.md', /\|\s*Go SDK\s*\|\s*v?(\d+\.\d+\.\d+)/],
  ['agentprimordia/docs/版本规范.md', /\|\s*TypeScript SDK\s*\|\s*v?(\d+\.\d+\.\d+)/],
  ['agentprimordia/docs/AP-开发手册-EN.md', /@agentprimordia\/sdk v(\d+\.\d+\.\d+)/],
  ['sdk/typescript/docs/cross-language-guide.md', /Go SDK: v(\d+\.\d+\.\d+)/],
  ['sdk/typescript/docs/cross-language-guide.md', /TypeScript SDK: v(\d+\.\d+\.\d+)/],
  ['agentprimordia/ecosystem/docs/ap-guide.md', /AgentPrimordia CLI v(\d+\.\d+\.\d+)/],
];

for (const [file, re] of ANCHORS) {
  let content;
  try {
    content = read(file);
  } catch (e) {
    fail(`${file} 读取失败: ${e.message}`);
    continue;
  }
  const m = content.match(re);
  if (!m) {
    fail(`${file} 中未找到版本锚点 ${re}（文档可能已被改写，请同步更新脚本 ANCHORS）`);
  } else if (m[1] !== truth) {
    fail(`${file} 标注版本 ${m[1]}，应等于真值 ${truth}`);
  }
}

// ===== 5. README 统计数字对账 =====
const MODULE = 'agentprimordia';

/** 递归收集模块内 .go 文件（排除 operator 独立模块、node_modules、.git） */
function collectGoFiles(dir, acc = []) {
  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === '.git' || name === 'operator') continue;
    const full = join(dir, name);
    const st = statSync(full);
    if (st.isDirectory()) {
      collectGoFiles(full, acc);
    } else if (name.endsWith('.go')) {
      acc.push(full);
    }
  }
  return acc;
}

const goFiles = collectGoFiles(resolve(root, MODULE));
const testFileCount = goFiles.filter((f) => f.endsWith('_test.go')).length;
const goFileCount = goFiles.length;
const packageDirCount = new Set(goFiles.map((f) => dirname(f))).size;

let agentSubpkgCount = 0;
try {
  agentSubpkgCount = readdirSync(resolve(root, MODULE, 'internal/agent')).filter((n) =>
    statSync(resolve(root, MODULE, 'internal/agent', n)).isDirectory(),
  ).length;
} catch (e) {
  fail(`无法统计 internal/agent 子包: ${e.message}`);
}

// internal/ 顶层子包数（口径：目录数 == `go list ./internal/... | grep -E '/internal/[^/]+$'`）
let internalTopPkgCount = 0;
try {
  internalTopPkgCount = readdirSync(resolve(root, MODULE, 'internal')).filter((n) =>
    statSync(resolve(root, MODULE, 'internal', n)).isDirectory(),
  ).length;
} catch (e) {
  fail(`无法统计 internal 顶层子包: ${e.message}`);
}

// 「Go 包口径」目录数：递归统计含 .go 文件的目录（跳过 go 工具链忽略的 testdata 与 _/. 前缀目录），
// 与 `go list ./<rel>/... | wc -l` 同值（2026-10-17 实测两者均为 37：HEAD fa622aac = v7.5.1 基线，工作树为 v7.5.2 发布准备）。
function countGoPkgDirs(rel) {
  let n = 0;
  const walk = (dir) => {
    let hasGo = false;
    for (const ent of readdirSync(dir, { withFileTypes: true })) {
      if (ent.isDirectory()) {
        if (ent.name === 'testdata' || ent.name.startsWith('_') || ent.name.startsWith('.')) continue;
        walk(resolve(dir, ent.name));
      } else if (ent.name.endsWith('.go')) {
        hasGo = true;
      }
    }
    if (hasGo) n += 1;
  };
  walk(resolve(root, MODULE, rel));
  return n;
}

let agentSubtreePkgCount = 0;
try {
  agentSubtreePkgCount = countGoPkgDirs('internal/agent');
} catch (e) {
  fail(`无法统计 internal/agent 子树 Go 包: ${e.message}`);
}

const README = read('README.md');
const INTERNAL_ROUTER = read('agentprimordia/internal/AGENTS.md');
const ROOT_AGENTS = read('AGENTS.md');
const CONTRIBUTING = read('agentprimordia/CONTRIBUTING.md');
// 每条：[标签, 待检文本, 锚点正则（第一个捕获组为实测数字）, 期望值]
// 历史上这些数字靠人工批量修（"29 包/32 子包" 长期漂移），故一并纳入门禁。
const STAT_ANCHORS = [
  ['README.md tests badge', README, /badge\/tests-(\d+)%20files/, testFileCount],
  ['README.md 测试文件数', README, /Red → Green → Refactor，(\d+) 个测试文件/, testFileCount],
  ['README.md 包数', README, /核心框架主模块（(\d+) 个包，/, packageDirCount],
  ['README.md Go 文件数', README, /核心框架主模块（\d+ 个包，(\d+) 个 Go 文件）/, goFileCount],
  ['README.md agent 子包数', README, /微内核（(\d+) 个子包）/, agentSubpkgCount],
  ['README.md internal 顶层包数', README, /internal\/\s*#\s*(\d+) 个内部包/, internalTopPkgCount],
  ['internal/AGENTS.md 顶层子包数', INTERNAL_ROUTER, /本目录包含顶层 (\d+) 个子包/, internalTopPkgCount],
  ['internal/AGENTS.md agent 子树 Go 包数', INTERNAL_ROUTER, /Go 包口径 (\d+) 个/, agentSubtreePkgCount],
  ['AGENTS.md 完整 N 包表格', ROOT_AGENTS, /完整 (\d+) 包表格/, internalTopPkgCount],
  ['CONTRIBUTING.md 完整 N 包', CONTRIBUTING, /完整 (\d+) 包见 internal\/AGENTS\.md/, internalTopPkgCount],
];

for (const [label, text, re, want] of STAT_ANCHORS) {
  const m = text.match(re);
  if (!m) {
    fail(`${label}: 未找到统计锚点 ${re}`);
  } else if (Number(m[1]) !== want) {
    fail(`${label}: 文档写 ${m[1]}，实测 ${want}`);
  }
}
notes.push(
  `实测统计: 测试文件 ${testFileCount} / Go 文件 ${goFileCount} / 包目录 ${packageDirCount} / agent 子包 ${agentSubpkgCount} / agent 子树 Go 包 ${agentSubtreePkgCount} / internal 顶层包 ${internalTopPkgCount}`,
);

console.log('');
for (const n of notes) console.log(`INFO: ${n}`);
console.log('');

if (errors.length > 0) {
  console.error(`::error::版本文档一致性检查失败（${errors.length} 项漂移）`);
  console.error('  版本真值唯一来源: agentprimordia/pkg/agent.go 的 const Version');
  console.error('  修复后请重跑: node scripts/doc-version-consistency.mjs');
  process.exit(1);
}

console.log('========================================');
console.log(`  通过：全部版本与文档锚点 == ${truth}`);
console.log('========================================');
