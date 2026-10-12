/* 文件作用：验证 Firefox 正式包与 Chrome 共用业务文件、只保留必要清单差异，并能用 browser 命名空间运行后台和侧栏。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), os = require("node:os"), vm = require("node:vm"), crypto = require("node:crypto");
const { manifest, preparePackageDirectory } = require("../scripts/build.js");
const ROOT = path.resolve(__dirname, ".."), SRC = path.join(ROOT, "src");

/** filesUnder 按相对路径枚举目录文件，便于逐字节比较两种浏览器公共产物。 */
function filesUnder(directory, prefix = "") {
  return fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name)).flatMap((entry) => {
    const relative = path.posix.join(prefix, entry.name), filename = path.join(directory, entry.name);
    return entry.isDirectory() ? filesUnder(filename, relative) : [relative];
  });
}

/** firefoxBackgroundFixture 只提供 Firefox browser API 替身，验证后台不会偷偷依赖 chrome 全局。 */
function firefoxBackgroundFixture() {
  const values = {}, opened = []; let listener;
  const firefoxManifest = manifest("firefox"), extensionID = "synthetic-firefox-extension";
  const extension = {
    runtime: { id: extensionID, getURL: (file) => `moz-extension://${extensionID}/${file}`, getManifest: () => firefoxManifest, onMessage: { addListener: (handler) => { listener = handler; } } },
    storage: { local: { get: async (keys) => Object.fromEntries(keys.filter((key) => key in values).map((key) => [key, values[key]])), set: async (input) => Object.assign(values, input), remove: async (keys) => { for (const key of Array.isArray(keys) ? keys : [keys]) delete values[key]; }, setAccessLevel: async () => {} } },
    tabs: { query: async () => [{ id: 17, url: "https://h.liepin.com/resume/showresumedetail" }], sendMessage: async () => null, create: async () => ({}), onRemoved: { addListener: () => {} }, onUpdated: { addListener: () => {} } },
    action: { setBadgeText: () => {}, setBadgeBackgroundColor: () => {}, setTitle: () => {} },
    sidebarAction: { open: async () => { opened.push("sidebar"); } }
  };
  const context = vm.createContext({ browser: extension, URL, AbortSignal, crypto: crypto.webcrypto, Uint8Array, TextEncoder, console, setTimeout, clearTimeout, fetch: async () => ({ ok: true, status: 200, json: async () => ({ release: { version: firefoxManifest.version } }) }) });
  for (const file of ["shared.js", "avatar.js", "background.js"]) vm.runInContext(fs.readFileSync(path.join(SRC, file), "utf8"), context);
  const sender = { id: extensionID, url: extension.runtime.getURL("src/ui.html") };
  return { context, opened, message: (type) => new Promise((resolve) => { const accepted = listener({ type, payload: {} }, sender, resolve); if (accepted === false) resolve(undefined); }) };
}

test("Chrome 与 Firefox 正式目录的公共业务文件和品牌图标逐字节一致", () => {
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "goodhr-firefox-parity-")), chrome = path.join(temporary, "chrome"), firefox = path.join(temporary, "firefox");
  try {
    preparePackageDirectory(chrome, "chrome"); preparePackageDirectory(firefox, "firefox");
    const chromeFiles = filesUnder(chrome).filter((file) => !["manifest.json", "INSTALL.md"].includes(file));
    const firefoxFiles = filesUnder(firefox).filter((file) => !["manifest.json", "INSTALL.md"].includes(file));
    assert.deepEqual(firefoxFiles, chromeFiles);
    for (const file of chromeFiles) assert.deepEqual(fs.readFileSync(path.join(firefox, file)), fs.readFileSync(path.join(chrome, file)), file);
    const firefoxGuide = fs.readFileSync(path.join(firefox, "INSTALL.md"), "utf8");
    assert.match(firefoxGuide, /Mozilla 审核签名/);
    assert.match(firefoxGuide, /无需解压/);
    assert.doesNotMatch(firefoxGuide, /about:debugging|临时载入附加组件/);
    assert.deepEqual(fs.readFileSync(path.join(chrome, "INSTALL.md")), fs.readFileSync(path.join(ROOT, "INSTALL.md")));
  } finally { fs.rmSync(temporary, { recursive: true, force: true }); }
});

test("Firefox 清单只保留浏览器必需差异并声明实际读取的数据类别", () => {
  const chrome = manifest("chrome"), firefox = manifest("firefox"), chromeCommon = { ...chrome }, firefoxCommon = { ...firefox };
  for (const key of ["minimum_chrome_version", "background", "side_panel", "browser_specific_settings", "sidebar_action"]) { delete chromeCommon[key]; delete firefoxCommon[key]; }
  chromeCommon.permissions = chromeCommon.permissions.filter((permission) => permission !== "sidePanel");
  assert.deepEqual(firefoxCommon, chromeCommon);
  assert.deepEqual(firefox.background.scripts, ["src/shared.js", "src/avatar.js", "src/background.js"]);
  assert.equal(firefox.sidebar_action.default_panel, chrome.side_panel.default_path); assert.equal(firefox.sidebar_action.open_at_install, false);
  assert.deepEqual(firefox.browser_specific_settings.gecko.data_collection_permissions.required, ["authenticationInfo", "personallyIdentifyingInfo", "websiteContent", "personalCommunications", "locationInfo", "browsingActivity"]);
});

test("Firefox browser 命名空间可加载后台、返回 STATE 并打开原生侧栏", async () => {
  const fixture = firefoxBackgroundFixture();
  assert.equal(fixture.context.chrome, undefined);
  const state = await fixture.message("STATE"); assert.equal(state.ok, true); assert.equal(state.data.version, require("../package.json").version); assert.equal(state.data.logged_in, false);
  const opened = await fixture.message("OPEN_SIDEBAR"); assert.equal(opened.ok, true); assert.deepEqual(fixture.opened, ["sidebar"]);
});
