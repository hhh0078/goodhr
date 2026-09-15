// 文件作用说明：验证运行状态优先展示 GoodHR 指定的 Camoufox 二进制路径。

import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile, mkdir } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { ActionService } from "../dist/browser/actions/action-service.js";

/** 验证 CAMOUFOX_BINARY_PATH 会覆盖默认安装目录的路径显示。 */
test("运行状态使用配置的浏览器路径", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "goodhr-camoufox-"));
  const binaryPath = path.join(directory, "camoufox");
  const previousBinary = process.env.CAMOUFOX_BINARY_PATH;
  const previousInstall = process.env.CAMOUFOX_INSTALL_DIR;
  try {
    await writeFile(binaryPath, "");
    process.env.CAMOUFOX_BINARY_PATH = binaryPath;
    process.env.CAMOUFOX_INSTALL_DIR = directory;
    const status = new ActionService().runtimeStatus();
    assert.equal(status.binary_path, binaryPath);
    assert.equal(status.installed, true);
    assert.equal(typeof status.camoufox_version, "string");
  } finally {
    restoreEnv("CAMOUFOX_BINARY_PATH", previousBinary);
    restoreEnv("CAMOUFOX_INSTALL_DIR", previousInstall);
    await rm(directory, { recursive: true, force: true });
  }
});

/** 验证安装目录内可以自动找到 Camoufox 启动文件并读取版本。 */
test("默认安装目录自动探测启动文件和版本", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "goodhr-camoufox-"));
  const binaryPath = path.join(directory, "Camoufox.app", "Contents", "MacOS");
  const previousBinary = process.env.CAMOUFOX_BINARY_PATH;
  const previousInstall = process.env.CAMOUFOX_INSTALL_DIR;
  try {
    delete process.env.CAMOUFOX_BINARY_PATH;
    await mkdir(binaryPath, { recursive: true });
    await writeFile(path.join(binaryPath, "camoufox"), "");
    await writeFile(
      path.join(directory, "version.json"),
      JSON.stringify({ version: "135.0.1", release: "beta.24" }),
      "utf8",
    );
    process.env.CAMOUFOX_INSTALL_DIR = directory;
    const status = new ActionService().runtimeStatus();
    assert.equal(status.binary_path, path.join(binaryPath, "camoufox"));
    assert.equal(status.installed, true);
    assert.equal(status.camoufox_version, "135.0.1-beta.24");
  } finally {
    restoreEnv("CAMOUFOX_BINARY_PATH", previousBinary);
    restoreEnv("CAMOUFOX_INSTALL_DIR", previousInstall);
    await rm(directory, { recursive: true, force: true });
  }
});

/** restoreEnv 恢复环境变量原值，避免测试相互影响。 */
function restoreEnv(name, previous) {
  if (previous === undefined) {
    delete process.env[name];
  } else {
    process.env[name] = previous;
  }
}
