#!/usr/bin/env node
/**
 * 语言包一致性检查：
 * 1. zh.json 与 en.json 的键集合必须完全一致；
 * 2. 源码中出现的 t('x.y') 静态键必须存在于语言包；
 * 3. 报告语言包中未被引用的键（仅提示，不视为失败）。
 *
 * 用法：node scripts/check-locales.mjs
 */
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const localesDir = join(root, 'src/i18n/locales');
const srcDir = join(root, 'src');

const zh = JSON.parse(readFileSync(join(localesDir, 'zh.json'), 'utf8'));
const en = JSON.parse(readFileSync(join(localesDir, 'en.json'), 'utf8'));

const flatten = (obj, prefix = '') =>
  Object.entries(obj).flatMap(([key, value]) =>
    value && typeof value === 'object' ? flatten(value, `${prefix}${key}.`) : [`${prefix}${key}`],
  );

const zhKeys = new Set(flatten(zh));
const enKeys = new Set(flatten(en));

let failed = false;

const onlyZh = [...zhKeys].filter((key) => !enKeys.has(key));
const onlyEn = [...enKeys].filter((key) => !zhKeys.has(key));

if (onlyZh.length || onlyEn.length) {
  failed = true;
  console.error('✖ 语言包键不一致');
  if (onlyZh.length) console.error(`  仅存在于 zh.json: ${onlyZh.join(', ')}`);
  if (onlyEn.length) console.error(`  仅存在于 en.json: ${onlyEn.join(', ')}`);
} else {
  console.log(`✓ 语言包键一致（${zhKeys.size} 个键）`);
}

const walk = (dir) =>
  readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) return walk(full);
    return /\.(ts|tsx)$/.test(entry) ? [full] : [];
  });

const usedKeys = new Set();
const tCall = /\bt\(\s*'([A-Za-z0-9_.]+)'/g;

for (const file of walk(srcDir)) {
  const source = readFileSync(file, 'utf8');
  for (const match of source.matchAll(tCall)) {
    usedKeys.add(match[1]);
  }
}

const missing = [...usedKeys].filter((key) => !zhKeys.has(key)).sort();

if (missing.length) {
  failed = true;
  console.error(`✖ 源码引用了不存在的文案键（${missing.length} 个）：`);
  for (const key of missing) console.error(`  - ${key}`);
} else {
  console.log(`✓ 源码引用的文案键全部存在（静态引用 ${usedKeys.size} 个）`);
}

const unused = [...zhKeys].filter((key) => !usedKeys.has(key) && !key.endsWith('.locale')).sort();
if (unused.length) {
  console.log(`ℹ 暂未被静态引用的键（${unused.length} 个，可能通过变量拼接使用）：`);
  console.log(`  ${unused.join(', ')}`);
}

process.exit(failed ? 1 : 0);
