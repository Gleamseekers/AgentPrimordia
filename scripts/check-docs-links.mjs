#!/usr/bin/env node
// check-docs-links.mjs — v4.4-4 文档死链门
//
// 扫描仓库 Markdown 文档中的相对链接（[text](./path) / [text](path)），
// 断言目标文件/锚点存在；发现死链退出码 1（docs-build CI 门）。
//
// v7.5.2 补扫：Markdown 里内嵌的 HTML 链接（<img src="..."> / <a href="...">）此前完全不检查——
// README 的架构图与"交互式架构图"入口正是用 HTML 写的，模块路径迁移后遗留的
// `github.com/Gleamseekers/...` 前缀因此长期死链而门禁全绿。现两类语法都扫。
//
// 用法：
//   node scripts/check-docs-links.mjs [--root .]
import { readFileSync, existsSync, statSync } from 'node:fs';
import { resolve, dirname, join } from 'node:path';

const args = {};
for (let i = 2; i < process.argv.length; i++) {
  if (process.argv[i] === '--root') args.root = process.argv[++i];
}
const ROOT = resolve(args.root ?? '.');
const SKIP_PREFIX = ['http://', 'https://', 'mailto:', '#', 'skill://', 'rule://'];
const SKIP_DIRS = new Set(['node_modules', '.git', 'dist', '.claude', '.zcode', '.qoder', '.aelacli', '.pdf-build', '.promo-build']);

/** 递归收集 *.md 文件 */
function walk(dir, out) {
  for (const name of readFileSync ? listDir(dir) : []) {
    const p = join(dir, name);
    const st = statSync(p);
    if (st.isDirectory()) {
      if (SKIP_DIRS.has(name)) continue;
      walk(p, out);
    } else if (name.endsWith('.md')) {
      out.push(p);
    }
  }
  return out;
}

import { readdirSync } from 'node:fs';
function listDir(dir) {
  return readdirSync(dir);
}

const files = walk(ROOT, []);
// Markdown 链接语法：[text](path)
const LINK_RE = /\[[^\]]*\]\(([^)]+)\)/g;
// Markdown 内嵌的 HTML 链接：<img src="path"> / <a href="path">（README 架构图就写在 HTML 里）
const ATTR_RE = /(?:src|href)\s*=\s*["']([^"']+)["']/g;
let broken = 0;
let checked = 0;
let brokenHtml = 0;
let checkedHtml = 0;

/** 校验单条相对链接（跳过外链/锚点/站点根路径）；返回是否计入了检查数 */
function verify(raw, file, kind) {
  const link = raw.trim();
  if (SKIP_PREFIX.some((p) => link.startsWith(p))) return false;
  if (link.includes('://')) return false;
  // 站点根路径（vitepress /api/ 等）非文件链接
  if (link.startsWith('/')) return false;
  // 去掉锚点部分
  const target = link.split('#')[0];
  if (!target) return false;
  const abs = resolve(dirname(file), target);
  if (kind === 'html') checkedHtml++;
  else checked++;
  if (!existsSync(abs)) {
    console.error(`${kind === 'html' ? '死链(HTML 内嵌)' : '死链'}: ${file.replace(ROOT, '.')} → ${link}`);
    if (kind === 'html') brokenHtml++;
    else broken++;
  }
  return true;
}

for (const file of files) {
  const raw = readFileSync(file, 'utf8');
  // 剔除围栏代码块与行内代码跨度后再扫描：文档里示范链接语法（如 `[text](path)`）或
  // 贴 HTML 片段时，代码内的"链接"不会被渲染成链接，扫它们只会产生假阳性。
  const content = raw.replace(/```[\s\S]*?```/g, '').replace(/`[^`\n]*`/g, '');
  for (const m of content.matchAll(LINK_RE)) verify(m[1], file, 'md');
  for (const m of content.matchAll(ATTR_RE)) verify(m[1], file, 'html');
}

if (broken + brokenHtml > 0) {
  console.error(
    `\n文档死链门: ${broken + brokenHtml} 条死链（Markdown ${checked} 条 / HTML 内嵌 ${checkedHtml} 条相对链接）`
  );
  process.exit(1);
}
console.log(`文档死链门: 通过（Markdown ${checked} 条 + HTML 内嵌 ${checkedHtml} 条相对链接全部可达）`);
