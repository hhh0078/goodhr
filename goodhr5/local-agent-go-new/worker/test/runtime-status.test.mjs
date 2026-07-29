// 文件作用说明：验证运行状态优先展示 GoodHR 指定的 CloakBrowser 二进制路径。

import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { ActionService } from "../dist/browser/actions/action-service.js";

/** 验证 CLOAKBROWSER_BINARY_PATH 会覆盖 SDK 自身的缓存路径显示。 */
test("运行状态使用配置的浏览器路径", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "goodhr-cloakbrowser-"));
  const binaryPath = path.join(directory, "Chromium");
  const previous = process.env.CLOAKBROWSER_BINARY_PATH;
  try {
    await writeFile(binaryPath, "");
    process.env.CLOAKBROWSER_BINARY_PATH = binaryPath;
    const status = new ActionService().runtimeStatus();
    assert.equal(status.binary_path, binaryPath);
    assert.equal(status.installed, true);
  } finally {
    if (previous === undefined) {
      delete process.env.CLOAKBROWSER_BINARY_PATH;
    } else {
      process.env.CLOAKBROWSER_BINARY_PATH = previous;
    }
    await rm(directory, { recursive: true, force: true });
  }
});
