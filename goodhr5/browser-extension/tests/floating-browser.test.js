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
    window.chrome = { runtime: { id: "fixture-extension", getURL: (file) => origin + "/" + file, getManifest: () => ({ version }), sendMessage: async (message) => { const result = await window.fixtureMessage(message); if (message.type === "CLOSE_FLOATING" && result?.ok && window.top.fixtureCaptureListener) window.top.fixtureCaptureListener({ type: "CLOSE_FLOATING", identity: message.payload?.identity || "" }, { id: "fixture-extension" }, () => {}); return result; }, onMessage: { addListener: (listener) => { window.fixtureCaptureListener = listener; } } } };
    // 只在合成测试页捕获本插件自己的 closed Shadow；生产脚本没有这一入口。
    const attachShadow = Element.prototype.attachShadow; Element.prototype.attachShadow = function (options) { const shadow = attachShadow.call(this, options); if (this.id === "goodhr-resume-assistant") window.fixtureShadow = shadow; return shadow; };
  }, { origin, version });
  await page.goto(origin + "/fixture"); return { page, network };
}

/** point 在测试捕获的 Shadow 中读取控件位置，点击和拖动均使用真实鼠标。 */
async function point(page, selector) { return page.evaluate((selector) => { const rect = window.fixtureShadow.querySelector(selector).getBoundingClientRect(); return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 }; }, selector); }

/** clickOwnButton 用真实鼠标点击插件自身控件，不分发页面自定义事件。 */
async function clickOwnButton(page, selector) { const target = await point(page, selector); await page.mouse.click(target.x, target.y); }

/** waitForFixture 限时等待合成接口收到请求，流程中断时明确失败而不是永久卡住测试。 */
async function waitForFixture(page, predicate, message) { const deadline = Date.now() + 12000; while (!predicate() && Date.now() < deadline) await page.waitForTimeout(20); assert.ok(predicate(), message); }

/** sampleResume 提供同名不同平台编号的合成简历，用于验证候选人身份隔离。 */
function sampleResume(id = "candidate-a", phone = "13800000151") { return { candidate_name: "合成悬浮候选人", phone, platform_id: "hliepin", platform_candidate_id: id, source_url: "https://h.liepin.com/resume/show", work_experiences: [{ company_name: "合成悬浮公司", position_name: "合成岗位", start_ym: "2021-01", content: "仅用于本地测试" }] }; }

test("Chrome真实浮窗BOSS：无需勾选便自动基本识别，保存才完整解析，两个阶段都保留手改并复用原保存", async () => {
  const { server, origin } = await localServer(true), browser = await chromium.launch({ channel: "chrome", headless: true }), calls = [], drafts = new Map(), errors = [];
  const hash = "a".repeat(64), snapshot = { platform_id: "boss", source_url: "https://www.zhipin.com/web/chat/index", capture_id: "boss-" + hash, attachment: { text_hash: hash, page_count: 1, total_pages: 0, complete: false, blocked: false, warning: "已读到1页，总页数请核对" } };
  let releaseBasic, releaseFull;
  try {
    const { page } = await preparePage(browser, origin, async ({ type, payload = {} }) => {
      calls.push({ type, payload });
      if (type === "STATE") return { ok: true, data: { logged_in: true, data_use_accepted: true, auto_close_after_save: false, account_key: "boss-fixture", resume: snapshot, version } };
      if (type === "DRAFT_GET") return { ok: true, data: { draft: drafts.get(payload.identity) || null } };
      if (type === "DRAFT_SAVE") { drafts.set(payload.identity, payload.draft); return { ok: true, data: {} }; }
      if (type === "ATTACHMENT_START") {
        const task = { id: "task-" + payload.phase, phase: payload.phase, capture_id: snapshot.capture_id, text_hash: hash, request_key: payload.request_key };
        if (payload.phase === "basic") { await new Promise((resolve) => { releaseBasic = resolve; }); return { ok: true, data: { task: { ...task, status: "completed", resume: { candidate_name: "AI识别姓名", phone: "13800000991", gender: "女", work_experiences: [{ company_name: "最早合成公司", start_ym: "2017-01", end_ym: "2019-01" }], educations: [{ school_name: "最早合成学校", start_ym: "2010-09", end_ym: "2014-06" }] } } } }; }
        return { ok: true, data: { task: { ...task, status: "running" } } };
      }
      if (type === "ATTACHMENT_POLL") { await new Promise((resolve) => { releaseFull = resolve; }); return { ok: true, data: { task: { id: "task-full", phase: "full", status: "completed", capture_id: snapshot.capture_id, text_hash: hash, resume: { candidate_name: "完整AI旧姓名", phone: "13800000999", gender: "male", email: "full-synthetic@example.test", ai_summary: "合成忠实摘要", work_experiences: [{ company_name: "最新合成公司", start_ym: "2022-01", content: "最新完整内容" }, { company_name: "最早合成公司", start_ym: "2017-01", end_ym: "2019-01", content: "最早完整内容" }], educations: [{ school_name: "最新合成学校", start_ym: "2014-09" }, { school_name: "最早合成学校", start_ym: "2010-09", end_ym: "2014-06" }] } } } }; }
      if (type === "DUPLICATES") return { ok: true, data: { matches: [], groups: [] } };
      if (type === "CREATE") return { ok: true, data: { candidate: { id: "created-boss-fixture", ...payload.resume, row_version: 1 } } };
      if (type === "FOLLOWUPS") return { ok: true, data: { notes_saved: true, recommendation_saved: true, errors: [] } };
      if (type === "DETAIL") return { ok: true, data: { notes: [], recommendations: [] } };
      return { ok: true, data: {} };
    });
    page.on("pageerror", (error) => errors.push(error.message));
    await page.addScriptTag({ path: path.join(ROOT, "src", "shared.js") }); await page.addScriptTag({ path: path.join(ROOT, "src", "floating.js") });
    await page.evaluate((resume) => GoodHRFloating.update(resume), snapshot);
    await page.waitForFunction(() => window.fixtureShadow?.querySelector("iframe")?.contentDocument?.getElementById("attachment-panel"));
    const frame = page.frames().find((item) => item.url().includes("/src/floating.html"));
    await frame.locator("#attachment-panel").waitFor({ state: "visible" }); await page.waitForTimeout(550);
    assert.equal(calls.filter((item) => item.type === "ATTACHMENT_START").length, 0); assert.equal(await frame.locator("#save").isDisabled(), true);
    assert.equal(await frame.locator("input[type=checkbox]").count(), 0);
    await waitForFixture(page, () => Boolean(releaseBasic), "附件稳定后没有自动发起基础识别");
    await frame.locator("#name").fill("基本识别前手改姓名"); releaseBasic();
    await frame.waitForFunction(() => document.getElementById("phone").value === "13800000991");
    assert.equal(await frame.locator("#name").inputValue(), "基本识别前手改姓名"); assert.equal(await frame.locator("#gender").inputValue(), "female");
    assert.equal(await frame.locator("#first-company").inputValue(), "最早合成公司"); assert.equal(await frame.locator("#first-school").inputValue(), "最早合成学校");
    assert.equal(calls.some((item) => item.type === "ATTACHMENT_START" && item.payload.phase === "full"), false); assert.equal(calls.some((item) => item.type === "CREATE"), false);
    fs.mkdirSync(ARTIFACTS, { recursive: true }); await page.locator("#goodhr-resume-assistant").screenshot({ path: path.join(ARTIFACTS, `plugin-boss-attachment-synthetic-${version}.png`), caret: "initial" });
    await frame.locator("#attachment-review>summary").click(); await frame.locator("#first-company").fill("人工核对首份公司"); await frame.locator("#first-work-end").fill("");
    await frame.waitForFunction(() => !document.getElementById("save").disabled); await frame.locator("#save").click();
    await waitForFixture(page, () => Boolean(releaseFull), "点击添加后没有发起完整解析");
    await frame.locator("#name").fill("完整识别期间手改姓名"); releaseFull();
    await frame.waitForFunction(() => document.getElementById("status").textContent === "已添加");
    const saved = calls.find((item) => item.type === "CREATE").payload.resume;
    assert.equal(saved.candidate_name, "完整识别期间手改姓名"); assert.equal(saved.phone, "13800000991"); assert.equal(saved.gender, "female"); assert.equal(saved.email, "full-synthetic@example.test"); assert.equal(saved.ai_summary, "合成忠实摘要");
    assert.equal(saved.work_experiences.length, 2); assert.equal(saved.work_experiences[0].content, "最新完整内容"); assert.equal(saved.work_experiences[1].company_name, "人工核对首份公司"); assert.equal(saved.work_experiences[1].end_ym, ""); assert.equal(saved.work_experiences[1].content, "最早完整内容");
    assert.equal(saved.educations.length, 2); assert.equal(saved.educations[1].school_name, "最早合成学校");
    assert.equal(calls.filter((item) => item.type === "ATTACHMENT_START" && item.payload.phase === "basic").length, 1); assert.equal(calls.filter((item) => item.type === "ATTACHMENT_START" && item.payload.phase === "full").length, 1);
    assert.equal(calls.filter((item) => ["ATTACHMENT_START", "ATTACHMENT_POLL", "CREATE"].includes(item.type)).some((item) => Object.hasOwn(item.payload, "text") || Object.hasOwn(item.payload, "html")), false);
    assert.deepEqual(errors, []);
  } finally { releaseBasic?.(); releaseFull?.(); await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});

test("Chrome真实浮窗BOSS：当前 AI 未配置只提示，轮询状态不重复发起收费请求", async () => {
  const { server, origin } = await localServer(true), browser = await chromium.launch({ channel: "chrome", headless: true }), calls = [];
  const hash = "b".repeat(64), snapshot = { platform_id: "boss", source_url: "https://www.zhipin.com/web/chat/index", capture_id: "boss-" + hash, attachment: { text_hash: hash, page_count: 1, total_pages: 0, complete: false, blocked: false } };
  try {
    const { page } = await preparePage(browser, origin, async ({ type, payload = {} }) => { calls.push({ type, payload }); if (type === "STATE") return { ok: true, data: { logged_in: true, data_use_accepted: true, account_key: "no-ai-fixture", resume: snapshot, version } }; if (type === "ATTACHMENT_START") return { ok: false, status: 422, code: "AI_NOT_CONFIGURED", message: "当前账号没有配置 AI" }; return { ok: true, data: {} }; });
    await page.goto(origin + "/src/floating.html"); await page.locator("#attachment-panel").waitFor({ state: "visible" });
    await page.waitForFunction(() => document.getElementById("attachment-status").textContent.includes("还没配置 AI")); await page.waitForTimeout(2800);
    assert.equal(calls.filter((item) => item.type === "ATTACHMENT_START").length, 1); assert.equal(calls.some((item) => item.type === "CREATE"), false); assert.equal(await page.locator("#save").isDisabled(), true); assert.equal(await page.locator("#attachment-start").isDisabled(), false);
  } finally { await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});

test("Chrome合成BOSS附件：同源可见框架只读呈现叶子文字，多页缺失、隐藏框架和多附件均受保护", async () => {
  const { server, origin } = await localServer(), browser = await chromium.launch({ channel: "chrome", headless: true });
  try {
    const { page, network } = await preparePage(browser, origin, async () => ({ ok: true }));
    await page.evaluate(() => {
      const fixture = '<style>.page{position:relative;width:747px;height:1056px}.textLayer{position:absolute;inset:0}.textLayer span[role=presentation]{position:absolute;font:14px sans-serif}</style><div class="bzl-office-pdf" data-page-count="2"><div class="page" data-page-number="1"><div class="textLayer"><span><span><span role="presentation" style="left:70px;top:60px">合成学校</span></span></span><span role="presentation" style="left:70px;top:20px">合成姓名 13800000991</span><span role="presentation" style="left:70px;top:90px;display:none">隐藏资料不能读取</span></div></div><div class="page" data-page-number="2"></div></div>';
      const frame = document.createElement("iframe"); frame.id = "pdf-fixture"; frame.style.cssText = "width:800px;height:500px"; frame.srcdoc = fixture; document.getElementById("business").append(frame);
      const hidden = document.createElement("iframe"); hidden.hidden = true; hidden.srcdoc = fixture.replaceAll("合成姓名", "隐藏框架"); document.getElementById("business").append(hidden);
      const isolated = document.createElement("iframe"); isolated.sandbox = "allow-scripts"; isolated.srcdoc = fixture.replaceAll("合成姓名", "隔离框架"); document.getElementById("business").append(isolated);
    });
    await page.waitForFunction(() => document.getElementById("pdf-fixture")?.contentDocument?.querySelector(".textLayer"));
    const before = await page.locator("#business").innerHTML();
    await page.addScriptTag({ path: path.join(ROOT, "src", "boss-attachment.js") });
    const first = await page.evaluate(() => GoodHRBossAttachment.extract(document, "https://www.zhipin.com/web/chat/index"));
    assert.equal(first.attachment.text, "合成姓名 13800000991\n合成学校"); assert.equal(first.attachment.page_count, 1); assert.equal(first.attachment.total_pages, 2); assert.equal(first.attachment.blocked, true); assert.equal(first.attachment.complete, false); assert.match(first.attachment.warning, /1\/2/);
    assert.equal(await page.locator("#business").innerHTML(), before, "提取不得修改网站 DOM");
    await page.evaluate(() => { const frameDoc = document.getElementById("pdf-fixture").contentDocument, layer = frameDoc.createElement("div"), span = frameDoc.createElement("span"); layer.className = "textLayer"; span.setAttribute("role", "presentation"); span.style.cssText = "left:70px;top:20px"; span.textContent = "第二页合成工作经历"; layer.append(span); frameDoc.querySelector('[data-page-number="2"]').append(layer); });
    const full = await page.evaluate(() => GoodHRBossAttachment.extract(document, "https://www.zhipin.com/web/chat/index")); assert.equal(full.attachment.complete, true); assert.equal(full.attachment.blocked, false); assert.ok(full.attachment.text.endsWith("第二页合成工作经历"));
    await page.evaluate(() => { const frame = document.createElement("iframe"); frame.srcdoc = document.getElementById("pdf-fixture").srcdoc; document.getElementById("business").append(frame); });
    await page.waitForFunction(() => [...document.querySelectorAll("#business>iframe")].filter((frame) => !frame.hidden && frame.contentDocument?.querySelector(".bzl-office-pdf")).length === 2);
    const ambiguous = await page.evaluate(() => GoodHRBossAttachment.extract(document, "https://www.zhipin.com/web/chat/index")); assert.equal(ambiguous.attachment.text, ""); assert.equal(ambiguous.attachment.blocked, true);
    assert.equal(network.some((url) => !url.startsWith(origin + "/")), false);
  } finally { await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});

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
    assert.deepEqual(await page.evaluate(() => { const panel = window.fixtureShadow.querySelector(".panel"), iframe = window.fixtureShadow.querySelector("iframe"); return { panelWidth: panel.getBoundingClientRect().width, frameHeight: iframe.getBoundingClientRect().height }; }), { panelWidth: 248, frameHeight: 460 });
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".open")), null);
    assert.equal(await page.evaluate(() => document.getElementById("goodhr-resume-assistant").shadowRoot), null);
    assert.equal(await page.evaluate(() => document.activeElement.id), "business-input"); assert.equal(await page.locator("#business").innerHTML(), before);
    assert.equal(await page.evaluate(() => window.fixtureShadow.textContent.includes("打开助手") || window.fixtureShadow.textContent.includes("资料不会自动入库")), false);
    assert.equal(await page.evaluate(() => GoodHRFloating.close("not-current")), false); assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), false);
    await frame.locator("#note").fill("关闭后保留的合成草稿"); await clickOwnButton(page, ".close");
    await page.evaluate((resume) => { GoodHRFloating.update(resume); GoodHRFloating.update({ ...resume, online_status: "刚刚活跃" }); }, resume);
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), true);
    assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), false); assert.equal(await frame.locator("#note").inputValue(), "关闭后保留的合成草稿");
    await page.evaluate((resume) => GoodHRFloating.update(resume), sampleResume("candidate-b", "13800000152"));
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), false);
    await page.evaluate(() => { window.postMessage({ type: "CREATE", payload: { candidate_name: "伪造" } }, "*"); window.fixtureShadow.querySelector("iframe").contentWindow.postMessage({ type: "LOGIN", payload: { email: "evil@example.test" } }, "*"); });
    assert.equal(calls.some((type) => ["LOGIN", "CREATE", "UPDATE", "DUPLICATES", "FOLLOWUPS"].includes(type)), false);
    const bar = await point(page, ".bar"); await page.mouse.move(bar.x, bar.y); await page.mouse.down(); await page.mouse.move(-70, -70, { steps: 8 }); await page.mouse.up();
    const rectangle = await page.locator("#goodhr-resume-assistant").boundingBox(); assert.ok(rectangle.x >= 7 && rectangle.y >= 7); assert.ok(rectangle.x + rectangle.width <= 1150 && rectangle.y + rectangle.height <= 900);
    await clickOwnButton(page, ".close"); await page.evaluate((resume) => GoodHRFloating.update(resume), sampleResume("candidate-c", "13800000153"));
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), false); assert.equal(await page.locator("#goodhr-resume-assistant").count(), 1);
    assert.equal(await page.locator("#business").innerHTML(), before); assert.equal(await page.evaluate(() => window.scrollY), 0); assert.equal(network.some((url) => !url.startsWith(origin + "/")), false);
    fs.mkdirSync(ARTIFACTS, { recursive: true }); await page.screenshot({ path: path.join(ARTIFACTS, `plugin-floating-host-${version}.png`), caret: "initial" });
    await page.evaluate(() => GoodHRFloating.update(null)); assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), false);
    await page.evaluate((resume) => GoodHRFloating.update(resume), sampleResume("candidate-c", "13800000153")); assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), false);
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
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".open")), null);
    await page.evaluate((resume) => GoodHRFloating.update(resume), sampleResume("candidate-b", "13800000152")); await page.setViewportSize({ width: 360, height: 420 });
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
        return { ok: true, data: { logged_in: true, data_use_accepted: true, auto_close_after_save: true, account_key: "synthetic-only", resume: snapshot, version } };
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
    await frame.waitForFunction(() => document.getElementById("status").textContent === "已添加"); await page.waitForTimeout(300); assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), true);
    await page.waitForFunction(() => document.getElementById("goodhr-resume-assistant").hidden); assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), false);
    assert.deepEqual(dialogs, []); assert.deepEqual(errors, []); assert.equal(await page.locator("#business").innerHTML(), before);
  } finally { await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});

test("Chrome真实简历浮层：仅完整CREATE延迟关闭，UPDATE、部分失败和切人都保持打开", async () => {
  const { server, origin } = await localServer(true), browser = await chromium.launch({ channel: "chrome", headless: true }), drafts = new Map(), closes = [];
  let page, snapshot = null, duplicateMode = "none", failFollowup = true, autoClose = true;
  try {
    ({ page } = await preparePage(browser, origin, async ({ type, payload = {}, resume }) => {
      if (type === "CANDIDATE_SNAPSHOT") { snapshot = resume ? { ...resume, capture_id: `close-${resume.platform_candidate_id}` } : null; return { ok: true }; }
      if (type === "STATE") return { ok: true, data: { logged_in: true, data_use_accepted: true, auto_close_after_save: autoClose, account_key: "close-fixture", resume: snapshot, version } };
      if (type === "DRAFT_GET") return { ok: true, data: { draft: drafts.get(payload.identity) || null } };
      if (type === "DRAFT_SAVE") { drafts.set(payload.identity, payload.draft); return { ok: true, data: {} }; }
      if (type === "CHECK_UPDATE") return { ok: true, data: { current_version: version, latest_version: version, update_required: false } };
      if (type === "POSITIONS") return { ok: true, data: { items: [] } };
      if (type === "DUPLICATES") { const matches = duplicateMode === "exact" ? [{ match_type: "exact", reasons: ["手机号一致"], candidate: { id: "existing-close", candidate_name: snapshot.candidate_name, phone: snapshot.phone, current_position: "原职位", row_version: 1 } }] : []; return { ok: true, data: { matches, groups: [] } }; }
      if (type === "CREATE") return { ok: true, data: { candidate: { id: `created-${snapshot.platform_candidate_id}`, ...payload.resume, row_version: 1 } } };
      if (type === "UPDATE") return { ok: true, data: { candidate: { id: payload.candidate_id, ...payload.resume, row_version: 2 } } };
      if (type === "FOLLOWUPS") return { ok: true, data: { notes_saved: !failFollowup, recommendation_saved: true, errors: failFollowup ? [{ kind: "note", message: "合成备注失败" }] : [] } };
      if (type === "DETAIL") return { ok: true, data: { notes: [], recommendations: [] } };
      if (type === "CLOSE_FLOATING") { closes.push(payload.identity); return { ok: true, data: { closed: true } }; }
      return { ok: true, data: {} };
    }));
    await page.addScriptTag({ path: path.join(ROOT, "src", "shared.js") }); await page.addScriptTag({ path: path.join(ROOT, "src", "floating.js") });
    await page.evaluate((resume) => { window.fixtureCandidate = resume; window.GoodHRExtraction = { extractCurrentCandidate: () => window.fixtureCandidate }; }, sampleResume("partial", "13800000201"));
    await page.addScriptTag({ path: path.join(ROOT, "src", "content.js") }); await page.waitForFunction(() => Boolean(window.fixtureShadow?.querySelector("iframe")?.contentDocument?.getElementById("name")?.value));
    const frame = page.frames().find((item) => item.url().includes("/src/floating.html"));

    await frame.locator("#note").fill("这条备注模拟部分失败"); await frame.waitForFunction(() => !document.getElementById("save").disabled); await frame.locator("#save").click();
    await frame.waitForFunction(() => document.getElementById("status").textContent.includes("输入保留")); await page.waitForTimeout(650); assert.equal(closes.length, 0); assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), true);

    failFollowup = false; duplicateMode = "none";
    await page.evaluate((resume) => { window.fixtureCandidate = resume; window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, () => {}); }, sampleResume("switch-before-close", "13800000202"));
    await frame.waitForFunction(() => document.getElementById("phone").value === "13800000202" && !document.getElementById("save").disabled); await frame.locator("#save").click(); await frame.waitForFunction(() => document.getElementById("status").textContent === "已添加");
    await page.evaluate((resume) => { window.fixtureCandidate = resume; window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, () => {}); }, sampleResume("existing-update", "13800000203"));
    await frame.waitForFunction(() => document.getElementById("phone").value === "13800000203"); await page.waitForTimeout(650); assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), true); const closesAfterSwitch = closes.length;

    duplicateMode = "exact"; await frame.evaluate(() => checkDuplicates(true)); await frame.locator("#update").waitFor({ state: "visible" }); await frame.locator("#resume-details > summary").click(); await frame.locator("#position").fill("更新后的职位"); await frame.locator("#update").click();
    await frame.waitForFunction(() => document.getElementById("status").textContent === "已更新"); await page.waitForTimeout(650); assert.equal(closes.length, closesAfterSwitch); assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), true);

    autoClose = false; duplicateMode = "none"; await page.evaluate((resume) => { window.fixtureCandidate = resume; window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, () => {}); }, sampleResume("disabled-create", "13800000204"));
    await frame.waitForFunction(() => document.getElementById("phone").value === "13800000204" && !document.getElementById("save").disabled); await frame.locator("#save").click(); await frame.waitForFunction(() => document.getElementById("status").textContent === "已添加");
    await page.waitForTimeout(650); assert.equal(closes.length, closesAfterSwitch); assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), true);

    autoClose = true; await page.evaluate((resume) => { window.fixtureCandidate = resume; window.fixtureCaptureListener({ type: "REQUEST_CAPTURE" }, { id: "fixture-extension" }, () => {}); }, sampleResume("complete-create", "13800000205"));
    await frame.waitForFunction(() => document.getElementById("phone").value === "13800000205" && !document.getElementById("save").disabled); await frame.locator("#save").click(); await frame.waitForFunction(() => document.getElementById("status").textContent === "已添加");
    await page.waitForTimeout(300); assert.equal(closes.length, closesAfterSwitch); assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), true);
    await page.waitForFunction(() => document.getElementById("goodhr-resume-assistant").hidden); assert.equal(closes.length, closesAfterSwitch + 1);
  } finally { await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});
