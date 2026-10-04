// 本文件验证前端只清理确定失效的原会话，不因临时故障或旧响应退出新登录。
import assert from "node:assert/strict";
import test from "node:test";
import { shouldClearSession } from "./auth-session.ts";

test("临时故障和未分类错误保留凭证", () => {
  assert.equal(shouldClearSession(503, "AUTH_UNAVAILABLE", "old", "old"), false);
  assert.equal(shouldClearSession(401, "", "old", "old"), false);
});
test("只有确定失效的同一个凭证会被清理", () => {
  for (const code of ["SESSION_EXPIRED", "SESSION_REQUIRED"]) {
    assert.equal(shouldClearSession(401, code, "old", "old"), true);
    assert.equal(shouldClearSession(401, code, "old", "new"), false);
    assert.equal(shouldClearSession(401, code, "", ""), false);
  }
});
