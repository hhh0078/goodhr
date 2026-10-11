/* 文件作用：用本机 Chrome 和本地合成页面验证自动悬浮、候选人切换时序、拖动及业务 DOM 隔离；不访问招聘网站。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), http = require("node:http");
const ROOT = path.resolve(__dirname, ".."), { chromium } = require(path.join(ROOT, "..", "cloud", "frontend-next", "node_modules", "@playwright", "test"));
const version = require("../package.json").version;
const ARTIFACTS = process.env.GOODHR_EXTENSION_ARTIFACT_DIR || path.join(ROOT, "artifacts");

/** localServer 只在随机 loopback 端口提供插件脚本与独立合成 iframe，不提供业务 API 或外部代理。 */
async function localServer(realFloating = false) {
  const html = '<!doctype html><style>body{margin:0;background:#f8f7f1;font:14px system-ui}button{background:red;color:white;font-size:30px}#business{padding:24px;height:1600px}</style><main id="business"><h1>合成候选人详情</h1><input id="business-input" value="不变的页面输入"><button id="business-action">招聘页面按钮</button><p>合成手机号13800000151 · 公司/学历仅作合成验证</p></main>';
  const iframe = '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>本地简历浮层测试</title><style>body{font:13px system-ui;margin:12px}label{display:block;margin-bottom:8px}input,textarea{width:90%}</style><h2>未入库</h2><label>姓名<input id="name"></label><label>手机号<input id="phone"></label><label>备注<textarea id="note"></textarea></label><script src="/fixture-frame.js"></script></html>';
  const frameScript = 'chrome.runtime.sendMessage({type:"STATE"}).then(result=>{document.getElementById("name").value=result.data.resume?.candidate_name||"";document.getElementById("phone").value=result.data.resume?.phone||"";document.body.dataset.ready="true";});';
  const server = http.createServer((request, response) => {
    const file = new URL(request.url, "http://127.0.0.1").pathname;
    if (file === "/fixture" || (!realFloating && file === "/src/floating.html") || file === "/fixture-frame.js") { response.writeHead(200, { "Content-Type": file.endsWith(".js") ? "application/javascript" : "text/html; charset=utf-8" }); response.end(file === "/fixture" ? html : file.endsWith(".js") ? frameScript : iframe); return; }
    const filename = /^\/src\/[a-z.-]+$/.test(file) ? path.join(ROOT, file.slice(1)) : "";
    if (!filename || !fs.existsSync(filename)) { response.writeHead(404); response.end(); return; }
    response.writeHead(200, { "Content-Type": file.endsWith(".html") ? "text/html; charset=utf-8" : file.endsWith(".css") ? "text/css" : "application/javascript" }); response.end(fs.readFileSync(filename));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve)); return { server, origin: `http://127.0.0.1:${server.address().port}` };
}

/** preparePage 为合成页面配置封闭网络和插件运行时，生产代码不暴露 Shadow DOM 或调试函数。 */
async function preparePage(browser, origin, handler) {
  const page = await browser.newPage({ viewport: { width: 1150, height: 900 } }), network = [];
  await page.route("**/*", (route) => { const url = route.request().url(); network.push(url); return url.startsWith(origin + "/") ? route.continue() : route.abort(); });
  await page.exposeFunction("fixtureMessage", handler);
  await page.addInitScript(({ origin, version }) => {
    window.chrome = { runtime: { id: "fixture-extension", getURL: (file) => origin + "/" + file, getManifest: () => ({ version }), sendMessage: (message) => window.fixtureMessage(message), onMessage: { addListener: (listener) => { window.fixtureCaptureListener = listener; } } } };
    // 只在合成测试页捕获本插件自己的 closed Shadow；生产脚本没有这一入口。
    const attachShadow = Element.prototype.attachShadow; Element.prototype.attachShadow = function (options) { const shadow = attachShadow.call(this, options); if (this.id === "goodhr-resume-assistant") window.fixtureShadow = shadow; return shadow; };
  }, { origin, version });
  await page.goto(origin + "/fixture"); return { page, network };
}

/** point 在测试捕获的 Shadow 中读取控件位置，点击和拖动均使用真实鼠标。 */
async function point(page, selector) { return page.evaluate((selector) => { const rect = window.fixtureShadow.querySelector(selector).getBoundingClientRect(); return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 }; }, selector); }

/** clickOwnButton 用真实鼠标点击插件自身控件，不分发页面自定义事件。 */
async function clickOwnButton(page, selector) { const target = await point(page, selector); await page.mouse.click(target.x, target.y); }

/** sampleResume 提供同名不同平台编号的合成简历，用于验证候选人身份隔离。 */
function sampleResume(id = "candidate-a", phone = "13800000151") { return { candidate_name: "合成悬浮候选人", phone, platform_id: "hliepin", platform_candidate_id: id, source_url: "https://h.liepin.com/resume/show", work_experiences: [{ company_name: "合成悬浮公司", position_name: "合成岗位", start_ym: "2021-01", content: "仅用于本地测试" }] }; }

/** floatingFrame 查找宿主创建的独立简历页，不等待已经发生过的导航事件。 */
async function floatingFrame(page) { await page.waitForFunction(() => window.fixtureShadow?.querySelector("iframe")?.contentDocument?.body?.dataset.ready === "true"); return page.frames().find((item) => item.url().includes("/src/floating.html")); }

test("Chrome合成页面：识别即自动展开，同人收起保持，换人重开且不影响业务页面", async () => {
  const { server, origin } = await localServer(), browser = await chromium.launch({ channel: "chrome", headless: true }), calls = [], resume = sampleResume();
  try {
    const { page, network } = await preparePage(browser, origin, async ({ type }) => { calls.push(type); return { ok: true, data: type === "STATE" ? { logged_in: true, account_key: "synthetic", resume, version } : {} }; });
    const before = await page.locator("#business").innerHTML(); await page.locator("#business-input").focus();
    await page.addScriptTag({ path: path.join(ROOT, "src", "shared.js") }); await page.addScriptTag({ path: path.join(ROOT, "src", "floating.js") });
    await page.evaluate((resume) => GoodHRFloating.update(resume), resume); const frame = await floatingFrame(page);
    assert.equal(await frame.locator("#name").inputValue(), resume.candidate_name); assert.equal(await frame.locator("#phone").inputValue(), resume.phone);
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), false);
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".open").hidden), true);
    assert.equal(await page.evaluate(() => document.getElementById("goodhr-resume-assistant").shadowRoot), null);
    assert.equal(await page.evaluate(() => document.activeElement.id), "business-input"); assert.equal(await page.locator("#business").innerHTML(), before);
    assert.equal(await page.evaluate(() => window.fixtureShadow.textContent.includes("打开助手") || window.fixtureShadow.textContent.includes("资料不会自动入库")), false);
    await frame.locator("#note").fill("关闭后保留的合成草稿"); await clickOwnButton(page, ".close");
    await page.evaluate((resume) => { GoodHRFloating.update(resume); GoodHRFloating.update({ ...resume, online_status: "刚刚活跃" }); }, resume);
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), true);
    await clickOwnButton(page, ".open"); assert.equal(await frame.locator("#note").inputValue(), "关闭后保留的合成草稿");
    await page.evaluate(() => { window.postMessage({ type: "CREATE", payload: { candidate_name: "伪造" } }, "*"); window.fixtureShadow.querySelector("iframe").contentWindow.postMessage({ type: "LOGIN", payload: { email: "evil@example.test" } }, "*"); });
    assert.equal(calls.some((type) => ["LOGIN", "CREATE", "UPDATE", "DUPLICATES", "FOLLOWUPS"].includes(type)), false);
    const bar = await point(page, ".bar"); await page.mouse.move(bar.x, bar.y); await page.mouse.down(); await page.mouse.move(-70, -70, { steps: 8 }); await page.mouse.up();
    const rectangle = await page.locator("#goodhr-resume-assistant").boundingBox(); assert.ok(rectangle.x >= 7 && rectangle.y >= 7); assert.ok(rectangle.x + rectangle.width <= 1150 && rectangle.y + rectangle.height <= 900);
    await clickOwnButton(page, ".close"); await page.evaluate((resume) => GoodHRFloating.update(resume), sampleResume("candidate-b", "13800000152"));
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), false); assert.equal(await page.locator("#goodhr-resume-assistant").count(), 1);
    assert.equal(await page.locator("#business").innerHTML(), before); assert.equal(await page.evaluate(() => window.scrollY), 0); assert.equal(network.some((url) => !url.startsWith(origin + "/")), false);
    fs.mkdirSync(ARTIFACTS, { recursive: true }); await page.screenshot({ path: path.join(ARTIFACTS, `plugin-floating-host-${version}.png`), caret: "initial" });
    await page.evaluate(() => GoodHRFloating.update(null)); assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), false);
    await page.evaluate((resume) => GoodHRFloating.update(resume), sampleResume("candidate-b", "13800000152")); assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), false);
  } finally { await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});

test("Chrome合成页面：先同步快照再自动读到当前人，迟到捕获不能重新显示上一人", async () => {
  const { server, origin } = await localServer(), browser = await chromium.launch({ channel: "chrome", headless: true }), calls = [], updates = [];
  let page, snapshot = null, releaseOld;
  const oldResponse = new Promise((resolve) => { releaseOld = resolve; });
  try {
    ({ page } = await preparePage(browser, origin, async (message) => {
      calls.push(message.type);
      if (message.type === "CANDIDATE_SNAPSHOT") { snapshot = message.resume; if (message.resume?.platform_candidate_id === "candidate-a") await oldResponse; return { ok: true }; }
      if (message.type === "STATE") {
        // 模拟后台 currentSnapshot 的 REQUEST_CAPTURE 回读，确保面板初始化不会递归等待自己。
        await page.evaluate(() => new Promise((resolve) => window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, resolve)));
        return { ok: true, data: { logged_in: true, account_key: "synthetic", resume: snapshot, version } };
      }
      return { ok: true, data: {} };
    }));
    await page.exposeFunction("fixtureUpdate", (resume) => updates.push(resume?.platform_candidate_id || ""));
    await page.addScriptTag({ path: path.join(ROOT, "src", "shared.js") }); await page.addScriptTag({ path: path.join(ROOT, "src", "floating.js") });
    await page.evaluate((resume) => { window.fixtureCandidate = resume; window.GoodHRExtraction = { extractCurrentCandidate: () => window.fixtureCandidate }; const floating = GoodHRFloating; window.GoodHRFloating = { update: (value) => { floating.update(value); window.fixtureUpdate(value); } }; }, sampleResume());
    await page.addScriptTag({ path: path.join(ROOT, "src", "content.js") }); assert.equal(await page.locator("#goodhr-resume-assistant").count(), 0);
    await page.evaluate((resume) => { window.fixtureCandidate = resume; window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, () => {}); }, sampleResume("candidate-b", "13800000152"));
    const frame = await floatingFrame(page); assert.equal(await frame.locator("#phone").inputValue(), "13800000152");
    await clickOwnButton(page, ".close"); releaseOld();
    await page.evaluate(() => new Promise((resolve) => window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, resolve)));
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), true); assert.equal(updates.includes("candidate-a"), false);
    const count = calls.filter((type) => type === "CANDIDATE_SNAPSHOT").length;
    assert.equal(await page.evaluate(() => window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "another-extension" }, () => {})), false);
    assert.equal(calls.filter((type) => type === "CANDIDATE_SNAPSHOT").length, count);
    await page.evaluate(() => { window.fixtureCandidate = null; window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, () => {}); });
    await page.waitForFunction(() => document.getElementById("goodhr-resume-assistant").hidden);
  } finally { releaseOld(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});

test("Chrome合成页面：补齐手机号不打断同人收起状态，小窗口拖动只影响插件", async () => {
  const { server, origin } = await localServer(), browser = await chromium.launch({ channel: "chrome", headless: true });
  try {
    const resume = sampleResume("candidate-a", ""), { page } = await preparePage(browser, origin, async () => ({ ok: true, data: { resume, version } }));
    await page.addScriptTag({ path: path.join(ROOT, "src", "shared.js") }); await page.addScriptTag({ path: path.join(ROOT, "src", "floating.js") });
    await page.evaluate((resume) => GoodHRFloating.update(resume), resume); await clickOwnButton(page, ".close");
    await page.evaluate((resume) => GoodHRFloating.update(resume), sampleResume()); assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), true);
    await clickOwnButton(page, ".open"); await page.setViewportSize({ width: 360, height: 420 });
    const bar = await point(page, ".bar"); await page.mouse.move(bar.x, bar.y); await page.mouse.down(); await page.mouse.move(600, 600, { steps: 8 }); await page.mouse.up();
    const rectangle = await page.locator("#goodhr-resume-assistant").boundingBox(); assert.ok(rectangle.x >= 7 && rectangle.y >= 7); assert.ok(rectangle.x + rectangle.width <= 360 && rectangle.y + rectangle.height <= 420);
    assert.equal(await page.evaluate(() => window.scrollY), 0);
  } finally { await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});

test("Chrome合成页面：页面换人仍处于防抖等待时，快照响应也不能展开旧人", async () => {
  const { server, origin } = await localServer(), browser = await chromium.launch({ channel: "chrome", headless: true }), updates = [];
  let snapshot = null, releaseOld;
  const oldResponse = new Promise((resolve) => { releaseOld = resolve; });
  try {
    const { page } = await preparePage(browser, origin, async (message) => {
      if (message.type === "CANDIDATE_SNAPSHOT") { snapshot = message.resume; if (message.resume?.platform_candidate_id === "candidate-a") await oldResponse; return { ok: true }; }
      return { ok: true, data: { resume: snapshot, version } };
    });
    await page.exposeFunction("fixtureUpdate", (resume) => updates.push(resume?.platform_candidate_id || ""));
    await page.addScriptTag({ path: path.join(ROOT, "src", "shared.js") }); await page.addScriptTag({ path: path.join(ROOT, "src", "floating.js") });
    await page.evaluate((resume) => { window.fixtureCandidate = resume; window.GoodHRExtraction = { extractCurrentCandidate: () => window.fixtureCandidate }; const floating = GoodHRFloating; window.GoodHRFloating = { update: (value) => { floating.update(value); window.fixtureUpdate(value); } }; }, sampleResume());
    await page.addScriptTag({ path: path.join(ROOT, "src", "content.js") });
    // 不触发新的捕获，模拟页面刚切换而 MutationObserver 的防抖尚未执行。
    await page.evaluate((resume) => { window.fixtureCandidate = resume; }, sampleResume("candidate-b", "13800000152")); releaseOld();
    const frame = await floatingFrame(page); assert.equal(await frame.locator("#phone").inputValue(), "13800000152"); assert.equal(updates.includes("candidate-a"), false);
  } finally { releaseOld(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});

test("Chrome真实简历浮层：初次自动读当前人，缺手机可填写，草稿不串人且一次点击直接添加", async () => {
  const { server, origin } = await localServer(true), browser = await chromium.launch({ channel: "chrome", headless: true }), drafts = new Map(), writes = [], dialogs = [], errors = [];
  let page, snapshot = null;
  try {
    ({ page } = await preparePage(browser, origin, async ({ type, payload = {}, resume }) => {
      if (type === "CANDIDATE_SNAPSHOT") { snapshot = resume ? { ...resume, capture_id: `synthetic-${resume.platform_candidate_id}` } : null; return { ok: true }; }
      if (type === "STATE") {
        await page.evaluate(() => new Promise((resolve) => window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, resolve)));
        return { ok: true, data: { logged_in: true, data_use_accepted: true, account_key: "synthetic-only", resume: snapshot, version } };
      }
      if (type === "DRAFT_GET") return { ok: true, data: { draft: drafts.get(payload.identity) || null } };
      if (type === "DRAFT_SAVE") { drafts.set(payload.identity, payload.draft); return { ok: true, data: {} }; }
      if (type === "CHECK_UPDATE") return { ok: true, data: { current_version: version, latest_version: version, update_required: false } };
      if (type === "POSITIONS") return { ok: true, data: { items: [{ id: "synthetic-position", name: "合成工程师", company_name: "合成公司" }] } };
      if (type === "DUPLICATES") return { ok: true, data: { matches: [], groups: [] } };
      if (type === "CREATE") { writes.push({ type, payload }); return { ok: true, data: { candidate: { id: "synthetic-created", ...payload.resume, row_version: 1 } } }; }
      if (type === "FOLLOWUPS") { writes.push({ type, payload }); return { ok: true, data: { notes_saved: true, recommendation_saved: true, errors: [] } }; }
      if (type === "DETAIL") return { ok: true, data: { notes: [], recommendations: [] } };
      return { ok: true, data: {} };
    }));
    page.on("dialog", (dialog) => { dialogs.push(dialog.message()); dialog.dismiss(); }); page.on("pageerror", (error) => errors.push(error.message));
    const before = await page.locator("#business").innerHTML(); await page.locator("#business-input").focus();
    await page.addScriptTag({ path: path.join(ROOT, "src", "shared.js") }); await page.addScriptTag({ path: path.join(ROOT, "src", "floating.js") });
    await page.evaluate((resume) => { window.fixtureCandidate = resume; window.GoodHRExtraction = { extractCurrentCandidate: () => window.fixtureCandidate }; }, sampleResume("candidate-a", ""));
    await page.addScriptTag({ path: path.join(ROOT, "src", "content.js") });
    await page.waitForFunction(() => Boolean(window.fixtureShadow?.querySelector("iframe")?.contentDocument?.getElementById("name")?.value));
    const frame = page.frames().find((item) => item.url().includes("/src/floating.html"));
    assert.equal(await page.evaluate(() => document.activeElement.id), "business-input"); assert.equal(await page.locator("#business").innerHTML(), before);
    await frame.locator("#duplicate-status").filter({ hasText: "未入库，请补填手机号" }).waitFor();
    assert.equal(await frame.locator("#phone").inputValue(), ""); assert.equal(await frame.locator("#save").isDisabled(), true);
    assert.equal(await frame.locator("#login-form, #pair-start, #logout, #data-use").count(), 0);
    await frame.locator("#phone").fill("13800000151"); await frame.locator("#note").fill("甲候选人的合成草稿");
    await frame.waitForFunction(() => !document.getElementById("save").disabled);
    await frame.waitForFunction(() => document.getElementById("duplicate-status").textContent === "未入库");
    await page.waitForFunction(() => window.fixtureShadow.querySelector("iframe").contentDocument.getElementById("note").value === "甲候选人的合成草稿");
    await page.evaluate((resume) => { window.fixtureCandidate = resume; window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, () => {}); }, sampleResume("candidate-b", "13800000152"));
    await frame.waitForFunction(() => document.getElementById("phone").value === "13800000152"); assert.equal(await frame.locator("#note").inputValue(), "");
    await page.evaluate((resume) => { window.fixtureCandidate = resume; window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, () => {}); }, sampleResume("candidate-a", ""));
    await frame.waitForFunction(() => document.getElementById("phone").value === "13800000151" && document.getElementById("note").value === "甲候选人的合成草稿" && !document.getElementById("save").disabled);
    fs.mkdirSync(ARTIFACTS, { recursive: true }); await page.screenshot({ path: path.join(ARTIFACTS, `plugin-floating-actual-${version}.png`), caret: "initial" });
    await frame.locator("#save").click(); await frame.waitForFunction(() => document.getElementById("duplicate-status").textContent === "已入库");
    assert.equal(writes.filter((item) => item.type === "CREATE").length, 1); assert.equal(writes.find((item) => item.type === "CREATE").payload.resume.phone, "13800000151");
    await frame.waitForFunction(() => document.getElementById("note").value === "");
    assert.equal(writes.filter((item) => item.type === "FOLLOWUPS").length, 1); assert.equal(writes.find((item) => item.type === "FOLLOWUPS").payload.note, "甲候选人的合成草稿");
    assert.deepEqual(dialogs, []); assert.deepEqual(errors, []); assert.equal(await page.locator("#business").innerHTML(), before);
  } finally { await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});
