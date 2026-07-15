// 本文件负责验证 Profile 残留浏览器进程清理器的安全输入规则。

import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { terminateProfileBrowserProcesses } from "./profile-process.js";

test("空 Profile 目录不会触发任何进程清理", async () => {
  assert.deepEqual(await terminateProfileBrowserProcesses(""), []);
});

test("不存在的 Profile 不会误杀其他浏览器进程", async () => {
  const target = path.join(os.tmpdir(), `goodhr-profile-not-running-${process.pid}`);
  assert.deepEqual(await terminateProfileBrowserProcesses(target), []);
});
