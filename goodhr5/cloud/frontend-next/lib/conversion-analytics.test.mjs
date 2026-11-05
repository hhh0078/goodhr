/** 文件作用：验证统计的匿名分类、私密页面排除、后台到达去重及故障隔离。 */
import test from "node:test";
import assert from "node:assert/strict";
import { canRecordClarity, isPrivateAnalyticsPath, trackConversion, classifyAuthFailure, markPendingAdminArrival, trackAdminArrival } from "./conversion-analytics.ts";

test("回放只覆盖官网和登录，不覆盖后台、私人分享或未知路径", () => {
  for (const path of ["/", "/login", "/download", "/articles/hello"]) assert.equal(canRecordClarity(path), true);
  for (const path of ["/admin", "/admin/resumes", "/resumes/share/secret", "/diagnostics/token", "/interview-invitation/token", "/unknown"]) assert.equal(canRecordClarity(path), false);
  assert.equal(isPrivateAnalyticsPath("/diagnostics/token"), true);
  assert.equal(isPrivateAnalyticsPath("/diagnostics"), true);
});

test("失败原因是固定分类，不上传包含邮箱和验证码的错误原文", () => {
  const messages = ["连接云端失败 test@example.invalid", "验证码错误 1234", "域名不在白名单内 test@example.invalid", "未知错误 user-secret"];
  assert.deepEqual(messages.map((value) => classifyAuthFailure(new Error(value))), ["network", "credentials", "email_domain", "other"]);
});

test("私密页完全不入队；后台不调用Clarity；统计异常不影响业务", () => {
  const calls = [];
  globalThis.window = { location: { pathname: "/login" }, clarity: (...args) => calls.push(args) };
  trackConversion("login_failed", { reason: "credentials", method: "code" });
  assert.equal(calls.at(-1)[1], "login_failed");
  window.location.pathname = "/admin/positions";
  trackConversion("admin_entered", { flow: "new_account" });
  assert.equal(calls.length, 3);
  assert.equal(window._hmt.at(-1)[2], "admin_entered");
  window.location.pathname = "/resumes/share/private-token";
  const count = window._hmt.length;
  trackConversion("login_success");
  assert.equal(window._hmt.length, count);
  window.location.pathname = "/login";
  window.clarity = () => { throw new Error("blocked"); };
  assert.doesNotThrow(() => trackConversion("login_success"));
  delete globalThis.window;
});

test("后台真实到达只记录一次，忽略过期、损坏和未来的记录", () => {
  const values = new Map();
  globalThis.sessionStorage = { setItem: (key, value) => values.set(key, value), getItem: (key) => values.get(key), removeItem: (key) => values.delete(key) };
  globalThis.window = { location: { pathname: "/admin/positions" } };
  markPendingAdminArrival("new_account");
  trackAdminArrival();
  trackAdminArrival();
  assert.equal(window._hmt.length, 1);
  for (const value of ["broken", JSON.stringify({ flow: "new_account", at: 0 }), JSON.stringify({ flow: "new_account", at: Date.now() + 60_000 })]) {
    values.set("goodhr.auth.pending-arrival", value);
    assert.doesNotThrow(trackAdminArrival);
  }
  assert.equal(window._hmt.length, 1);
  delete globalThis.sessionStorage;
  delete globalThis.window;
});
