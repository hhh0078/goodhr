// 文件作用说明：验证同一持久化 Profile 使用稳定指纹文件，且异常情况下不会阻断启动。

import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { loadStableProfileFingerprint } from "../dist/browser/session/fingerprint.js";

/** 验证首次生成指纹文件后，同一 Profile 复用同一份指纹。 */
test("持久化 Profile 使用稳定指纹文件", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-fp-a-"));
  try {
    const first = await loadStableProfileFingerprint(directory);
    const repeated = await loadStableProfileFingerprint(directory);
    assert.ok(first, "首次应生成指纹");
    assert.ok(repeated, "第二次应读取指纹");
    assert.deepEqual(first, repeated);
    assert.match(first.navigator.userAgent, /Firefox/);
    const saved = JSON.parse(
      await fs.readFile(path.join(directory, "goodhr-fingerprint.json"), "utf8"),
    );
    assert.deepEqual(saved, first);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});

/** 验证不同 Profile 各自生成不同指纹。 */
test("不同 Profile 指纹互不相同", async () => {
  const firstDir = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-fp-b1-"));
  const secondDir = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-fp-b2-"));
  try {
    const first = await loadStableProfileFingerprint(firstDir);
    const second = await loadStableProfileFingerprint(secondDir);
    assert.ok(first && second);
    assert.notDeepEqual(first, second);
  } finally {
    await fs.rm(firstDir, { recursive: true, force: true });
    await fs.rm(secondDir, { recursive: true, force: true });
  }
});

/** 验证损坏的指纹文件会被重新生成，空目录入参直接跳过。 */
test("损坏指纹文件自动重建并容忍空入参", async () => {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-fp-c-"));
  try {
    await fs.writeFile(
      path.join(directory, "goodhr-fingerprint.json"),
      "{not-json",
      "utf8",
    );
    const recovered = await loadStableProfileFingerprint(directory);
    assert.ok(recovered, "损坏文件后应重新生成指纹");
    assert.equal(await loadStableProfileFingerprint(undefined), undefined);
    assert.equal(await loadStableProfileFingerprint("  "), undefined);
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
});
