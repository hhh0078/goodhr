// 文件作用说明：验证新配置先完成中文注册、旧配置跳过初始化以及失败时不放行。
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { initializeChineseLanguagePack } from "../dist/browser/session/language-initializer.js";

/** fixture 创建隔离的浏览器与配置目录。 */
async function fixture(t) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-language-test-"));
  t.after(() => fs.rm(root, { recursive: true, force: true }));
  const extensions = path.join(root, "browser", "extensions");
  await fs.mkdir(extensions, { recursive: true });
  await fs.writeFile(path.join(extensions, "langpack-zh-CN@firefox.mozilla.org.xpi"), "fixture");
  return { root, options: { executablePath: path.join(root, "camoufox.exe"), headless: false } };
}

/** 验证初始化在关闭后保存注册结果，并且第二次不会再启动后台浏览器。 */
test("首次隐藏注册中文，后续直接启动", async (t) => {
  const { root, options } = await fixture(t);
  let launches = 0;
  await initializeChineseLanguagePack(root, options, async (profile, launchOptions) => {
    launches++;
    assert.equal(profile, root);
    assert.equal(launchOptions.headless, true);
    return { async close() {
      await fs.writeFile(path.join(root, "extensions.json"), JSON.stringify({ addons: [{ id: "langpack-zh-CN@firefox.mozilla.org", active: true }] }));
    } };
  });
  await initializeChineseLanguagePack(root, options, async () => { throw new Error("不应再次启动"); });
  assert.equal(launches, 1);
  assert.equal(options.headless, false);
});

/** 验证未启用的语言包不会误判成功，也不会无限重启。 */
test("注册失败会中止，不循环弹窗", async (t) => {
  const { root, options } = await fixture(t);
  await fs.writeFile(path.join(root, "extensions.json"), JSON.stringify({ addons: [{ id: "langpack-zh-CN@firefox.mozilla.org", active: false }] }));
  let launches = 0;
  await assert.rejects(initializeChineseLanguagePack(root, options, async () => {
    launches++;
    return { async close() {} };
  }), /初始化没有完成/);
  assert.equal(launches, 1);
});

/** 验证缺少语言包时仍可沿用原来的启动流程。 */
test("没有语言包不额外启动", async (t) => {
  const { root, options } = await fixture(t);
  await fs.rm(path.join(root, "browser", "extensions"), { recursive: true });
  await initializeChineseLanguagePack(root, options, async () => { throw new Error("不应启动"); });
});
