/** 本文件防止后台页面重新写入旧版固定绿色，确保会员主题能完整生效。 */

import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { extname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const frontendRoot = fileURLToPath(new URL("..", import.meta.url));
const sourceDirectories = [
  join(frontendRoot, "app", "admin"),
  join(frontendRoot, "components", "admin"),
];
const forbiddenFixedGreens = [
  "#0f754a",
  "#15271e",
  "#15945f",
  "#15804f",
  "#16724c",
  "#1b241e",
  "#1e6545",
  "#1f7048",
  "#22372c",
  "#244d3b",
  "#285f44",
  "#2f6f4f",
  "#46524c",
  "#4d8d68",
  "#4f5e56",
  "#52665a",
  "#54635a",
  "#718078",
  "#82a891",
  "#87aa92",
  "#89958f",
  "#97a39d",
  "#9fbca9",
  "#b9d4c1",
  "#cbdccf",
  "#cbded4",
  "#cfe0d3",
  "#cfe4d6",
  "#d5e5d9",
  "#d7e3da",
  "#d8e4dc",
  "#d8e6db",
  "#dce5e0",
  "#dcece1",
  "#dff0e4",
  "#e7efe9",
  "#e7f1ea",
  "#e7f5ed",
  "#e9f2ec",
  "#eaf3ed",
  "#edf5ef",
  "#edf5f0",
  "#edf6ef",
  "#eef3f0",
  "#eef4f0",
  "#eef6f0",
  "#eef7f1",
  "#f0f7f2",
  "#f2f7f3",
  "#f2f7f4",
  "#f3f8f4",
  "#f3f8f5",
  "#f3faf5",
  "#f4f7f5",
  "#f4f8f5",
  "#f5f8f6",
  "#f6faf7",
  "#f7faf8",
  "#f7fbf6",
  "#f8faf8",
  "#f8faf9",
  "#f8fbf8",
  "#fafbfa",
  "#fbfcfb",
  "#fbfdfc",
];
const forbiddenGreenShadows = [
  "rgba(21,154,98,.2)",
  "rgba(31,54,42,.06)",
  "rgba(31,54,42,.07)",
  "rgba(31,54,42,.08)",
];

/**
 * collectSourceFiles 递归收集目录中的前端源码文件。
 * @param {string} directory - 要扫描的源码目录。
 * @returns {string[]} 目录内可检查的前端源码文件路径。
 */
function collectSourceFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const target = join(directory, entry.name);
    if (entry.isDirectory()) return collectSourceFiles(target);
    return [".ts", ".tsx", ".js", ".jsx"].includes(extname(entry.name))
      ? [target]
      : [];
  });
}

test("后台装饰颜色统一使用会员主题变量", () => {
  const violations = [];

  for (const filePath of sourceDirectories.flatMap(collectSourceFiles)) {
    const source = readFileSync(filePath, "utf8").toLowerCase();
    for (const color of [...forbiddenFixedGreens, ...forbiddenGreenShadows]) {
      if (source.includes(color)) {
        violations.push(`${filePath.replace(`${frontendRoot}/`, "")}: ${color}`);
      }
    }
  }

  assert.deepEqual(violations, []);
});
