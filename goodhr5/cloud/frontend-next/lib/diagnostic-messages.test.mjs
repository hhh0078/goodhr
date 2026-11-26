/** 文件作用：验证日志展示去重保留完整错误和不同原因，兼容空值及旧正文。 */
import assert from "node:assert/strict";
import test from "node:test";
import { diagnosticMessages } from "./diagnostic-messages.ts";

test("重复原因只显示一次，固定前后文完整保留", () => {
  const reason = "读取候选人列表超时，请稍后重试";
  const wrapped = `task_id=synthetic flow=platform step=find_candidates error=${reason}`;
  assert.deepEqual(diagnosticMessages(reason, reason), [reason]);
  assert.deepEqual(diagnosticMessages(` ${reason} `, `\n${reason}`), [reason]);
  assert.deepEqual(diagnosticMessages(wrapped, reason), [wrapped]);
  assert.deepEqual(diagnosticMessages(reason, wrapped), [wrapped]);
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
