/** 文件作用说明：在合成接口下验证固定详情策略、旧选择框隐藏和运行组件不展示OCR，覆盖桌面及手机。 */
import { test, expect } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const output = resolve(process.env.UI_TEST_SCREENSHOT_DIR || "../../../output/detail-mode-policy");
test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "detail-mode-synthetic-token" }] }] } });
// 本机开发预览首次编译两个路由占用时间；单个界面断言仍沿用10秒上限。
test.setTimeout(90_000);

for (const width of [1366, 390]) {
  test(`固定详情策略 ${width}px 不展示OCR选择，保存按平台归一`, async ({ page }) => {
    const state = await mockUniformAdmin(page);
    // 只初始化合成控制台的短期连接缓存，确保路由跳转不依赖本机真实Agent或浏览器设备权限。
    await page.addInitScript(port => {
      localStorage.setItem("goodhr5_local_agent_detect_cache", JSON.stringify({ baseURL: `http://127.0.0.1:${port}`, checkedAt: Date.now() }));
    }, process.env.UI_UNIFORM_AGENT_PORT || "43129");
    const saved: Array<{ platform_id?: string; common_config?: { detail_mode?: string } }> = [];
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    await page.route(/\/api\/platforms(?:\/config\/)?$/, route => route.fulfill({ headers, json: { platforms: ["boss", "zhaopin", "liepin", "hliepin", "51job"].map(id => ({ id, platform_id: id, enabled: true, is_open: true, open: true })) } }));
    await page.route(/\/api\/positions$/, async route => {
      if (route.request().method() !== "POST") return route.fallback();
      const body = route.request().postDataJSON();
      saved.push(body);
      await route.fulfill({ headers, json: { position: { ...body, id: `synthetic-${saved.length}`, is_open: true } } });
    });
    await page.setViewportSize({ width, height: 960 });
    await page.goto("/admin/positions");
    for (const platform of ["boss", "zhaopin"]) {
      await page.getByRole("button", { name: "新建岗位", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: "新建岗位模板", exact: true });
      await expect(dialog).toBeVisible();
      await expect(dialog.locator('[data-onboarding="position-detail-mode"]')).toHaveCount(0);
      await expect(dialog.getByRole("radio", { name: /OCR/ })).toHaveCount(0);
      if (platform === "zhaopin") await dialog.getByRole("radio", { name: /^智联招聘/ }).check();
      await dialog.locator('[data-onboarding="position-name"]').fill("合成测试岗位");
      await dialog.locator('[data-onboarding="position-requirement"]').fill("有招聘工作经验。仅用于合成界面测试。");
      const box = await dialog.boundingBox();
      expect(box && box.width <= width).toBeTruthy();
      mkdirSync(output, { recursive: true });
      await page.screenshot({ path: resolve(output, `fixed-detail-${platform}-${width}.png`), fullPage: true });
      await dialog.getByRole("button", { name: "创建岗位", exact: true }).click();
      await expect(dialog).toHaveCount(0);
      expect(saved.at(-1)?.platform_id).toBe(platform);
      expect(saved.at(-1)?.common_config?.detail_mode).toBe(platform === "boss" ? "ai" : "dom");
    }
    await page.goto("/admin/agent-download");
    await expect(page.getByText("Node 运行环境", { exact: true })).toBeVisible();
    await expect(page.getByText("OCR 组件", { exact: true })).toHaveCount(0);
    await page.screenshot({ path: resolve(output, `runtime-no-ocr-${width}.png`), fullPage: true });
    expect(state.errors).toEqual([]);
    expect(state.writes).toEqual([]);
  });
}
