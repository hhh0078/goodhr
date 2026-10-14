/* 文件作用：在插件自身文档验证收藏夹搜索多选、草稿隔离及与原技能标签分别保存，不访问招聘网站。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), fs = require("node:fs"), path = require("node:path"), { pathToFileURL } = require("node:url");
const ROOT = path.resolve(__dirname, ".."), shared = require("../src/shared.js"), { chromium } = require(path.join(ROOT, "..", "cloud", "frontend-next", "node_modules", "@playwright", "test"));
const TEAM_ID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", PERSONAL_ID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";

test("原技能保留预览和保存，收藏夹只选已有团队/个人分类且切人恢复草稿", async () => {
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
      if (type === "FOLDERS") return { ok: true, data: { folders: [{ id: TEAM_ID, name: "合成团队", scope: "team", candidate_count: 12 }, { id: PERSONAL_ID, name: "合成个人", scope: "personal", candidate_count: 3 }] } };
      if (type === "POSITIONS") return { ok: true, data: { positions: [] } };
      if (type === "DUPLICATES") return { ok: true, data: { matches: saved ? [{ match_type: "exact", candidate: saved }] : [], groups: [] } };
      if (["CREATE", "UPDATE"].includes(type)) { saved = { id: "synthetic-tag-candidate", ...payload.resume, row_version: (saved?.row_version || 0) + 1 }; return { ok: true, data: { candidate: saved } }; }
      if (type === "FOLLOWUPS") return { ok: true, data: { notes_saved: true, recommendation_saved: true, errors: [] } };
      if (type === "DETAIL") return { ok: true, data: { notes: [], recommendations: [] } };
      return { ok: true, data: {} };
    });
    await context.addInitScript(() => { globalThis.chrome = { runtime: { sendMessage: (message) => globalThis.fixtureMessage(message) } }; });
    await page.goto(pathToFileURL(path.join(ROOT, "src", "floating.html")).href); await page.waitForFunction(() => !document.getElementById("save").disabled);
    assert.equal(await page.locator("#selected-folders button").count(), 0, "采集技能不能自动加入收藏夹");
    await page.locator("#resume-details > summary").click(); assert.match(await page.locator("#resume-preview").innerText(), /技能标签：招聘页面采集技能/); await page.locator("#resume-details > summary").click();
    assert.equal(await page.locator("#folder-options").isHidden(), true);
    await page.locator("#folder-filter").fill("合成"); await page.locator("#folder-options button").first().waitFor();
    assert.match(await page.locator(`#folder-options [data-value="${TEAM_ID}"]`).innerText(), /团队可见.*12 人/);
    assert.match(await page.locator(`#folder-options [data-value="${PERSONAL_ID}"]`).innerText(), /仅自己可见.*3 人/);
    await page.locator(`#folder-options [data-value="${TEAM_ID}"]`).click();
    await page.locator("#folder-filter").fill("个人"); await page.locator(`#folder-options [data-value="${PERSONAL_ID}"]`).click();
    assert.equal(await page.locator("#selected-folders button").count(), 2);
    await page.locator("#resume-details > summary").click(); fs.mkdirSync(path.join(ROOT, "artifacts"), { recursive: true }); await page.screenshot({ path: path.join(ROOT, "artifacts", "plugin-folder-selection-1.0.9.png"), fullPage: true }); await page.locator("#resume-details > summary").click();
    await page.locator("#folder-filter").fill("并不存在的新标签"); await page.locator("#folder-filter").press("Enter");
    assert.equal(await page.locator("#selected-folders button").count(), 2); assert.equal(calls.some((call) => call.type === "CREATE" || call.type.startsWith("FOLDER_CREATE")), false); assert.match(await page.locator("#folder-hint").innerText(), /管理收藏夹新增/);
    const first = resume; resume = { ...first, candidate_name: "合成标签乙", platform_candidate_id: "synthetic-tag-b", phone: "13800000152" };
    await page.locator("#reload").click(); await page.waitForFunction(() => document.getElementById("name").value === "合成标签乙" && !document.getElementById("reload").disabled); assert.equal(await page.locator("#selected-folders button").count(), 0);
    resume = first; await page.locator("#reload").click(); await page.waitForFunction(() => document.getElementById("name").value === "合成标签甲" && !document.getElementById("save").disabled); assert.equal(await page.locator("#selected-folders button").count(), 2);
    await page.locator("#save").click(); await page.waitForFunction(() => !document.getElementById("reload").disabled && !document.getElementById("update").hidden);
    const create = calls.find((call) => call.type === "CREATE"); assert.deepEqual(create.payload.resume.folder_ids, [TEAM_ID, PERSONAL_ID]); assert.deepEqual(create.payload.resume.tags, first.tags);
    await page.locator("#resume-details > summary").click(); await page.locator("#check").click(); await page.waitForFunction(() => !document.getElementById("reload").disabled); assert.equal(await page.locator("#selected-folders button").count(), 2, "查重回填已保存可见标签");
    await page.locator(`#selected-folders [data-value="${TEAM_ID}"]`).click(); await page.locator(`#selected-folders [data-value="${PERSONAL_ID}"]`).click();
    await page.locator("#update").click(); await page.waitForFunction(() => !document.getElementById("reload").disabled && document.getElementById("status").textContent === "已更新");
    const clear = calls.filter((call) => call.type === "UPDATE").at(-1).payload.resume; assert.deepEqual(clear.folder_ids, []); assert.deepEqual(clear.tags, first.tags); assert.equal(await page.locator("#selected-folders button").count(), 0);
    resume = { ...first, tags: ["新的招聘技能"] }; await page.locator("#reload").click(); await page.waitForFunction(() => !document.getElementById("reload").disabled); await page.locator("#update").click(); await page.waitForFunction(() => !document.getElementById("reload").disabled);
    const skillUpdate = calls.filter((call) => call.type === "UPDATE").at(-1).payload.resume; assert.deepEqual(skillUpdate.tags, resume.tags); assert.deepEqual(skillUpdate.folder_ids, [], "技能变化不能加入或新增收藏夹");
  } finally { await browser.close(); }
});

test("收藏夹清除会更新分类且排序不产生假变化，AI仅保留原技能标签", () => {
  const input = { folder_ids: [TEAM_ID, PERSONAL_ID], tags: ["合成技能"] }, clean = shared.candidateWriteResume(input);
  assert.deepEqual(clean.folder_ids, input.folder_ids); assert.deepEqual(clean.tags, input.tags);
  assert.ok(shared.describeResumeChanges(input, { folder_ids: [] }).some((line) => line.startsWith("收藏夹：")));
  assert.equal(shared.describeResumeChanges(input, { folder_ids: [PERSONAL_ID, TEAM_ID] }).some((line) => line.startsWith("收藏夹：")), false);
  assert.equal(shared.summaryAction({ status: "completed" }, false, input, { ...input, folder_ids: [] }), "new");
  assert.deepEqual(shared.stripSummaryContacts(input).tags, input.tags); assert.equal(Object.hasOwn(shared.stripSummaryContacts(input), "folder_ids"), false);
  assert.deepEqual(shared.stripSummaryContacts(input), shared.stripSummaryContacts({ ...input, folder_ids: [] }));
  assert.deepEqual(shared.candidateWriteResume({ tags: ["Java"], tag_ids: [TEAM_ID] }).folder_ids, [], "旧分类字段不能隐式转收藏夹");
  assert.equal(shared.sanitizeFolderIDs(Array.from({ length: 35 }, (_, index) => `aaaaaaaa-aaaa-4aaa-8aaa-${String(index).padStart(12, "0")}`)).length, 30);
});
