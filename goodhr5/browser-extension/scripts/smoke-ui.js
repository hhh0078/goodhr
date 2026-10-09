/* 文件作用：复用仓库已有 Playwright 验证插件本地 UI 的候选人选择、备注重试和安全渲染，不连接真实账号。 */
"use strict";
const path = require("node:path"), fs = require("node:fs"), assert = require("node:assert/strict"), { pathToFileURL } = require("node:url");
const ROOT = path.resolve(__dirname, ".."), { chromium } = require(path.join(ROOT, "..", "cloud", "frontend-next", "node_modules", "@playwright", "test"));

/** runSmoke 使用本地页面和假接口验证真实浏览器 UI，网络一律阻断。 */
async function runSmoke() {
  const browser = await chromium.launch({ channel: "chrome", headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 420, height: 900 } });
    await page.route(/^https?:/, (route) => route.abort());
    const errors = []; page.on("pageerror", (error) => errors.push(error.message));
    await page.addInitScript(() => {
      const fixture = { resume: { candidate_name: "测试候选人", phone: "13812345678", platform_id: "liepin", work_region: "上海", current_position: "测试职位", work_experiences: [{ company_name: "合成预览公司", position_name: "测试工程师", start_ym: "2021-01", end_ym: "", content: "只用于合成测试的工作经历" }] }, logged_in: true, user: { email: "fixture@example.test" }, version: "1.0.0", local_port: 43129, data_use_accepted: true };
      globalThis.testCalls = []; let failedNote = false;
      globalThis.chrome = { runtime: { sendMessage: async ({ type, payload }) => {
        globalThis.testCalls.push({ type, payload });
        const result = {
          STATE: fixture, PREVIEW: { resume: fixture.resume, version: fixture.version }, POSITIONS: { positions: [{ id: "position-1", name: "测试岗位" }] },
          DUPLICATES: { matches: [{ match_type: "possible", reasons: ["姓名相似"], candidate: { id: "candidate-1", candidate_name: "<img src=x onerror=alert(1)>", phone: "13812345678", row_version: 1 } }] },
          DETAIL: { notes: [{ content: "旧备注\n保留换行", author_name: "测试人", created_at: "2026-10-10T00:00:00Z" }], recommendations: [] }, CREATE: { candidate: { id: "candidate-2", candidate_name: "测试候选人", row_version: 1 } }, UPDATE: { candidate: { id: "candidate-1", row_version: 2 } }
        };
        if (type === "FOLLOWUPS") {
          if (!failedNote) { failedNote = true; return { ok: true, data: { notes_saved: false, recommendation_saved: true, errors: [{ kind: "note", message: "备注测试失败" }] } }; }
          return { ok: true, data: { notes_saved: true, recommendation_saved: true, errors: [] } };
        }
        if (type === "LOGOUT") { fixture.logged_in = false; fixture.revocation_pending = true; fixture.revocation_message = "本地程序连接授权仍未撤销，请重试"; return { ok: true, data: { revocation_pending: true, message: fixture.revocation_message } }; }
        if (type === "REVOKE_RETRY") { fixture.revocation_pending = false; fixture.revocation_message = ""; return { ok: true, data: { revocation_pending: false, message: "已退出，旧授权已经撤销" } }; }
        if (type === "LOGIN") return { ok: false, status: 404, message: "not found" };
        return { ok: true, data: result[type] || {} };
      } } };
    });
    await page.goto(pathToFileURL(path.join(ROOT, "src", "ui.html")).href);
    await page.locator("#name").waitFor({ state: "visible" });
    assert.equal(await page.locator("#name").inputValue(), "测试候选人");
    await page.locator("#check").click(); await page.locator(".match").waitFor();
    assert.equal(await page.locator("#duplicates img").count(), 0); assert.match(await page.locator(".match").innerText(), /可能重复/);
    await page.locator(".match input").check(); await page.locator("#save-related").waitFor({ state: "visible" });
    let updateConfirmation = ""; page.once("dialog", async (dialog) => { updateConfirmation = dialog.message(); await dialog.dismiss(); }); await page.locator("#update").click();
    assert.match(updateConfirmation, /未读到和未填写的字段会保留已有内容/); assert.match(updateConfirmation, /工作经历/);
    await page.locator("#note").fill("测试备注\n下一步跟进"); await page.locator("#job").selectOption("position-1"); await page.locator("#save-related").click();
    await page.waitForFunction(() => document.getElementById("status").textContent.includes("输入保留"));
    assert.equal(await page.locator("#note").inputValue(), "测试备注\n下一步跟进"); assert.equal(await page.locator("#job").inputValue(), "");
    await page.locator("#save-related").click(); await page.waitForFunction(() => document.getElementById("note").value === "");
    const calls = await page.evaluate(() => globalThis.testCalls.filter((item) => item.type === "FOLLOWUPS"));
    assert.equal(calls.length, 2); assert.equal(calls[0].payload.note_key, calls[1].payload.note_key); assert.equal(calls[1].payload.position_id, "");
    await page.locator("#logout").click(); await page.locator("#revocation-panel").waitFor({ state: "visible" });
    assert.match(await page.locator("#revocation-message").innerText(), /仍未撤销/); assert.equal(await page.locator("#login-panel").isVisible(), true);
    await page.locator("#revoke-retry").click(); await page.locator("#revocation-panel").waitFor({ state: "hidden" });
    assert.equal(await page.locator("#name").isVisible(), true); assert.equal(await page.locator("#check").isDisabled(), true); assert.equal(await page.locator("#save").isDisabled(), true);
    await page.locator("#candidate-form details summary").click(); assert.match(await page.locator("#resume-preview").innerText(), /合成预览公司/); assert.equal((await page.locator("#resume-preview").innerText()).includes("company_name"), false);
    const beforePreview = await page.evaluate(() => globalThis.testCalls.length);
    await page.locator("#reload").click(); const previewCalls = await page.evaluate((offset) => globalThis.testCalls.slice(offset), beforePreview);
    assert.ok(previewCalls.some((item) => item.type === "PREVIEW")); assert.equal(previewCalls.some((item) => ["POSITIONS", "DUPLICATES", "CREATE", "UPDATE", "FOLLOWUPS"].includes(item.type)), false);
    await page.locator("#login-panel > summary").click(); await page.locator("#email").fill("fixture@example.test"); await page.locator("#password").fill("synthetic-password"); await page.locator("#agreement").check(); await page.locator("#login-form button[type=submit]").click();
    await page.waitForFunction(() => document.getElementById("status").textContent.includes("当前服务还没更新插件登录")); assert.equal(await page.locator("#workspace").isVisible(), true);
    await page.locator("#login-panel > summary").click();
    assert.deepEqual(errors, []);
    fs.mkdirSync(path.join(ROOT, "artifacts"), { recursive: true }); await page.screenshot({ path: path.join(ROOT, "artifacts", "plugin-sidebar-smoke.png"), fullPage: true });
    console.log("插件 UI 验证通过：相似查询人工选择、安全文本显示、备注幂等重试、退出撤销重试、未登录本地中文预览和登录404指引。浏览器使用本地假接口，无真实数据。 ");
  } finally { await browser.close(); }
}

runSmoke().catch((error) => { console.error(error); process.exitCode = 1; });
