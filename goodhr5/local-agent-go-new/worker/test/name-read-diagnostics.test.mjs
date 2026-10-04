// 文件作用说明：验证姓名读取诊断区分定位、超时和正文变化，且不泄露候选人资料。
import assert from "node:assert/strict";
import test from "node:test";
import { FindAction } from "../dist/browser/actions/find.js";
import { WorkerError } from "../dist/errors/worker-error.js";
import { LocatorPrimitive } from "../dist/browser/primitives/locator.js";

/** harness 使用只读模拟对象复现字段读取失败，不接触真实招聘页面。 */
function harness(rows) {
  const logs = [];
  const fieldReads = new Map();
  const page = { url: () => "https://example.com/list?token=private-token#secret" };
  const resolved = rows.map((row, index) => {
    let reads = 0;
    return {
      page, view: { visible: true }, matched_selector: { type: "css", value: ".card" },
      attempts: [{ level: "target", selector_type: "css", selector_value: ".card", matches: rows.length, selected_index: index }],
      locator: { index, async innerText() {
        reads++;
        if (reads === 1 && row.beforeError) throw row.beforeError;
        if (reads === 2 && row.afterError) throw row.afterError;
        return reads === 2 && row.changed ? "另一段私密简历" : "私密简历正文";
      } },
    };
  });
  const logger = {};
  for (const level of ["info", "warn", "failure"]) logger[level] = (context, step, status, fields) => logs.push({ level, step, status, fields });
  const action = new FindAction(
    { async requirePage() { return page; }, elements: { remember() { return "ref"; } } },
    { async resolveAll() { return resolved; }, async resolveRelative(_page, root) {
      const row = rows[root.index];
      if (row.locateError) throw row.locateError;
      return { ...resolved[root.index], locator: { async innerText() {
        const count = (fieldReads.get(root.index) ?? 0) + 1;
        fieldReads.set(root.index, count);
        if (row.readError || (row.readErrorOnce && count === 1)) throw row.readError ?? row.readErrorOnce;
        return row.name ?? "私密姓名";
      } } };
    } }, logger,
  );
  return { logs, async run() {
    return action.all({ target: { selectors: [{ type: "css", value: ".card" }] }, description: "列表" }, 100,
      { name: { target: { selectors: [{ type: "css", value: ".name" }] }, description: "姓名", state: "visible", timeout_ms: 1500 } },
      { action: "element.find_all", trace_id: "diagnostics", request_id: "round-1", started_at: Date.now() });
  } };
}

/** timeoutError 构造可能在原始消息中包含个人资料的读取超时。 */
function timeoutError() {
  const error = new Error("私密姓名 私密简历正文 private-token");
  error.name = "TimeoutError";
  return error;
}

test("姓名读取超时保留卡片并记录阶段、耗时和缺失序号，不记录个人资料", async () => {
  const { run, logs } = harness([{ readError: timeoutError() }, { name: "" }, {}]);
  const items = await run();
  assert.equal(items.length, 3);
  assert.equal(items[0].fields.name, "");
  const failure = logs.find(log => log.step === "read_field:name" && log.status === "failed");
  assert.equal(failure.fields.index, 0);
  assert.equal(failure.fields.phase, "read");
  assert.equal(failure.fields.error_code, "READ_TIMEOUT");
  assert.equal(failure.fields.effective_locator_timeout_ms, 0);
  assert.deepEqual(logs.find(log => log.status === "summary").fields.missing_name_indices, [0, 1]);
  assert.doesNotMatch(JSON.stringify(logs), /私密姓名|私密简历|private-token/);
});

test("姓名首次读取超时后逐项重读，恢复姓名且不重新读取整批列表", async () => {
  const { run, logs } = harness([{ readErrorOnce: timeoutError() }, {}, {}]);
  const items = await run();
  assert.equal(items.length, 3);
  assert.equal(items[0].fields.name, "私密姓名");
  assert.equal(logs.find(log => log.status === "retry_read").fields.index, 0);
  assert.deepEqual(logs.find(log => log.status === "summary").fields.missing_name_indices, []);
});

test("姓名节点存在但隐藏时保留实际错误码和匹配数量", async () => {
  const error = new WorkerError({ code: "ELEMENT_NOT_VISIBLE", message: "私密姓名", action: "element.find_all", step: "read_field:name", trace_id: "test", details: {
    attempts: [{ level: "relative_target", matches: 1, selected_index: 0, selector_value: "私密姓名" }], original_error: "私密简历正文",
  } });
  const { run, logs } = harness([{ locateError: error }]);
  await run();
  const failure = logs.find(log => log.status === "failed");
  assert.equal(failure.fields.phase, "locate");
  assert.equal(failure.fields.visible, false);
  assert.equal(failure.fields.error_code, "ELEMENT_NOT_VISIBLE");
  assert.equal(failure.fields.match_attempts[0].matches, 1);
  assert.doesNotMatch(JSON.stringify(logs), /私密姓名|私密简历/);
});

test("正文偶发超时或变化时逐项重读，恢复卡片而不跳过", async () => {
  const { run, logs } = harness([{ beforeError: timeoutError() }, { afterError: timeoutError() }, { changed: true }]);
  assert.equal((await run()).length, 3);
  const failed = logs.filter(log => log.status === "read_failed");
  assert.deepEqual(failed.map(log => [log.fields.index, log.fields.phase]), [[0, "before"], [1, "after"]]);
  assert.deepEqual(logs.filter(log => log.status === "changed").map(log => log.fields.index), [2]);
  assert.deepEqual(logs.find(log => log.status === "summary").fields.dropped_indices, []);
});

test("卡片正文连续读取失败时明确报错，不能把这张卡片静默跳过", async () => {
  const page = { url: () => "https://example.com/list" };
  const action = new FindAction(
    { requirePage: async () => page, elements: { remember: () => "ref" } },
    { resolveAll: async () => [{ list_index: 7, locator: { innerText: async () => { throw timeoutError(); } } }] },
    { info() {}, warn() {}, failure() {} },
  );
  await assert.rejects(action.all({ description: "列表" }, 1, {},
    { action: "element.find_all", trace_id: "unreadable", started_at: Date.now() }),
  error => error.code === "ACTION_TIMEOUT" && error.details.unreadable_indices[0] === 7);
});

test("可见性探测超时会保留类别，而不是只剩节点不可见", async () => {
  const locator = { async count() { return 1; }, nth() { return locator; }, async isVisible() { throw timeoutError(); } };
  await assert.rejects(new LocatorPrimitive().resolveRelative(
    { url: () => "https://example.com/list" }, { locator() { return locator; } },
    { target: { selectors: [{ type: "css", value: ".name" }] }, description: "姓名", state: "visible", timeout_ms: 0 },
    { action: "element.find_all", trace_id: "visibility", step: "read_field:name", require_unique: false },
  ), error => error.code === "ELEMENT_NOT_VISIBLE" && error.details.visibility_error_type === "TimeoutError");
});
