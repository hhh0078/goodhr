/* 文件作用：在独立Chrome临时配置和合成招聘响应中验证实际扩展iframe的sender.tab，绝不访问真实账号或平台接口。 */
"use strict";
const path = require("node:path"), fs = require("node:fs"), assert = require("node:assert/strict"), { chromium } = require(path.resolve(__dirname, "../../cloud/frontend-next/node_modules/@playwright/test"));
const ROOT = path.resolve(__dirname, "..");

/** runChromeExtension 使用Chrome官方扩展调试协议加载隔离构建，仅记录UI消息来源字段，无任何凭证。 */
async function runChromeExtension() {
  const profile = fs.mkdtempSync(path.join(ROOT, "artifacts", "chrome-profile-"));
  const context = await chromium.launchPersistentContext(profile, { channel: "chrome", headless: true, ignoreDefaultArgs: ["--disable-extensions"], args: ["--enable-unsafe-extension-debugging"], viewport: { width: 1200, height: 900 } });
  try {
    const page = context.pages()[0], cdp = await context.browser().newBrowserCDPSession();
    const installed = await cdp.send("Extensions.loadUnpacked", { path: path.join(ROOT, "artifacts", "build-local-test", "chrome") });
    assert.ok(installed.id);
    const worker = context.serviceWorkers()[0] || await context.waitForEvent("serviceworker");
    await worker.evaluate(() => {
      globalThis.fixtureSenders = [];
      chrome.runtime.onMessage.addListener((message, sender) => { if (["STATE", "PREVIEW"].includes(message.type)) fixtureSenders.push({ type: message.type, url: sender.url, frameId: sender.frameId, tab: sender.tab ? { id: sender.tab.id, url: sender.tab.url } : null }); });
    });
    await context.route("**/*", (route) => {
      if (route.request().url().startsWith(`chrome-extension://${installed.id}/`)) return route.continue();
      if (route.request().url() === "https://h.liepin.com/resume/showresumedetail?resumeId=synthetic-extension-1") return route.fulfill({ contentType: "text/html; charset=utf-8", body: '<!doctype html><meta charset="utf-8"><div id="resume-detail"><div id="resume-detail-basic-info"><b class="name">合成扩展候选人</b><div class="connect-box"><dd>13800000171 复制</dd></div></div></div>' });
      if (route.request().url() === "https://www.zhipin.com/web/chat/index?token=synthetic-extension-only") return route.fulfill({ contentType: "text/html; charset=utf-8", body: '<!doctype html><meta charset="utf-8"><main id="boss-business">合成聊天内容，不属于附件</main><iframe title="合成附件预览" style="width:800px;height:600px" src="/bzl-office/pdf-viewer-b"></iframe>' });
      if (route.request().url() === "https://www.zhipin.com/bzl-office/pdf-viewer-b") return route.fulfill({ contentType: "text/html; charset=utf-8", body: '<!doctype html><meta charset="utf-8"><style>.page{position:relative;width:747px;height:1056px}.textLayer{position:absolute;inset:0}.textLayer span[role=presentation]{position:absolute;font:14px sans-serif}</style><div class="bzl-office-pdf" data-page-count="1"><div class="page" data-page-number="1"><div class="textLayer"><span><span><span role="presentation" style="left:70px;top:60px">合成学校</span></span></span><span role="presentation" style="left:70px;top:20px">合成附件候选人 13800000172</span></div></div></div>' });
      return route.abort();
    });
    await page.goto("https://h.liepin.com/resume/showresumedetail?resumeId=synthetic-extension-1");
    const host = page.locator("#goodhr-resume-assistant"); await host.waitFor();
    if (!page.frames().some((frame) => frame.url().startsWith(`chrome-extension://${installed.id}/src/floating.html`))) await page.waitForEvent("framenavigated", { predicate: (frame) => frame.url().startsWith(`chrome-extension://${installed.id}/src/floating.html`) });
    const frame = page.frames().find((item) => item.url().includes("/src/floating.html")); assert.ok(frame);
    await frame.locator("#duplicate-status").waitFor({ state: "visible" });
    await frame.locator("#name").waitFor({ state: "visible" });
    // 扩展状态读取是异步的，等待合成资料填入后再验证自动展示效果。
    await frame.waitForFunction(() => document.getElementById("name")?.value === "合成扩展候选人" && document.getElementById("phone")?.value === "13800000171");
    assert.equal(await frame.locator("#name").inputValue(), "合成扩展候选人"); assert.equal(await frame.locator("#phone").inputValue(), "13800000171");
    assert.equal(await frame.locator("#login-form,#consent,#pair-panel").count(), 0);
    const senders = await worker.evaluate(() => fixtureSenders); assert.ok(senders.length); assert.ok(senders.every((sender) => sender.tab && sender.frameId > 0 && sender.tab.url.includes("synthetic-extension-1")));
    fs.writeFileSync(path.join(ROOT, "artifacts", "chrome-extension-senders.json"), JSON.stringify({ description: "文件作用：只记录合成扩展iframe消息来源，无真实资料或凭证。", chrome: await context.browser()?.version(), senders }, null, 2));
    await page.goto("https://www.zhipin.com/web/chat/index?token=synthetic-extension-only");
    await page.locator("#goodhr-resume-assistant").waitFor();
    if (!page.frames().some((item) => item.url().startsWith(`chrome-extension://${installed.id}/src/floating.html`))) await page.waitForEvent("framenavigated", { predicate: (item) => item.url().startsWith(`chrome-extension://${installed.id}/src/floating.html`) });
    const bossFrame = page.frames().find((item) => item.url().startsWith(`chrome-extension://${installed.id}/src/floating.html`));
    await bossFrame.locator("#attachment-panel").waitFor({ state: "visible" });
    const bossState = await bossFrame.evaluate(() => chrome.runtime.sendMessage({ type: "STATE" }));
    assert.equal(bossState.ok, true); assert.equal(bossState.data.resume.platform_id, "boss"); assert.equal(bossState.data.resume.attachment.page_count, 1); assert.equal(bossState.data.resume.attachment.complete, true);
    assert.equal(bossState.data.resume.source_url, "https://www.zhipin.com/web/chat/index"); assert.equal(Object.hasOwn(bossState.data.resume.attachment, "text"), false); assert.match(bossState.data.resume.attachment.text_hash, /^[a-f0-9]{64}$/);
    assert.equal(await bossFrame.locator("#name").inputValue(), "", "未授权预览不能调用AI或猜姓名"); assert.equal(await page.locator("#boss-business").textContent(), "合成聊天内容，不属于附件");
    console.log(JSON.stringify({ installed: true, floating_preview: true, sender_tab_present: true, sender_frame: senders[0].frameId, boss_same_origin_pdf: true, boss_public_state_without_text: true, chrome: await context.browser()?.version() }));
  } finally {
    await context.close();
    if (!path.resolve(profile).startsWith(path.join(ROOT, "artifacts", "chrome-profile-"))) throw new Error("测试Chrome临时目录超出插件范围，停止清理");
    fs.rmSync(profile, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
  }
}
runChromeExtension().catch((error) => { console.error(error.message); process.exitCode = 1; });
