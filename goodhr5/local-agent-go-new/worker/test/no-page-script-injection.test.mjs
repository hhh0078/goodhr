// 文件作用说明：扫描 Worker 源码，防止招聘页面操作重新引入 JavaScript 注入。

import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const sourceDirectory = fileURLToPath(new URL("../src", import.meta.url));
const forbiddenPattern =
  /\.(?:evaluate|evaluateHandle|\$eval|\$\$eval|addScriptTag|addInitScript|dispatchEvent)\s*\(/;

/** sourceFiles 递归返回源码目录内的全部 TypeScript 文件。 */
async function sourceFiles(directory) {
  const entries = await fs.readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const fullPath = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await sourceFiles(fullPath)));
    } else if (entry.isFile() && entry.name.endsWith(".ts")) {
      files.push(fullPath);
    }
  }
  return files;
}

test("Worker 源码不得向招聘页面注入或执行 JavaScript", async () => {
  const violations = [];
  for (const filePath of await sourceFiles(sourceDirectory)) {
    const source = await fs.readFile(filePath, "utf8");
    if (forbiddenPattern.test(source)) {
      violations.push(path.relative(sourceDirectory, filePath));
    }
  }
  assert.deepEqual(violations, []);
});
