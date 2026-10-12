// 文件作用说明：验证离线扩展启动参数、保留自定义扩展及缺失文件的可定位错误。
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { offlineAddonOptions } from "../dist/browser/session/bundled-addon.js";
import { addDefaultAddons } from "camoufox-js/dist/addons.js";

const context = { action: "browser.start", trace_id: "offline-addon-test", started_at: Date.now() };

/** fixture 创建独立扩展目录并恢复测试前的环境变量。 */
async function fixture(t) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-offline-addon-"));
  const previous = process.env.GOODHR_UBO_EXTENSION_PATH;
  process.env.GOODHR_UBO_EXTENSION_PATH = root;
  t.after(async () => {
    if (previous === undefined) delete process.env.GOODHR_UBO_EXTENSION_PATH;
    else process.env.GOODHR_UBO_EXTENSION_PATH = previous;
    await fs.rm(root, { recursive: true, force: true });
  });
  await fs.writeFile(path.join(root, "manifest.json"), JSON.stringify({ name: "fixture", version: "1", manifest_version: 2, background: { scripts: ["background.js"] } }));
  return root;
}

/** 验证所有启动参数都排除默认 UBO，依赖不会追加需要下载的扩展。 */
test("本地扩展保留并去重，Camoufox 默认扩展下载被关闭", async (t) => {
  const root = await fixture(t);
  const custom = path.join(root, "custom");
  await fs.mkdir(custom);
  await fs.copyFile(path.join(root, "manifest.json"), path.join(custom, "manifest.json"));
  const input = [custom, root];
  const options = await offlineAddonOptions(input, context);
  assert.deepEqual(options, { addons: [custom, root], exclude_addons: ["UBO"] });
  assert.deepEqual(input, [custom, root]);
  // 直接运行依赖的默认扩展入口：排除 UBO 后既不解析浏览器目录，也不下载。
  await addDefaultAddons(options.addons, options.exclude_addons);
  assert.deepEqual(options.addons, [custom, root]);
});

/** 验证内置目录为空时返回具体路径，而非笼统的启动失败。 */
test("内置目录缺少 manifest 时报告路径与 trace", async (t) => {
  const root = await fixture(t);
  await fs.unlink(path.join(root, "manifest.json"));
  await assert.rejects(offlineAddonOptions(undefined, context), (error) => {
    assert.equal(error.code, "BROWSER_ADDON_INVALID");
    assert.equal(error.details.addon_path, root);
    assert.equal(error.details.bundled, true);
    assert.equal(error.trace_id, context.trace_id);
    return true;
  });
});

/** 验证自定义扩展失败时保留诊断，内置路径缺失时禁止回退到网络下载。 */
test("自定义扩展损坏和内置路径缺失时明确报错", async (t) => {
  const root = await fixture(t);
  const custom = path.join(root, "custom");
  await fs.mkdir(custom);
  await fs.writeFile(path.join(custom, "manifest.json"), "{}");
  await assert.rejects(offlineAddonOptions([custom], context), (error) => {
    assert.equal(error.details.addon_path, custom);
    assert.equal(error.details.bundled, false);
    return true;
  });
  delete process.env.GOODHR_UBO_EXTENSION_PATH;
  await assert.rejects(offlineAddonOptions([], context), { code: "BROWSER_ADDON_INVALID" });
});

/** 验证旧 Chrome service_worker 扩展不会被误当成 Firefox 扩展启动。 */
test("旧 Chrome 插件不能进入 Camoufox 的扩展列表", async (t) => {
  const root = await fixture(t);
  const chrome = path.join(root, "legacy-chrome");
  await fs.mkdir(chrome);
  await fs.writeFile(path.join(chrome, "manifest.json"), JSON.stringify({
    name: "旧插件", version: "1", manifest_version: 3,
    background: { service_worker: "background.js" },
  }));
  await assert.rejects(offlineAddonOptions([chrome], context), (error) => {
    assert.equal(error.code, "BROWSER_ADDON_INVALID");
    assert.equal(error.details.addon_path, chrome);
    return true;
  });
});

/** 验证没有后台脚本的纯内容脚本 Firefox 插件仍能使用。 */
test("纯内容脚本 Firefox 插件仍可加载", async (t) => {
  const root = await fixture(t);
  const contentOnly = path.join(root, "content-only");
  await fs.mkdir(contentOnly);
  await fs.writeFile(path.join(contentOnly, "manifest.json"), JSON.stringify({
    name: "内容脚本插件", version: "1", manifest_version: 3,
    content_scripts: [{ matches: ["https://example.com/*"], js: ["content.js"] }],
  }));
  const result = await offlineAddonOptions([contentOnly], context);
  assert.ok(result.addons?.includes(contentOnly));
});
