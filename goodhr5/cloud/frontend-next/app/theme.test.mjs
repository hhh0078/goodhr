/** 本文件验证用户主题默认绿色、账号保存键隔离和各配色保留业务状态色。 */

import assert from "node:assert/strict";
import test from "node:test";
import {
  createGoodHRTheme,
  createGoodHRAdminTheme,
  createGoodHRVariables,
  normalizeThemePreset,
  themePreferenceKey,
} from "./theme.ts";

test("未设置和不合法的用户主题都使用默认松绿色", () => {
  for (const value of [undefined, null, "", "free", "plus", "max", "unknown", {}, true]) {
    assert.equal(normalizeThemePreset(value), "green");
  }
  assert.equal(normalizeThemePreset("ink"), "ink");
  assert.equal(normalizeThemePreset("gold"), "gold");
  assert.equal(createGoodHRTheme().token.colorPrimary, "#159a62");
  assert.equal(createGoodHRTheme("invalid").token.colorPrimary, "#159a62");
});

test("用户可选深墨黑和黑金色，正文和全局浮层变量使用一致的浅色配色", () => {
  const inkTheme = createGoodHRTheme("ink");
  const goldTheme = createGoodHRTheme("gold");

  assert.equal(inkTheme.token.colorPrimary, "#242424");
  assert.equal(inkTheme.token.colorBgContainer, "#ffffff");
  assert.equal(goldTheme.token.colorPrimary, "#8a6518");
  assert.equal(goldTheme.token.colorBgContainer, "#ffffff");
  assert.match(createGoodHRVariables("ink"), /--goodhr-primary-soft:#f1f1ef/);
  assert.match(createGoodHRVariables("gold"), /--goodhr-primary-soft:#f8f3e6/);
});

test("用户主题不会覆盖运行成功等业务状态色", () => {
  for (const preset of ["green", "ink", "gold"]) {
    const theme = createGoodHRTheme(preset);
    assert.equal(theme.token.colorSuccess, "#238653");
  }
});

test("主题按规范化账号隔离，匿名状态没有可写保存键", () => {
  assert.equal(themePreferenceKey(""), "");
  assert.equal(themePreferenceKey("  "), "");
  assert.equal(themePreferenceKey(" HR@Example.invalid "), themePreferenceKey("hr@example.invalid"));
  assert.notEqual(themePreferenceKey("first@example.invalid"), themePreferenceKey("second@example.invalid"));
});

test("创建后台紧凑主题不会改变公开页面尺寸，三种账号主题都保留业务状态色", () => {
  for (const preset of ["green", "ink", "gold"]) {
    const publicBefore = createGoodHRTheme(preset);
    const snapshot = structuredClone(publicBefore);
    const admin = createGoodHRAdminTheme(preset);
    assert.notStrictEqual(admin.token, publicBefore.token);
    assert.equal(admin.token.controlHeight, 32);
    assert.equal(admin.token.fontSize, 13);
    assert.equal(admin.token.colorPrimary, publicBefore.token.colorPrimary);
    for (const key of ["colorSuccess", "colorWarning", "colorError"]) {
      assert.equal(admin.token[key], publicBefore.token[key]);
    }
    assert.deepEqual(publicBefore, snapshot);
    const publicAfter = createGoodHRTheme(preset);
    assert.equal(publicAfter.token.controlHeight, 38);
    assert.equal(publicAfter.token.controlHeightLG, 44);
    assert.equal(publicAfter.token.controlHeightSM, 30);
    assert.deepEqual(publicAfter, snapshot);
  }
});
