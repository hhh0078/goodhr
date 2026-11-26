/** 文件作用：验证日志展示去重保留完整错误和不同原因，兼容空值及旧正文。 */
import assert from "node:assert/strict";
import test from "node:test";
import { diagnosticMessages } from "./diagnostic-messages.ts";

test("重复原因只显示一次，固定前后文完整保留", () => {
  const reason = "读取候选人列表超时，请稍后重试";
  const wrapped = `task_id=synthetic flow=platform step=find_candidates status=failed duration_ms=43051 error="${reason}"`;
  assert.deepEqual(diagnosticMessages(reason, reason), [reason]);
  assert.deepEqual(diagnosticMessages(` ${reason} `, `\n${reason}`), [reason]);
  assert.deepEqual(diagnosticMessages(wrapped, reason), [wrapped]);
  assert.deepEqual(diagnosticMessages(reason, wrapped), [reason]);
});

test("Go 编码的引号和换行只保留具体原因，未知格式和独立说明不隐藏", () => {
  const reason = '候选人读取超时："resume"\n请重新开始任务';
  const raw = `task_id=synthetic flow=platform step=find_candidates status=failed duration_ms=43051 error=${JSON.stringify(reason)}`;
  assert.deepEqual(diagnosticMessages(reason, raw), [reason]);
  assert.deepEqual(diagnosticMessages("", raw), [raw]);
  const unknown = raw.replace("duration_ms=43051", "duration_ms=unknown");
  assert.deepEqual(diagnosticMessages(reason, unknown), [reason, unknown]);
  const incomplete = raw.slice(0, -1);
  assert.deepEqual(diagnosticMessages(reason, incomplete), [reason, incomplete]);
  const missingField = raw.replace(" flow=platform", "");
  assert.deepEqual(diagnosticMessages(reason, missingField), [reason, missingField]);
  const withExplanation = `${raw}；另有云端状态保存失败`;
  assert.deepEqual(diagnosticMessages(reason, withExplanation), [reason, withExplanation]);
  assert.deepEqual(diagnosticMessages(reason, "云端状态保存失败"), [reason, "云端状态保存失败"]);
});

test("不同原因和补充信息不会因展示去重丢失", () => {
  assert.deepEqual(diagnosticMessages("读取候选人列表超时", "云端状态保存失败"), ["读取候选人列表超时", "云端状态保存失败"]);
  const complete = "读取候选人列表超时；云端状态保存失败";
  assert.deepEqual(diagnosticMessages("读取候选人列表超时", complete), [complete]);
});

test("旧日志、空白和异常字段安全回退", () => {
  assert.deepEqual(diagnosticMessages("旧版普通日志", undefined), ["旧版普通日志"]);
  assert.deepEqual(diagnosticMessages(null, "完整错误"), ["完整错误"]);
  assert.deepEqual(diagnosticMessages({}, []), []);
});
