/** 本文件验证后台主题会按照有效会员类型安全匹配。 */

import assert from "node:assert/strict";
import test from "node:test";
import { createGoodHRTheme, resolveMembershipTheme } from "./theme.ts";

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

  assert.equal(plusTheme.palette.mode, "light");
  assert.equal(plusTheme.palette.primary.main, "#242424");
  assert.equal(plusTheme.palette.background.paper, "#ffffff");
  assert.equal(maxTheme.palette.primary.main, "#8a6518");
  assert.equal(maxTheme.palette.secondary.main, "#1b1812");
  assert.equal(maxTheme.palette.background.paper, "#ffffff");
});
