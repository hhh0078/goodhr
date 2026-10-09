/** 本文件验证后台主题会按照有效会员类型安全匹配。 */

import assert from "node:assert/strict";
import test from "node:test";
import {
  createGoodHRTheme,
  createGoodHRVariables,
  resolveMembershipTheme,
} from "./theme.ts";

test("会员主题只对有效 Plus 和 Max 生效", () => {
  assert.equal(resolveMembershipTheme(false, "max"), "free");
  assert.equal(resolveMembershipTheme(true, "plus"), "plus");
  assert.equal(resolveMembershipTheme(true, "MAX"), "max");
  assert.equal(resolveMembershipTheme(true, "unknown"), "free");
  assert.equal(resolveMembershipTheme(true, null), "free");
});

test("Plus 和 Max 保持浅色页面并使用专属强调色", () => {
  const plusTheme = createGoodHRTheme("plus");
  const maxTheme = createGoodHRTheme("max");

  assert.equal(plusTheme.token.colorPrimary, "#242424");
  assert.equal(plusTheme.token.colorBgContainer, "#ffffff");
  assert.equal(maxTheme.token.colorPrimary, "#8a6518");
  assert.equal(maxTheme.token.colorBgContainer, "#ffffff");
  assert.match(createGoodHRVariables("plus"), /--goodhr-primary-soft:#f1f1ef/);
  assert.match(createGoodHRVariables("max"), /--goodhr-primary-soft:#f8f3e6/);
});

test("会员主题不会覆盖运行成功等业务状态色", () => {
  for (const memberType of ["free", "plus", "max"]) {
    const theme = createGoodHRTheme(memberType);
    assert.equal(theme.token.colorSuccess, "#238653");
  }
});
