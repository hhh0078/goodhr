// 文件作用说明：显式启用时在独立 Camoufox Profile 中验证临时扩展重启、同 ID 升级保留合成 storage；默认跳过且永不下载浏览器。
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { firefox } from "playwright-core";

const executable = process.env.CAMOUFOX_SMOKE_EXECUTABLE?.trim();
const extensionID = "synthetic-goodhr-temp-storage@example.test";
const sentinel = "synthetic-storage-marker-never-a-token";

/** runtimePaths 验证显式提供的本机运行组件，兼容 Windows/Linux 目录及 macOS app 布局，不查找或安装浏览器。 */
async function runtimePaths(configuredExecutable) {
  assert.ok(path.isAbsolute(configuredExecutable), "CAMOUFOX_SMOKE_EXECUTABLE 必须是现有浏览器的绝对路径");
  const executablePath = await fs.realpath(configuredExecutable);
  assert.equal((await fs.stat(executablePath)).isFile(), true);
  const installDirectory = path.resolve(process.env.CAMOUFOX_SMOKE_INSTALL_DIR?.trim() || (process.platform === "darwin" ? path.resolve(path.dirname(executablePath), "../../..") : path.dirname(executablePath)));
  const resourceDirectory = process.platform === "darwin" ? path.join(installDirectory, "Camoufox.app", "Contents", "Resources") : installDirectory;
  const version = JSON.parse(await fs.readFile(path.join(installDirectory, "version.json"), "utf8"));
  assert.equal(typeof version.version, "string"); assert.equal(typeof version.release, "string");
  assert.equal((await fs.stat(path.join(resourceDirectory, "properties.json"))).isFile(), true);
  return { executablePath, installDirectory };
}

/** writeSyntheticAddon 生成固定测试 ID 的最小 Firefox 扩展，只写合成哨兵并向本测试 loopback 地址报告。 */
async function writeSyntheticAddon(directory, version, receiptURL) {
  await fs.mkdir(directory, { recursive: true });
  await fs.writeFile(path.join(directory, "manifest.json"), JSON.stringify({
    manifest_version: 3, name: "GoodHR 临时扩展 storage 合成测试", version,
    permissions: ["storage"], host_permissions: ["http://127.0.0.1/*"],
    background: { scripts: ["background.js"] },
    browser_specific_settings: { gecko: { id: extensionID, strict_min_version: "140.0" } },
  }));
  await fs.writeFile(path.join(directory, "background.js"), `// 文件作用说明：只读写本合成扩展的 storage，回执仅发送至临时 loopback 测试服务。\n(async () => {
    const previous = await browser.storage.local.get(["test_sentinel", "test_boots"]);
    const receipt = { version: browser.runtime.getManifest().version, before: previous.test_sentinel ?? null, boots_before: previous.test_boots ?? 0, uuid: new URL(browser.runtime.getURL("/")).hostname };
    await browser.storage.local.set({ test_sentinel: ${JSON.stringify(sentinel)}, test_boots: (previous.test_boots ?? 0) + 1 });
    await fetch(${JSON.stringify(receiptURL)}, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(receipt) });
  })().catch((error) => console.error("合成 storage 测试失败", error.message));`);
}

/** receiptServer 仅绑定 127.0.0.1 的临时端口，拒绝超量或非合成回执，不接受账号、Token 或招聘资料。 */
async function receiptServer() {
  const receipts = [];
  const server = http.createServer((request, response) => {
    if (request.method !== "POST" || request.url !== "/synthetic-storage") { response.writeHead(404); response.end(); return; }
    let body = "";
    request.on("data", (chunk) => { body += chunk; if (body.length > 4096) request.destroy(); });
    request.on("end", () => {
      try {
        const value = JSON.parse(body);
        assert.deepEqual(Object.keys(value).sort(), ["before", "boots_before", "uuid", "version"]);
        assert.ok([null, sentinel].includes(value.before)); assert.ok(Number.isInteger(value.boots_before) && value.boots_before >= 0 && value.boots_before <= 2);
        assert.ok(["1.0.0", "1.0.1"].includes(value.version)); assert.match(value.uuid, /^[a-f0-9-]{36}$/);
        receipts.push(value); response.writeHead(200); response.end("ok");
      } catch { response.writeHead(400); response.end(); }
    });
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  return { receipts, server, url: `http://127.0.0.1:${server.address().port}/synthetic-storage` };
}

/** nextReceipt 等待实际扩展后台执行后的最小回执，不把安装目录或 UUID 文件存在当作运行成功。 */
async function nextReceipt(receipts) {
  const deadline = Date.now() + 15000;
  while (!receipts.length && Date.now() < deadline) await new Promise((resolve) => setTimeout(resolve, 100));
  assert.ok(receipts.length, "合成扩展未返回实际 storage 回执");
  return receipts.shift();
}

/** launchOnce 使用同一独立 Profile 正常启动并关闭一次，临时安装由 Camoufox 自身完成，不手动卸载扩展。 */
async function launchOnce(launchOptions, paths, profile, addon, receiver) {
  let context;
  try {
    const options = await launchOptions({ headless: true, addons: [addon], exclude_addons: ["UBO"], env: { ...process.env, MOZ_DISABLE_NONLOCAL_CONNECTIONS: "1" } });
    assert.equal(await fs.realpath(options.executablePath), paths.executablePath, "SDK 实际启动文件必须与显式提供的浏览器一致");
    context = await firefox.launchPersistentContext(profile, { ...options, viewport: null, timeout: 30000 });
    await context.route(/^https?:\/\//, (route) => route.request().url() === receiver.url ? route.continue() : route.abort());
    return { firefox: context.browser()?.version(), ...await nextReceipt(receiver.receipts) };
  } finally { await context?.close(); }
}

test("实际 Camoufox 临时扩展正常重启与同 ID 升级保留 storage，无需保留卸载数据的全局偏好", {
  skip: executable ? false : "需要显式设置 CAMOUFOX_SMOKE_EXECUTABLE；默认不启动或下载真实浏览器，Mac 须在对应机器单独验证",
  timeout: 120000,
}, async (t) => {
  const paths = await runtimePaths(executable), temporary = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-camoufox-storage-"));
  const oldInstall = process.env.CAMOUFOX_INSTALL_DIR, oldSkip = process.env.PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD, originalFetch = globalThis.fetch;
  let receiver, fetcher, originalInstall;
  try {
    process.env.CAMOUFOX_INSTALL_DIR = paths.installDirectory; process.env.PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = "1";
    globalThis.fetch = async () => { throw new Error("实际浏览器测试禁止 Node 下载或外部网络请求"); };
    const pkg = await import("camoufox-js/dist/pkgman.js"); fetcher = pkg.CamoufoxFetcher;
    originalInstall = fetcher.prototype.install;
    fetcher.prototype.install = async () => { throw new Error("实际浏览器测试只接受现有运行组件，禁止自动下载安装"); };
    const { launchOptions } = await import("camoufox-js");
    receiver = await receiptServer();
    const addon = path.join(temporary, "synthetic-addon"), profile = path.join(temporary, "profile");
    await writeSyntheticAddon(addon, "1.0.0", receiver.url);
    const first = await launchOnce(launchOptions, paths, profile, addon, receiver);
    const second = await launchOnce(launchOptions, paths, profile, addon, receiver);
    await writeSyntheticAddon(addon, "1.0.1", receiver.url);
    const third = await launchOnce(launchOptions, paths, profile, addon, receiver);
    assert.equal(first.before, null); assert.equal(first.boots_before, 0);
    assert.equal(second.before, sentinel); assert.equal(second.boots_before, 1);
    assert.equal(third.before, sentinel); assert.equal(third.boots_before, 2); assert.equal(third.version, "1.0.1");
    assert.equal(second.uuid, first.uuid); assert.equal(third.uuid, first.uuid);
    assert.equal(receiver.receipts.length, 0, "每次启动仅有一份合成回执");
    t.diagnostic(JSON.stringify({ platform: process.platform, firefox: first.firefox, restart_retained: true, same_id_upgrade_retained: true, uuid_stable: true, real_account_used: false, uninstall_preservation_preferences_changed: false }));
  } finally {
    if (receiver) { receiver.server.closeAllConnections(); await new Promise((resolve) => receiver.server.close(resolve)); }
    globalThis.fetch = originalFetch;
    if (fetcher && originalInstall) fetcher.prototype.install = originalInstall;
    if (oldInstall === undefined) delete process.env.CAMOUFOX_INSTALL_DIR; else process.env.CAMOUFOX_INSTALL_DIR = oldInstall;
    if (oldSkip === undefined) delete process.env.PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD; else process.env.PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD = oldSkip;
    assert.equal(path.dirname(path.resolve(temporary)), path.resolve(os.tmpdir())); assert.ok(path.basename(temporary).startsWith("goodhr-camoufox-storage-"));
    await fs.rm(temporary, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
  }
});
