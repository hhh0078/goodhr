/** 文件作用说明：验证待确认接口边界拒绝坏计数、错账号迟到结果及不完整问题，分页只提交当前岗位游标。 */
import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";
import ts from "typescript";

const source = await readFile(new URL("./auto-reply-inbox.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText
  .replace(/import\s*\{[^}]+\}\s*from\s*["']\.\/admin-api["'];/, "const { cloudRequest, getToken } = globalThis.__replyInboxTestAPI;");
let sequence = 0;

/** withInbox 在隔离接口下加载真实转换代码，结束后恢复全局测试桩。 */
async function withInbox(handler, run) {
  const previous = globalThis.__replyInboxTestAPI;
  let token = "test-owner";
  globalThis.__replyInboxTestAPI = { cloudRequest: handler, getToken: () => token };
  try {
    const api = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}#${++sequence}`);
    await run(api, value => { token = value; });
  } finally {
    if (previous === undefined) delete globalThis.__replyInboxTestAPI;
    else globalThis.__replyInboxTestAPI = previous;
  }
}

test("缺失或坏数量不能被当成零，岗位状态为空保持关闭", async () => {
  for (const value of [undefined, null, "0", -1, 1.5, Number.MAX_SAFE_INTEGER + 1]) {
    await withInbox(async () => ({ positions: [], pending_count: value }), async api => {
      await assert.rejects(api.loadReplyInboxSummary(new AbortController().signal), /数量/);
    });
  }
  await withInbox(async () => ({ pending_count: 0, positions: [{ position_id: "closed", position_name: "关闭岗位", pending_count: 0 }] }), async api => {
    assert.equal((await api.loadReplyInboxSummary(new AbortController().signal)).positions[0].is_open, false);
  });
});

test("旧账号晚到响应不能进入新账号页面", async () => {
  let changeToken;
  await withInbox(async () => { changeToken("other-owner"); return { pending_count: 0, positions: [] }; }, async (api, update) => {
    changeToken = update;
    await assert.rejects(api.loadReplyInboxSummary(new AbortController().signal), /账号已变化/);
  });
});

test("分页只携带当前岗位范围，缺失正文安全显示而缺失版本必须拒绝", async () => {
  const calls = [];
  await withInbox(async path => { calls.push(path); return { pending_count: 2, next_cursor: "next", questions: [{ id: "q", question: "薪资范围？", version: 1, source_messages: null }] }; }, async api => {
    const page = await api.loadReplyInboxPage("position/a", "all", "cursor & signed?", new AbortController().signal);
    assert.deepEqual(page.questions[0].source_messages, []);
    assert.equal(page.questions[0].answer, "");
    const query = new URL(calls[0], "http://localhost").searchParams;
    assert.equal(query.get("position_id"), "position/a");
    assert.equal(query.get("view"), "all");
    assert.equal(query.get("cursor"), "cursor & signed?");
    assert.equal(query.get("limit"), "10");
    for (const version of [undefined, "1", 0, -1, 1.5]) assert.throws(() => api.normalizeReplyQuestion({ id: "q", question: "问题", version }), /完整/);
  });
});
