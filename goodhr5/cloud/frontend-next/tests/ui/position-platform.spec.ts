/** 文件作用说明：用隔离接口验证岗位编辑能更换招聘平台、保留岗位编号与历史统计，并检查桌面和手机展示。 */
import { test, expect, type Page } from "@playwright/test";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";
import { createOnboardingState, onboardingStorageKey } from "../../lib/onboarding-state";

test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "platform-synthetic-token" }] }] } });

/** platformFixture 在内存中保存合成岗位；所有读写均被拦截，不启动真实岗位或连接招聘网站。 */
async function platformFixture(page: Page) {
  const base = await mockUniformAdmin(page, true);
  const state = {
    position: { id: "platform-position", name: "合成高级软件工程师", platform_id: "boss", creator_email: "uniform@example.invalid", is_current_user: true, is_open: true, status: "stopped", scanned_count: 7, today_greeted_count: 2, skipped_count: 3, keywords: ["Java"], common_config: { mode_default: "keyword", detail_mode: "ocr" } } as Record<string, any>,
    saves: [] as Record<string, any>[],
  };
  const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
  await page.route(/\/api\/platforms\/config\//, (route) => route.fulfill({ headers, json: { configs: ["boss", "zhaopin", "51job", "liepin"].map((id) => ({ config_key: `platform.${id}`, config_value: { id, open: id !== "liepin" } })) } }));
  await page.route(/\/api\/positions(?:\?|$)/, async (route) => {
    const method = route.request().method();
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (method === "POST") {
      const payload = route.request().postDataJSON();
      state.saves.push(payload);
      state.position = { ...state.position, ...payload };
      return route.fulfill({ headers, json: { ok: true, position: state.position } });
    }
    return route.fulfill({ headers, json: { positions: [state.position] } });
  });
  return { state, base };
}

test.describe("教程中编辑已有岗位", () => {
  test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [
    { name: "goodhr5_access_token", value: "platform-synthetic-token" },
    { name: onboardingStorageKey("uniform-user"), value: JSON.stringify({ ...createOnboardingState(), status: "active", step: "position-platform", positionId: "platform-position", platformId: "boss", reviewPosition: true, replaying: true, environmentReady: true, filterMode: "keyword", detailMode: "ocr", completedFields: ["name", "filter-mode", "detail-mode", "keywords"] }) },
  ] }] } });

  test("更换平台的草稿刷新后仍保留，保存继续使用原岗位", async ({ page }) => {
    const { state, base } = await platformFixture(page);
    await page.goto("/admin/positions");
    let dialog = page.getByRole("dialog", { name: "编辑岗位模板", exact: true });
    await dialog.getByRole("radio", { name: /前程无忧/ }).click();
    await expect(dialog.getByRole("radio", { name: /前程无忧/ })).toBeChecked();
    await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-name");
    await page.reload();
    dialog = page.getByRole("dialog", { name: "编辑岗位模板", exact: true });
    await expect(dialog.getByRole("radio", { name: /前程无忧/ })).toBeChecked();
    await dialog.getByRole("textbox", { name: "岗位名称", exact: true }).fill("合成高级软件工程师");
    await page.keyboard.press("Tab");
    await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-detail-mode");
    await dialog.getByRole("radio", { name: /^页面文字识别/ }).click();
    await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-save");
    await dialog.getByRole("button", { name: "保存修改", exact: true }).click();
    await expect(dialog).toBeHidden();
    expect(state.saves).toHaveLength(1);
    expect(state.saves[0]).toMatchObject({ id: "platform-position", platform_id: "51job", common_config: { detail_mode: "dom" } });
    expect(base.errors).toEqual([]);
    expect(base.writes).toEqual([]);
  });
});

for (const width of [1440, 390]) {
  test(`${width}px 编辑同一岗位可换前程无忧并自动使用DOM，切回BOSS恢复OCR`, async ({ page }) => {
    const { state, base } = await platformFixture(page);
    await page.setViewportSize({ width, height: 960 });
    await page.goto("/admin/positions");
    const card = page.locator('.ant-card[data-position-id="platform-position"]');
    await card.getByRole("button", { name: "编辑", exact: true }).click();
    let dialog = page.getByRole("dialog", { name: "编辑岗位模板", exact: true });
    await expect(dialog.getByRole("radio", { name: /智联招聘/ })).toBeEnabled();
    await expect(dialog.getByRole("radio", { name: /猎聘企业端/ })).toBeDisabled();
    await dialog.getByRole("radio", { name: /前程无忧/ }).click();
    await expect(dialog.getByRole("radio", { name: /前程无忧/ })).toBeChecked();
    await dialog.getByRole("button", { name: "保存修改", exact: true }).click();
    await expect(dialog).toBeHidden();
    expect(state.saves[0]).toMatchObject({ id: "platform-position", platform_id: "51job", common_config: { detail_mode: "dom" } });
    expect(state.position).toMatchObject({ scanned_count: 7, today_greeted_count: 2, skipped_count: 3 });
    await expect(card).toContainText("前程无忧");
    await card.getByRole("button", { name: "编辑", exact: true }).click();
    dialog = page.getByRole("dialog", { name: "编辑岗位模板", exact: true });
    await expect(dialog.getByRole("radio", { name: /前程无忧/ })).toBeChecked();
    // 等保存提示收起和弹框入场完成，截图需包含真正可见的表单。
    await expect(page.getByText("岗位模板已更新", { exact: true })).toBeHidden();
    await expect(dialog).toHaveCSS("opacity", "1");
    await expect(dialog.getByRole("textbox", { name: "岗位名称", exact: true })).toBeVisible();
    await expect(dialog.locator("label.goodhr-choice").filter({ hasText: "前程无忧" })).toContainText("请先升级本地程序");
    const bounds = (await dialog.boundingBox())!;
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
    expect(bounds.y).toBeGreaterThanOrEqual(0);
    const saveBounds = (await dialog.getByRole("button", { name: "保存修改", exact: true }).boundingBox())!;
    expect(saveBounds.y).toBeGreaterThanOrEqual(0);
    expect(saveBounds.y + saveBounds.height).toBeLessThanOrEqual(960);
    await page.screenshot({ path: test.info().outputPath(`platform-edit-${width}.png`), fullPage: true, animations: "disabled" });
    await dialog.getByRole("radio", { name: /^Boss直聘/i }).click();
    await dialog.getByRole("button", { name: "保存修改", exact: true }).click();
    await expect(dialog).toBeHidden();
    expect(state.saves[1]).toMatchObject({ id: "platform-position", platform_id: "boss", common_config: { detail_mode: "ocr" } });
    expect(state.saves).toHaveLength(2);
    expect(base.writes).toEqual([]);
    expect(base.errors).toEqual([]);
    expect(base.calls.filter((call) => /\/(?:run|start|stop)$/.test(call.path) || call.path === "/api/v1/page/open")).toEqual([]);
  });
}

for (const width of [1440, 390]) {
  /** 平台账号入口需提醒升级客户端，选择平台本身不得创建账号或启动浏览器。 */
  test(`${width}px 前程平台账号提醒升级本地程序`, async ({ page }) => {
    const state = await mockUniformAdmin(page, true);
    await page.setViewportSize({ width, height: 960 });
    await page.goto("/admin/accounts");
    await page.getByRole("button", { name: "新增账号", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "新增平台账号", exact: true });
    await dialog.getByRole("radio", { name: /前程无忧/ }).click();
    await expect(dialog.getByRole("radio", { name: /前程无忧/ })).toBeChecked();
    await expect(dialog.locator("label.goodhr-choice").filter({ hasText: "前程无忧" })).toContainText("请先升级本地程序");
    await expect(dialog).toHaveCSS("opacity", "1");
    await expect(dialog.getByRole("textbox", { name: "账号名称", exact: true })).toBeVisible();
    const bounds = (await dialog.boundingBox())!;
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
    await page.screenshot({ path: test.info().outputPath(`platform-account-${width}.png`), fullPage: true, animations: "disabled" });
    expect(state.writes).toEqual([]);
    expect(state.errors).toEqual([]);
    expect(state.calls.filter((call) => call.path === "/api/v1/page/open")).toEqual([]);
  });
}
