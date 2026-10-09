/* 文件作用：验证备注和岗位独立保存、失败重试幂等键以及浏览器清单和打包兼容性。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), zlib = require("node:zlib");
const shared = require("../src/shared.js"), { manifest, zipBuffer } = require("../scripts/build.js");

test("备注失败仍保存岗位，重试保留备注幂等键并不重建候选人", async () => {
  const calls = []; let first = true;
  const request = async (path, body) => { calls.push({ path, body }); if (path.endsWith("/notes") && first) { first = false; throw new Error("备注暂时没保存好"); } return { ok: true }; };
  const draft = { note: "测试沟通\n下一步", position_id: "job-1", note_key: "same-note", recommendation_key: "same-job" };
  const result = await shared.saveFollowups(request, "candidate-1", draft);
  assert.equal(result.notes_saved, false); assert.equal(result.recommendation_saved, true); assert.equal(result.errors[0].kind, "note");
  const retry = await shared.saveFollowups(request, "candidate-1", { ...draft, position_id: "" });
  assert.equal(retry.notes_saved, true); assert.equal(calls.length, 3); assert.equal(calls[0].body.idempotency_key, calls[2].body.idempotency_key);
  assert.equal(calls.some((item) => item.path === "/api/candidates"), false);
  assert.equal(calls[1].body.stage, "draft");
});

test("没有备注和岗位时没有多余写请求", async () => {
  const result = await shared.saveFollowups(() => { throw new Error("不应请求"); }, "candidate", { note: "   ", position_id: "" });
  assert.deepEqual(result, { notes_saved: true, recommendation_saved: true, errors: [] });
});

test("备注按Unicode字符限1000字，超长保留原文不截断且岗位仍独立保存", async () => {
  const calls = [], note = "😀".repeat(1001), draft = { note, position_id: "fixture-job", note_key: "note-key", recommendation_key: "job-key" };
  const result = await shared.saveFollowups(async (path, body) => { calls.push({ path, body }); return { ok: true }; }, "fixture", draft);
  assert.equal(result.notes_saved, false); assert.equal(result.recommendation_saved, true); assert.match(result.errors[0].message, /原文保留/); assert.equal(draft.note, note);
  assert.equal(calls.length, 1); assert.equal(calls[0].path.endsWith("/recommendations"), true);
  const boundary = "😀".repeat(1000); const saved = await shared.saveFollowups(async (path, body) => { assert.equal(body.content, boundary); }, "fixture", { note: boundary, note_key: "same-note" }); assert.equal(saved.notes_saved, true);
});

test("更新前差异预览只列非空变化，并用中文显示职位与经历", () => {
  const old = { candidate_name: "合成候选人", phone: "13800000001", current_position: "旧职位", source_type: "browser_extension", work_experiences: [{ company_name: "合成原公司", content: "原经历" }] };
  const draft = { candidate_name: old.candidate_name, phone: old.phone, current_position: "新职位", work_experiences: [], email: "", tags: [] };
  assert.deepEqual(shared.describeResumeChanges(old, draft), ["当前职位：旧职位 → 新职位"]);
  const patch = shared.nonEmptyResumePatch({ ...draft, educations: [{}] });
  assert.equal(patch.work_experiences, undefined); assert.equal(patch.educations, undefined); assert.equal(patch.tags, undefined);
});

test("Chrome 和 Firefox 版本统一，使用各自后台和原生侧栏且无全站/网络拦截权限", () => {
  const chrome = manifest("chrome"), firefox = manifest("firefox");
  assert.equal(chrome.version, "1.0.1"); assert.equal(chrome.version, firefox.version);
  assert.equal(chrome.background.service_worker, "src/background.js"); assert.equal(firefox.background.service_worker, undefined); assert.deepEqual(firefox.background.scripts, ["src/shared.js", "src/avatar.js", "src/background.js"]);
  assert.equal(chrome.side_panel.default_path, firefox.sidebar_action.default_panel);
  for (const value of [chrome, firefox]) {
    assert.equal(value.host_permissions.includes("<all_urls>"), false);
    for (const forbidden of ["cookies", "webRequest", "scripting", "tabs", "debugger", "downloads", "nativeMessaging"]) assert.equal(value.permissions.includes(forbidden), false);
    assert.equal(value.content_scripts[0].all_frames, false); assert.equal(value.content_scripts[0].css, undefined);
  }
  assert.ok(firefox.browser_specific_settings.gecko.data_collection_permissions.required.includes("personallyIdentifyingInfo"));
});

test("开发 ZIP 包可正确解压并保留文件名和内容", () => {
  const files = [{ name: "manifest.json", content: Buffer.from('{"version":"1.0.0"}') }, { name: "src/test.js", content: Buffer.from("测试内容") }];
  const archive = zipBuffer(files); let offset = 0;
  for (const file of files) {
    assert.equal(archive.readUInt32LE(offset), 0x04034b50); const size = archive.readUInt32LE(offset + 18), nameLength = archive.readUInt16LE(offset + 26);
    assert.equal(archive.subarray(offset + 30, offset + 30 + nameLength).toString(), file.name);
    const compressed = archive.subarray(offset + 30 + nameLength, offset + 30 + nameLength + size); assert.deepEqual(zlib.inflateRawSync(compressed), file.content);
    offset += 30 + nameLength + size;
  }
  assert.equal(archive.readUInt32LE(offset), 0x02014b50); assert.equal(archive.readUInt32LE(archive.length - 22), 0x06054b50);
});
