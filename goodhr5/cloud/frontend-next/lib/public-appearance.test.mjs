/** 本文件验证官网配色偏好、公开页面边界和文字对比度，不改变后台主题。 */
import test from "node:test";
import assert from "node:assert/strict";
import { PUBLIC_APPEARANCES, normalizePublicAppearance, isPublicAppearancePath, createPublicAppearanceVariables, isPublicNavigationActive } from "./public-appearance.ts";

test("导航选中覆盖专题和文章，同时避免首页或相似地址误选", () => {
  for (const [path, href] of [["/", "/"], ["/pricing/", "/pricing"], ["/articles/a", "/articles"], ["/platforms/51job", "/platforms"], ["/hr-tools", "/features"]]) assert.equal(isPublicNavigationActive(path, href), true);
  for (const [path, href] of [[null, "/"], ["/features", "/"], ["/pricing-extra", "/pricing"], ["/articles-other/a", "/articles"], ["/platforms/boss", "/features"]]) assert.equal(isPublicNavigationActive(path, href), false);
});

/** luminance 按sRGB计算亮度，用于检查按钮和正文在两套背景上的可读性。 */
function luminance(hex) {
  const c = hex.slice(1).match(/../g).map((value) => parseInt(value, 16) / 255).map((v) => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4);
  return c[0] * .2126 + c[1] * .7152 + c[2] * .0722;
}

test("损坏的官网偏好回退绿色，莓红不会写入后台主题枚举", () => {
  for (const value of [undefined, null, "ink", "gold", "", {}, true]) assert.equal(normalizePublicAppearance(value), "green");
  assert.equal(normalizePublicAppearance("rose"), "rose");
});

test("只对官网与登录页应用双配色，后台及私人页面不命中", () => {
  for (const path of ["/", "/features", "/download", "/pricing", "/articles/a", "/platforms/boss", "/platforms/51job", "/login"]) assert.equal(isPublicAppearancePath(path), true, path);
  for (const path of [null, "/admin", "/admin/resumes", "/resumes/share/a", "/interview-invitation/a", "/diagnostics/a", "/platforms/unknown", "/not-found"]) assert.equal(isPublicAppearancePath(path), false, path);
});

test("两套配色保留指定主色，正文、辅助文字和按钮均达到4.5对比度", () => {
  for (const [key, p] of Object.entries(PUBLIC_APPEARANCES)) {
    assert.ok((luminance(p.paper) + .05) / (luminance(p.ink) + .05) >= 4.5, key);
    assert.ok((luminance(p.paper) + .05) / (luminance(p.muted) + .05) >= 4.5, key);
    assert.ok(1.05 / (luminance(p.action) + .05) >= 4.5, key);
    assert.match(createPublicAppearanceVariables(key), /\.public-theme-root/);
    assert.ok(!createPublicAppearanceVariables(key).includes(":root{"));
  }
  assert.equal(PUBLIC_APPEARANCES.green.brand, "#349b26");
  assert.equal(PUBLIC_APPEARANCES.green.accent, "#ffcb7a");
  assert.equal(PUBLIC_APPEARANCES.rose.brand, "#ff2742");
  assert.equal(PUBLIC_APPEARANCES.rose.accent, "#fff6f0");
});
