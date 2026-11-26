/** 文件作用：在独立Chrome和全拦截合成接口中回归备用AI表单、密钥保留、并发保护与手机布局，不访问真实配置或模型。 */
import { expect, test, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { expectUniformNetwork, mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const artifactDirectory = resolve(process.env.UI_AI_FALLBACK_ARTIFACT_DIR || "../../../output/playwright/ai-fallback");
const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "ai-fallback-synthetic-token" }] }] } });

/** mockFallback 只在内存处理备用配置接口，保留合成请求用于核对，不发起任何真实AI或保存操作。 */
async function mockFallback(page: Page, mode: "success" | "read-failed" | "incomplete" | "conflict" | "new-key" | "user" | "app-conflict" = "success", entry = "/admin/ai-fallback") {
  const base = await mockUniformAdmin(page);
  const saves: Record<string, unknown>[] = [];
  const appSaves: Record<string, unknown>[] = [];
  let appReads = 0;
  let app = { registration_enabled: true, untouched: "public-before", builtin_ai: { model: "primary-model", api_key: "synthetic-primary-key", fallback: { enabled: false, model: "synthetic-backup", api_key: "synthetic-old-key" } } };
  let appRaw = JSON.stringify(app);
  let reads = 0;
  let releaseSave = () => {};
  page.on("close", () => releaseSave());
  let snapshot = { ok: true, config: { enabled: false, base_url: "https://backup.example.invalid/v1/chat/completions", api_key_set: mode !== "new-key", model: "synthetic-backup" }, revision: "revision-before" };
  if (mode === "user") await page.route("**/api/auth/me", route => route.fulfill({ headers, json: { user: { id: "fallback-user", email: "user@example.invalid", role: "user" } } }));
  await page.route("**/api/admin/system/configs/**", async route => {
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (route.request().method() === "GET") {
      appReads++;
      return route.fulfill({ headers, json: { configs: [
        { config_key: "system.onboarding_config", config_value: "{}", enabled: true },
        { config_key: "system.app_config", description: "合成公共配置", config_value: appRaw, enabled: true },
      ] } });
    }
    const body = route.request().postDataJSON();
    appSaves.push(body);
    if (mode === "app-conflict" || body.expected_config_value !== appRaw) return route.fulfill({ status: 409, headers, json: { error: "公共配置已有更新，请重新打开后再保存" } });
    appRaw = body.config_value;
    app = JSON.parse(appRaw);
    return route.fulfill({ headers, json: { ok: true } });
  });
  await page.route("**/api/admin/system/ai-fallback", async route => {
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (route.request().method() === "GET") {
      reads++;
      if (mode === "read-failed") return route.fulfill({ status: 503, headers, json: { error: "合成读取失败，请稍后重试" } });
      if (mode === "incomplete") return route.fulfill({ headers, json: { ok: true, config: {} } });
    } else if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON();
      saves.push(body);
      if (mode === "conflict") return route.fulfill({ status: 409, headers, json: { error: "备用AI配置已经变化，请重新打开页面后再保存" } });
      if (mode === "success") await new Promise<void>(done => { releaseSave = done; });
      app.builtin_ai.fallback = { enabled: body.enabled, model: body.model, api_key: body.api_key || app.builtin_ai.fallback.api_key };
      appRaw = JSON.stringify(app);
      snapshot = { ok: true, config: { enabled: body.enabled, base_url: body.base_url, model: body.model, api_key_set: snapshot.config.api_key_set || Boolean(body.api_key) }, revision: "revision-after" };
    }
    return route.fulfill({ headers, json: snapshot });
  });
  await page.goto(`${uniformOrigin}${entry}`, { waitUntil: "domcontentloaded" });
  return { base, saves, appSaves, releaseSave: () => releaseSave(), get reads() { return reads; }, get appReads() { return appReads; }, get appRaw() { return appRaw; } };
}

/** expectFitsViewport 通过真实渲染宽度核对页面和输入控件不横向溢出。 */
async function expectFitsViewport(page: Page) {
  const viewport = page.viewportSize();
  const body = await page.locator("body").boundingBox();
  expect(body!.width).toBeLessThanOrEqual(viewport!.width + 1);
  for (const label of ["API 地址", "API 密钥", "模型名称"]) {
    const input = page.getByLabel(label, { exact: true });
    const box = await (label === "API 密钥" ? input.locator("xpath=ancestor::*[contains(@class,'ant-input-affix-wrapper')][1]") : input).boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport!.width + 1);
    expect(box!.height).toBeCloseTo(32, 0);
  }
}

for (const width of [1366, 390]) {
  test(`备用AI ${width}px：菜单、留空保留密钥、版本更新及真实表单保存`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await mockFallback(page);
    await expect(page).toHaveURL(/\/admin\/system-config\?key=system.app_config&section=ai-fallback$/);
    await expect(page.getByRole("heading", { name: "公共系统配置", exact: true })).toBeVisible();
    await expect(page.getByRole("tab", { name: "备用AI", exact: true })).toHaveAttribute("aria-selected", "true");
    await expect(page.getByLabel("API 地址", { exact: true })).toHaveValue("https://backup.example.invalid/v1/chat/completions");
    await expect(page.getByLabel("API 密钥", { exact: true })).toHaveValue("");
    await expect(page.getByText("已配置。留空会保留原密钥，填写后才会替换。", { exact: true })).toBeVisible();
    const save = page.getByRole("button", { name: "保存当前配置", exact: true });
    await expect(save).toBeDisabled();
    await expect(save).toHaveCount(1);
    if (width < 600) await page.getByRole("button", { name: "打开菜单", exact: true }).click();
    await expect(page.getByRole("menuitem", { name: "备用AI", exact: true })).toHaveCount(0);
    if (width < 600) await page.locator(".ant-drawer-close").click();
    await page.getByRole("switch", { name: "启用备用AI", exact: true }).click();
    await expect(page.getByRole("switch", { name: "启用备用AI", exact: true })).toBeChecked();
    await page.getByLabel("模型名称", { exact: true }).fill("changed-synthetic-model");
    mkdirSync(artifactDirectory, { recursive: true });
    await page.screenshot({ path: resolve(artifactDirectory, `ai-fallback-${width}.png`), fullPage: true, animations: "disabled" });
    await save.click();
    await expect.poll(() => fixture.saves.length).toBe(1);
    await expect(save).toHaveClass(/ant-btn-loading/);
    fixture.releaseSave();
    await expect(page.getByText("备用AI已保存", { exact: true })).toBeVisible();
    expect(fixture.saves).toEqual([{ enabled: true, base_url: "https://backup.example.invalid/v1/chat/completions", api_key: "", model: "changed-synthetic-model", revision: "revision-before" }]);
    await expect(page.getByLabel("API 密钥", { exact: true })).toHaveValue("");
    await page.getByLabel("模型名称", { exact: true }).fill("second-synthetic-model");
    await save.click();
    await expect.poll(() => fixture.saves.length).toBe(2);
    expect(fixture.saves[1].revision).toBe("revision-after");
    fixture.releaseSave();
    await expect(save).not.toHaveClass(/ant-btn-loading/);
    await expect.poll(() => fixture.appReads).toBeGreaterThanOrEqual(3);
    expect(JSON.parse(fixture.appRaw).builtin_ai.model).toBe("primary-model");
    expect(JSON.parse(fixture.appRaw).untouched).toBe("public-before");
    await expectFitsViewport(page);
    expectUniformNetwork(fixture.base);
  });

  test(`备用AI ${width}px：读取失败不可编辑或覆盖`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await mockFallback(page, "read-failed");
    await expect(page.getByText("合成读取失败，请稍后重试", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "保存当前配置", exact: true })).toBeDisabled();
    await expect(page.getByLabel("API 地址", { exact: true })).toBeDisabled();
    await expect(page.getByRole("link", { name: "重新打开配置", exact: true })).toBeVisible();
    expect(fixture.saves).toEqual([]);
    expect(fixture.reads).toBeGreaterThanOrEqual(1);
    expectUniformNetwork(fixture.base);
  });

  test(`备用AI ${width}px：409保留草稿并阻止继续覆盖`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await mockFallback(page, "conflict");
    await expect(page.getByLabel("模型名称", { exact: true })).toHaveValue("synthetic-backup");
    await page.getByLabel("模型名称", { exact: true }).fill("keep-unsaved-draft");
    await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
    await expect(page.getByRole("link", { name: "重新打开配置", exact: true })).toBeVisible();
    await expect(page.getByLabel("模型名称", { exact: true })).toHaveValue("keep-unsaved-draft");
    await expect(page.getByRole("button", { name: "保存当前配置", exact: true })).toBeDisabled();
    expect(fixture.saves.length).toBe(1);
    expectUniformNetwork(fixture.base);
  });

  test(`备用AI ${width}px：普通用户无菜单且不读配置`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await mockFallback(page, "user");
    await expect(page.getByText("只有超级管理员可以访问此页面", { exact: true })).toBeVisible();
    if (width < 600) await page.getByRole("button", { name: "打开菜单", exact: true }).click();
    await expect(page.getByRole("menuitem", { name: "备用AI", exact: true })).toHaveCount(0);
    expect(fixture.reads).toBe(0);
    expect(fixture.saves).toEqual([]);
    expectUniformNetwork(fixture.base);
  });
}

test("备用AI：响应不完整不能按空配置保存", async ({ page }) => {
  const fixture = await mockFallback(page, "incomplete");
  await expect(page.getByText("备用AI配置没读完整，请重新打开此页后再试", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "保存当前配置", exact: true })).toBeDisabled();
  expect(fixture.saves).toEqual([]);
  expectUniformNetwork(fixture.base);
});

test("备用AI：首次启用必须填密钥，保存后清空输入且不回显", async ({ page }) => {
  const fixture = await mockFallback(page, "new-key");
  await expect(page.getByLabel("API 地址", { exact: true })).toBeEnabled();
  await page.getByRole("switch", { name: "启用备用AI", exact: true }).click();
  await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
  await expect(page.getByText("启用前请填好备用密钥", { exact: true })).toBeVisible();
  expect(fixture.saves).toEqual([]);
  await page.getByLabel("API 密钥", { exact: true }).fill("synthetic-secret-not-real");
  await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
  await expect(page.getByText("备用AI已保存", { exact: true })).toBeVisible();
  expect(fixture.saves[0].api_key).toBe("synthetic-secret-not-real");
  await expect(page.getByLabel("API 密钥", { exact: true })).toHaveValue("");
  await expect(page.getByText("已配置。留空会保留原密钥，填写后才会替换。", { exact: true })).toBeVisible();
  expectUniformNetwork(fixture.base);
});

/** editPublicJSON 通过真实可编辑控件修改公共配置，保留完整JSON字段用于并发和覆盖回归。 */
async function editPublicJSON(page: Page, value: unknown) {
  await page.locator(".cm-content[contenteditable=true]").fill(JSON.stringify(value, null, 2));
}

for (const width of [1366, 390]) {
  test(`公共配置 ${width}px：备用保存后更新公共快照，JSON保存带完整CAS且保留主用`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await mockFallback(page);
    await expect(page.getByLabel("模型名称", { exact: true })).toHaveValue("synthetic-backup");
    await page.getByLabel("模型名称", { exact: true }).fill("fresh-fallback-model");
    await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
    await expect.poll(() => fixture.saves.length).toBe(1);
    fixture.releaseSave();
    await expect.poll(() => fixture.appReads).toBeGreaterThanOrEqual(2);
    await expect(page.getByRole("button", { name: "保存当前配置", exact: true })).not.toHaveClass(/ant-btn-loading/);
    const expected = fixture.appRaw;
    await page.getByRole("tab", { name: "公共配置", exact: true }).click();
    const edited = { ...JSON.parse(expected), untouched: "public-after" };
    await editPublicJSON(page, edited);
    await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
    await expect.poll(() => fixture.appSaves.length).toBe(1);
    expect(fixture.appSaves[0].expected_config_value).toBe(expected);
    expect(JSON.parse(fixture.appSaves[0].config_value as string)).toEqual(edited);
    expect(JSON.parse(fixture.appRaw).builtin_ai).toEqual({ model: "primary-model", api_key: "synthetic-primary-key", fallback: { enabled: false, model: "fresh-fallback-model", api_key: "synthetic-old-key" } });
    await expect(page.getByText("公共系统配置已保存", { exact: true })).toBeVisible();
    expectUniformNetwork(fixture.base);
  });

  test(`公共配置 ${width}px：切换分区确认草稿，备用撤销仍共用页头按钮`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await mockFallback(page);
    const model = page.getByLabel("模型名称", { exact: true });
    await expect(model).toHaveValue("synthetic-backup");
    await model.fill("unsaved-fallback");
    await page.getByRole("tab", { name: "公共配置", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "放弃未保存修改", exact: true });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(model).toHaveValue("unsaved-fallback");
    await page.getByRole("button", { name: "撤销修改", exact: true }).click();
    await expect(model).toHaveValue("synthetic-backup");
    await expect(page.getByRole("button", { name: "保存当前配置", exact: true })).toBeDisabled();
    await page.getByRole("tab", { name: "公共配置", exact: true }).click();
    await editPublicJSON(page, { ...JSON.parse(fixture.appRaw), untouched: "keep-public-draft" });
    await page.getByRole("tab", { name: "备用AI", exact: true }).click();
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(page.locator(".cm-content")).toContainText("keep-public-draft");
    await page.getByRole("tab", { name: "备用AI", exact: true }).click();
    await dialog.getByRole("button", { name: "确认", exact: true }).click();
    await expect(model).toHaveValue("synthetic-backup");
    await expect(page.getByRole("button", { name: "保存当前配置", exact: true })).toBeDisabled();
    expect(fixture.saves).toEqual([]);
    expect(fixture.appSaves).toEqual([]);
    expectUniformNetwork(fixture.base);
  });

  test(`公共配置 ${width}px：409保留JSON草稿与完整原版本`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await mockFallback(page, "app-conflict");
    await expect(page.getByLabel("模型名称", { exact: true })).toHaveValue("synthetic-backup");
    await page.getByRole("tab", { name: "公共配置", exact: true }).click();
    const expected = fixture.appRaw;
    await editPublicJSON(page, { ...JSON.parse(expected), untouched: "keep-public-draft" });
    await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
    await expect(page.getByText("公共配置已有更新，请重新打开后再保存", { exact: true })).toBeVisible();
    await expect(page.locator(".cm-content")).toContainText("keep-public-draft");
    expect(fixture.appSaves[0].expected_config_value).toBe(expected);
    expect(fixture.appRaw).toBe(expected);
    expectUniformNetwork(fixture.base);
  });
}

test("充值活动配置：同一公共记录的保存也携带原始版本并保留备用", async ({ page }) => {
  const fixture = await mockFallback(page);
  await expect(page.getByLabel("模型名称", { exact: true })).toHaveValue("synthetic-backup");
  await page.getByRole("menuitem", { name: /充值活动配置/ }).click();
  const expected = fixture.appRaw;
  await page.getByRole("button", { name: "新增活动", exact: true }).click();
  await page.getByLabel("充值门槛 1（元）", { exact: true }).fill("50");
  await page.getByLabel("赠送金额 1（元）", { exact: true }).fill("5");
  await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
  await expect.poll(() => fixture.appSaves.length).toBe(1);
  expect(fixture.appSaves[0].expected_config_value).toBe(expected);
  expect(JSON.parse(fixture.appSaves[0].config_value as string).builtin_ai).toEqual(JSON.parse(expected).builtin_ai);
  expect(JSON.parse(fixture.appSaves[0].config_value as string).ai_recharge_promotions).toEqual([{ amount_cents: 5000, bonus_cents: 500 }]);
  expectUniformNetwork(fixture.base);
});

for (const width of [1366, 390]) {
  test(`系统配置 ${width}px：从常规入口进入公共配置中的备用AI`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await mockFallback(page, "success", "/admin/system-config");
    await expect(page.getByRole("heading", { name: "本地程序与组件", exact: true })).toBeVisible();
    expect(fixture.reads).toBe(0);
    if (width < 600) {
      await page.getByRole("combobox", { name: "选择配置", exact: true }).click();
      await page.locator(".ant-select-dropdown").getByText("公共系统配置", { exact: true }).click();
    } else {
      await page.getByRole("menuitem", { name: /公共系统配置 合成公共配置/ }).click();
    }
    await expect(page.getByRole("tab", { name: "公共配置", exact: true })).toHaveAttribute("aria-selected", "true");
    await page.getByRole("tab", { name: "备用AI", exact: true }).click();
    await expect(page.getByLabel("模型名称", { exact: true })).toHaveValue("synthetic-backup");
    await expect(page.getByRole("button", { name: "保存当前配置", exact: true })).toHaveCount(1);
    expect(fixture.saves).toEqual([]);
    expectUniformNetwork(fixture.base);
  });
}
