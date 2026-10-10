/* 文件作用：用本机Chrome和纯本地合成页面验证隔离悬浮入口、拖动、iframe预览和业务DOM保护；不访问招聘网站。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), http = require("node:http");
const ROOT = path.resolve(__dirname, ".."), { chromium } = require(path.join(ROOT, "..", "cloud", "frontend-next", "node_modules", "@playwright", "test"));
const version = require("../package.json").version;

/** localServer 只在随机loopback端口提供插件静态文件和合成招聘DOM，不提供业务API或外部代理。 */
async function localServer() {
  const html = '<!doctype html><style>body{margin:0;background:#f8f7f1;font:14px system-ui}button{background:red;color:white;font-size:30px}#business{padding:24px;height:1600px}</style><main id="business"><h1>合成候选人详情</h1><input id="business-input" value="不变的页面输入"><button id="business-action">招聘页面按钮</button><p>合成手机号13800000151 · 公司/学历仅作合成验证</p></main>';
  const server = http.createServer((request, response) => {
    const file = new URL(request.url, "http://127.0.0.1").pathname;
    if (file === "/fixture") { response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" }); response.end(html); return; }
    const filename = /^\/src\/[a-z.]+$/.test(file) ? path.join(ROOT, file.slice(1)) : "";
    if (!filename || !fs.existsSync(filename)) { response.writeHead(404); response.end(); return; }
    response.writeHead(200, { "Content-Type": file.endsWith(".html") ? "text/html; charset=utf-8" : file.endsWith(".css") ? "text/css" : "application/javascript" }); response.end(fs.readFileSync(filename));
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve)); return { server, origin: `http://127.0.0.1:${server.address().port}` };
}

/** point 在测试捕获的closed Shadow中读取自己控件位置，点击始终通过真实鼠标完成。 */
async function point(page, selector) { return page.evaluate((selector) => { const rect = window.fixtureShadow.querySelector(selector).getBoundingClientRect(); return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 }; }, selector); }

test("Chrome合成页面：悬浮隔离、懒加载预览、拖动/收起保留输入，网页消息无权保存", async () => {
  const { server, origin } = await localServer(), browser = await chromium.launch({ channel: "chrome", headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1150, height: 900 } }), calls = [], network = [];
    await page.route("**/*", (route) => { const url = route.request().url(); network.push(url); if (url.startsWith(origin + "/")) return route.continue(); return route.abort(); });
    const resume = { candidate_name: "合成悬浮候选人", phone: "13800000151", platform_id: "hliepin", capture_id: "synthetic-floating-capture", work_experiences: [{ company_name: "合成悬浮公司", position_name: "合成岗位", start_ym: "2021-01", content: "仅用于本地测试" }] };
    await page.exposeFunction("fixtureMessage", async ({ type }) => { calls.push(type); if (["STATE", "PREVIEW"].includes(type)) return { ok: true, data: { logged_in: false, account_key: "preview", data_use_accepted: false, resume, version } }; if (type === "DRAFT_GET") return { ok: true, data: { draft: null } }; return { ok: true, data: {} }; });
    await page.addInitScript(({ origin, version }) => {
      window.chrome = { runtime: { getURL: (file) => origin + "/" + file, getManifest: () => ({ version }), sendMessage: (message) => window.fixtureMessage(message) } };
      // 仅测试页捕获自身closed Shadow引用；生产脚本没有调试入口或暴露内部DOM。
      const attachShadow = Element.prototype.attachShadow; Element.prototype.attachShadow = function (options) { const shadow = attachShadow.call(this, options); if (this.id === "goodhr-resume-assistant") window.fixtureShadow = shadow; return shadow; };
    }, { origin, version });
    await page.goto(origin + "/fixture");
    const before = await page.locator("#business").innerHTML();
    await page.locator("#business-input").focus();
    await page.addScriptTag({ path: path.join(ROOT, "src", "floating.js") });
    await page.evaluate((resume) => GoodHRFloating.update(resume), resume);
    assert.equal(await page.locator("#business").innerHTML(), before); assert.equal(await page.evaluate(() => document.activeElement.id), "business-input");
    assert.equal(await page.evaluate(() => document.getElementById("goodhr-resume-assistant").shadowRoot), null);
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector("iframe")), null); assert.equal(calls.length, 0);
    const open = await point(page, ".open"); await page.mouse.click(open.x, open.y);
    await page.waitForFunction(() => [...document.querySelectorAll("iframe")].length === 0 && window.fixtureShadow.querySelector("iframe"));
    const frame = page.frames().find((item) => item.url().includes("/src/ui.html?surface=floating")) || await page.waitForEvent("framenavigated", { predicate: (item) => item.url().includes("/src/ui.html?surface=floating") });
    await frame.locator("#name").waitFor({ state: "visible" }); assert.equal(await frame.locator("#name").inputValue(), resume.candidate_name); assert.equal(await frame.locator("#phone").inputValue(), resume.phone);
    await frame.getByText("已读取的更多资料", { exact: true }).click();
    await frame.waitForFunction(() => document.getElementById("resume-preview").innerText.includes("合成悬浮公司"));
    assert.match(await frame.locator("#resume-preview").innerText(), /合成悬浮公司/);
    await frame.locator("#phone").fill("13800000152");
    await page.evaluate(() => { window.postMessage({ type: "CREATE", payload: { candidate_name: "伪造" } }, "*"); window.fixtureShadow.querySelector("iframe").contentWindow.postMessage({ type: "LOGIN", payload: { email: "evil@example.test" } }, "*"); });
    assert.equal(calls.some((type) => ["LOGIN", "CREATE", "UPDATE", "DUPLICATES", "FOLLOWUPS"].includes(type)), false);
    const bar = await point(page, ".bar"); await page.mouse.move(bar.x, bar.y); await page.mouse.down(); await page.mouse.move(-70, -70, { steps: 8 }); await page.mouse.up();
    const rectangle = await page.locator("#goodhr-resume-assistant").boundingBox(); assert.ok(rectangle.x >= 7 && rectangle.y >= 7); assert.ok(rectangle.x + rectangle.width <= 1150 && rectangle.y + rectangle.height <= 900);
    const close = await point(page, ".close"); await page.mouse.click(close.x, close.y);
    assert.equal(await page.evaluate(() => window.fixtureShadow.querySelector(".panel").hidden), true);
    const reopen = await point(page, ".open"); await page.mouse.click(reopen.x, reopen.y); assert.equal(await frame.locator("#phone").inputValue(), "13800000152");
    assert.equal(await page.locator("#business").innerHTML(), before);
    assert.equal(network.some((url) => !url.startsWith(origin + "/")), false);
    await frame.locator("header").scrollIntoViewIfNeeded();
    fs.mkdirSync(path.join(ROOT, "artifacts"), { recursive: true }); await page.screenshot({ path: path.join(ROOT, "artifacts", `plugin-floating-${version}.png`) });
    await page.evaluate(() => GoodHRFloating.update(null)); assert.equal(await page.locator("#goodhr-resume-assistant").isVisible(), false);
    await page.evaluate((resume) => { GoodHRFloating.update(resume); GoodHRFloating.update(resume); }, resume); assert.equal(await page.locator("#goodhr-resume-assistant").count(), 1);
  } finally { await browser.close(); await new Promise((resolve) => server.close(resolve)); }
});
