/* 文件作用：验证固定本地构建不改正式源码或包、权限不扩，以及实际后台请求和配对地址隔离。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), vm = require("node:vm"), crypto = require("node:crypto");
const { build, manifest, parseBuildOptions } = require("../scripts/build.js");
const ROOT = path.resolve(__dirname, ".."), OUT = path.join(ROOT, "artifacts", "build-local-test");

/** hashes 记录目录内文件校验值，校验本地构建不会触碰正式源文件和已有正式包。 */
function hashes(directory, prefix = "") {
  if (!fs.existsSync(directory)) return {};
  return Object.fromEntries(fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name)).flatMap((entry) => {
    const filename = path.join(directory, entry.name), key = path.posix.join(prefix, entry.name);
    return entry.isDirectory() ? Object.entries(hashes(filename, key)) : [[key, crypto.createHash("sha256").update(fs.readFileSync(filename)).digest("hex")]];
  }));
}

const sourceBefore = hashes(path.join(ROOT, "src")), productionBefore = hashes(path.join(ROOT, "dist")), existingLocalBefore = hashes(path.join(ROOT, "dist-local"));
const installBefore = fs.readFileSync(path.join(ROOT, "INSTALL.md"));
test.before(() => { build({ local: true, test: true }); });

/** localFixture 运行已构建的本地后台，所有网络由内存替身处理，不能访问真实候选人或服务器。 */
function localFixture(approveOrigin = "http://127.0.0.1:5173") {
  const values = { data_use_accepted: true, session: { token: "local-private-token", user: { id: "synthetic-local", email: "local@example.test" } } }, requests = [], opened = [];
  let listener;
  const extension = {
    runtime: { id: "synthetic-local-extension", getURL: (file) => "chrome-extension://synthetic-local-extension/" + file, getManifest: () => manifest("chrome", true), onMessage: { addListener: (handler) => { listener = handler; } } },
    storage: { local: { get: async (keys) => Object.fromEntries(keys.filter((key) => key in values).map((key) => [key, values[key]])), set: async (input) => Object.assign(values, input), remove: async (keys) => { for (const key of Array.isArray(keys) ? keys : [keys]) delete values[key]; }, setAccessLevel: async () => {} } },
    tabs: { query: async () => [], create: async ({ url }) => { opened.push(url); return {}; }, onRemoved: { addListener: () => {} }, onUpdated: { addListener: () => {} } },
    action: { setBadgeText: () => {}, setBadgeBackgroundColor: () => {}, setTitle: () => {} }
  };
  const context = vm.createContext({ chrome: extension, URL, AbortSignal, crypto: crypto.webcrypto, Uint8Array, TextEncoder, console, setTimeout, clearTimeout, fetch: async (url, options) => {
    requests.push({ url, options });
    const input = JSON.parse(options.body || "{}");
    const body = url.endsWith("/pair/start") ? { ok: true, data: { pairing_id: "synthetic-pair", expires_in: 120, approve_url: approveOrigin + "/admin/resumes?extension_pair=synthetic-pair&agent_port=43139" } }
      : url.endsWith("/extension-login") ? { token: "local-login-token", user: { id: "synthetic-local", email: "local@example.test" } }
      : url.endsWith("/api/candidates") ? { ok: true, candidate: { id: "synthetic-candidate", ...input } }
      : { ok: true, matches: [] };
    return { ok: true, status: 200, json: async () => body };
  } });
  for (const file of ["shared.js", "avatar.js", "background.js"]) vm.runInContext(fs.readFileSync(path.join(OUT, "chrome", "src", file), "utf8"), context);
  const sender = { id: extension.runtime.id, url: extension.runtime.getURL("src/ui.html") };
  return { values, requests, opened, message: (message) => new Promise((resolve) => { if (listener(message, sender, resolve) === false) resolve(undefined); }) };
}

test("本地构建保留正式源码、协议和正式包全部字节，拒绝自定义地址参数", () => {
  assert.deepEqual(hashes(path.join(ROOT, "src")), sourceBefore);
  assert.deepEqual(hashes(path.join(ROOT, "dist")), productionBefore);
  assert.deepEqual(hashes(path.join(ROOT, "dist-local")), existingLocalBefore);
  assert.deepEqual(fs.readFileSync(path.join(ROOT, "INSTALL.md")), installBefore);
  assert.deepEqual(parseBuildOptions([]), { local: false }); assert.deepEqual(parseBuildOptions(["--local"]), { local: true });
  assert.deepEqual(parseBuildOptions(["--chrome"]), { local: false, chromeOnly: true });
  for (const argv of [["--local", "--url=http://example.test"], ["--api-url", "http://example.test"], ["--local=https://example.test"]]) assert.throws(() => parseBuildOptions(argv), /固定/);
  assert.throws(() => build({ local: "true" }), /布尔/); assert.throws(() => build({ local: true, api: "http://example.test" }), /布尔/);
});

test("本地清单仅移除生产权限，不扩大招聘权限，并使用独立Firefox账号存储", () => {
  for (const browser of ["chrome", "firefox"]) {
    const production = manifest(browser), local = JSON.parse(fs.readFileSync(path.join(OUT, browser, "manifest.json")));
    assert.deepEqual(local, manifest(browser, true)); assert.equal(local.version, production.version);
    assert.match(local.name, /本地测试/); assert.deepEqual(local.permissions, production.permissions);
    assert.deepEqual(local.host_permissions, production.host_permissions.filter((host) => host !== "https://goodhr.58it.cn/*"));
    assert.deepEqual(local.content_scripts, production.content_scripts);
    assert.equal(local.host_permissions.includes("http://127.0.0.1/*"), true);
  }
  assert.notEqual(manifest("firefox", true).browser_specific_settings.gecko.id, manifest("firefox").browser_specific_settings.gecko.id);
});

test("本地网页、协议、更新和配对入口固定5173，API固定8084且不残留生产地址", () => {
  for (const browser of ["chrome", "firefox"]) {
    const read = (file) => fs.readFileSync(path.join(OUT, browser, "src", file), "utf8");
    for (const file of ["shared.js", "ui.js", "candidate-ui.js", "background.js", "ui.html", "floating.html", "privacy.html"]) assert.equal(read(file).includes("https://goodhr.58it.cn"), false, file);
    for (const file of ["shared.js", "ui.js", "candidate-ui.js", "background.js"]) assert.doesNotThrow(() => new vm.Script(read(file)), file);
    assert.match(read("shared.js"), /CLOUD_BASE = "http:\/\/127\.0\.0\.1:8084"/);
    assert.match(read("candidate-ui.js"), /"http:\/\/127\.0\.0\.1:5173".*\/admin\/resumes\/detail/);
    assert.match(read("ui.js"), /"http:\/\/127\.0\.0\.1:5173" \+ "\/download#browser-extension"/);
    assert.match(read("ui.html"), /href="http:\/\/127\.0\.0\.1:5173\/login"/); assert.match(read("ui.html"), /value="43139"/);
    assert.match(read("ui.html"), /本地测试.*简历仅入测试库/); assert.match(read("privacy.html"), /不会请求生产 GoodHR/);
    assert.equal(read("background.js").includes("43129"), false);
    assert.match(fs.readFileSync(path.join(OUT, browser, "INSTALL.md"), "utf8"), /本地测试/);
  }
});

test("实际本地后台登录、查重、新增仅请求测试API，配对默认使用独立43139端口", async () => {
  const value = localFixture();
  const login = await value.message({ type: "LOGIN", payload: { email: "local@example.test", password: "synthetic-password", agreement_accepted: true } }); assert.equal(login.ok, true);
  const resume = { candidate_name: "合成本地候选人", phone: "13800000001", platform_id: "hliepin" };
  const duplicates = await value.message({ type: "DUPLICATES", payload: { resume } }); assert.equal(duplicates.ok, true);
  const created = await value.message({ type: "CREATE", payload: { resume, idempotency_key: "local-create" } }); assert.equal(created.ok, true);
  assert.equal(value.requests.filter(({ url }) => !url.endsWith("/api/system/browser-extension")).length, 3);
  assert.deepEqual(value.requests.map(({ url }) => new URL(url).origin), Array(value.requests.length).fill("http://127.0.0.1:8084"));
  const paired = await value.message({ type: "PAIR_START", payload: {} }); assert.equal(paired.ok, true);
  assert.equal(value.requests.at(-1).url, "http://127.0.0.1:43139/api/v1/extensions/pair/start");
  assert.equal(new URL(value.opened[0]).origin, "http://127.0.0.1:5173");
});

test("本地配对拒绝生产、测试API及其它网页授权地址，不会打开错误环境", async () => {
  for (const origin of ["https://goodhr.58it.cn", "http://127.0.0.1:8084", "http://localhost:5173", "https://example.test"]) {
    const value = localFixture(origin), result = await value.message({ type: "PAIR_START", payload: {} });
    assert.equal(result.ok, false); assert.match(result.message, /授权地址/); assert.equal(value.opened.length, 0);
    assert.equal(value.requests.length, 1); assert.equal(new URL(value.requests[0].url).origin, "http://127.0.0.1:43139");
  }
});
