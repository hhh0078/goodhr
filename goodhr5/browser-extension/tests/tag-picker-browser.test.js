/* 文件作用：在插件自身本地文档验证标签搜索多选、草稿隔离、仅提交已有标签及显式清除，不访问招聘网站。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), path = require("node:path"), { pathToFileURL } = require("node:url");
const ROOT = path.resolve(__dirname, ".."), shared = require("../src/shared.js"), { chromium } = require(path.join(ROOT, "..", "cloud", "frontend-next", "node_modules", "@playwright", "test"));
const TEAM_ID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", PERSONAL_ID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";

test("标签选择只允许搜索已有团队/个人标签，切人保留草稿，添加和清除仅提交编号", async () => {
  const browser = await chromium.launch({ channel: "chrome", headless: true }), calls = [], drafts = new Map();
  let resume = { candidate_name: "合成标签甲", phone: "13800000151", platform_id: "hliepin", platform_candidate_id: "synthetic-tag-a", source_url: "https://h.liepin.com/resume/show", tags: ["招聘页面采集技能"] }, saved = null;
  try {
    const context = await browser.newContext({ viewport: { width: 360, height: 760 } }), page = await context.newPage(); await context.route(/^https?:/, (route) => route.abort());
    await context.exposeFunction("fixtureMessage", async ({ type, payload = {} }) => {
      calls.push({ type, payload });
      if (type === "STATE") return { ok: true, data: { logged_in: true, data_use_accepted: true, account_key: "synthetic-tag-account", resume, version: require("../package.json").version } };
      if (type === "PREVIEW") return { ok: true, data: { resume } };
      if (type === "DRAFT_SAVE") { drafts.set(payload.identity, payload.draft); return { ok: true, data: {} }; }
      if (type === "DRAFT_GET") return { ok: true, data: { draft: drafts.get(payload.identity) || null } };
      if (type === "TAGS") return { ok: true, data: { tags: [{ id: TEAM_ID, name: "合成团队", scope: "team", candidate_count: 12 }, { id: PERSONAL_ID, name: "合成个人", scope: "personal", candidate_count: 3 }] } };
      if (type === "POSITIONS") return { ok: true, data: { positions: [] } };
      if (type === "DUPLICATES") return { ok: true, data: { matches: saved ? [{ match_type: "exact", candidate: saved }] : [], groups: [] } };
      if (["CREATE", "UPDATE"].includes(type)) { saved = { id: "synthetic-tag-candidate", ...payload.resume, row_version: (saved?.row_version || 0) + 1 }; return { ok: true, data: { candidate: saved } }; }
      if (type === "FOLLOWUPS") return { ok: true, data: { notes_saved: true, recommendation_saved: true, errors: [] } };
      if (type === "DETAIL") return { ok: true, data: { notes: [], recommendations: [] } };
      return { ok: true, data: {} };
    });
    await context.addInitScript(() => { globalThis.chrome = { runtime: { sendMessage: (message) => globalThis.fixtureMessage(message) } }; });
    await page.goto(pathToFileURL(path.join(ROOT, "src", "floating.html")).href); await page.waitForFunction(() => !document.getElementById("save").disabled);
    assert.equal(await page.locator("#tag-options").isHidden(), true);
    await page.locator("#tag-filter").fill("合成"); await page.locator("#tag-options button").first().waitFor();
    assert.match(await page.locator(`#tag-options [data-value="${TEAM_ID}"]`).innerText(), /团队可见.*12 人/);
    assert.match(await page.locator(`#tag-options [data-value="${PERSONAL_ID}"]`).innerText(), /仅自己可见.*3 人/);
    await page.locator(`#tag-options [data-value="${TEAM_ID}"]`).click();
    await page.locator("#tag-filter").fill("个人"); await page.locator(`#tag-options [data-value="${PERSONAL_ID}"]`).click();
    assert.equal(await page.locator("#selected-tags button").count(), 2);
    await page.locator("#tag-filter").fill("并不存在的新标签"); await page.locator("#tag-filter").press("Enter");
    assert.equal(await page.locator("#selected-tags button").count(), 2); assert.equal(calls.some((call) => call.type === "CREATE" || call.type.startsWith("TAG_CREATE")), false); assert.match(await page.locator("#tag-hint").innerText(), /管理标签新增/);
    const first = resume; resume = { ...first, candidate_name: "合成标签乙", platform_candidate_id: "synthetic-tag-b", phone: "13800000152" };
    await page.locator("#reload").click(); await page.waitForFunction(() => document.getElementById("name").value === "合成标签乙" && !document.getElementById("reload").disabled); assert.equal(await page.locator("#selected-tags button").count(), 0);
    resume = first; await page.locator("#reload").click(); await page.waitForFunction(() => document.getElementById("name").value === "合成标签甲" && !document.getElementById("save").disabled); assert.equal(await page.locator("#selected-tags button").count(), 2);
    await page.locator("#save").click(); await page.waitForFunction(() => !document.getElementById("reload").disabled && !document.getElementById("update").hidden);
    const create = calls.find((call) => call.type === "CREATE"); assert.deepEqual(create.payload.resume.tag_ids, [TEAM_ID, PERSONAL_ID]); assert.equal(Object.hasOwn(create.payload.resume, "tags"), false);
    await page.locator("#resume-details > summary").click(); await page.locator("#check").click(); await page.waitForFunction(() => !document.getElementById("reload").disabled); assert.equal(await page.locator("#selected-tags button").count(), 2, "查重回填已保存可见标签");
    await page.locator(`#selected-tags [data-value="${TEAM_ID}"]`).click(); await page.locator(`#selected-tags [data-value="${PERSONAL_ID}"]`).click();
    await page.locator("#update").click(); await page.waitForFunction(() => !document.getElementById("reload").disabled && document.getElementById("status").textContent === "已更新");
    assert.deepEqual(calls.filter((call) => call.type === "UPDATE").at(-1).payload.resume.tag_ids, []); assert.equal(await page.locator("#selected-tags button").count(), 0);
  } finally { await browser.close(); }
});

test("标签清除会被识别为更新，标签顺序变化不造成假更新且不改变 AI 总结依据", () => {
  const input = { tag_ids: [TEAM_ID, PERSONAL_ID], tags: ["合成技能"] }, clean = shared.candidateWriteResume(input);
  assert.deepEqual(clean.tag_ids, input.tag_ids); assert.equal(Object.hasOwn(clean, "tags"), false);
  assert.ok(shared.describeResumeChanges(input, { tag_ids: [] }).some((line) => line.startsWith("简历标签：")));
  assert.equal(shared.describeResumeChanges(input, { tag_ids: [PERSONAL_ID, TEAM_ID] }).some((line) => line.startsWith("简历标签：")), false);
  assert.equal(shared.summaryAction({ status: "completed" }, false, input, { ...input, tag_ids: [] }), "new");
  assert.equal(shared.sanitizeTagIDs(Array.from({ length: 35 }, (_, index) => `aaaaaaaa-aaaa-4aaa-8aaa-${String(index).padStart(12, "0")}`)).length, 30);
});
