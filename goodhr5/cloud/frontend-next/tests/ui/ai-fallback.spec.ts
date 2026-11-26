/** 文件作用：在独立Chrome和全拦截合成接口中回归备用AI表单、密钥保留、并发保护与手机布局，不访问真实配置或模型。 */
import { expect, test, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { expectUniformNetwork, mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const artifactDirectory = resolve(process.env.UI_AI_FALLBACK_ARTIFACT_DIR || "../../../output/playwright/ai-fallback");
const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "ai-fallback-synthetic-token" }] }] } });

/** mockFallback 只在内存处理备用配置接口，保留合成请求用于核对，不发起任何真实AI或保存操作。 */
async function mockFallback(page: Page, mode: "success" | "read-failed" | "incomplete" | "conflict" | "new-key" | "user" = "success") {
  const base = await mockUniformAdmin(page);
  const saves: Record<string, unknown>[] = [];
  let reads = 0;
  let releaseSave = () => {};
  let snapshot = { ok: true, config: { enabled: false, base_url: "https://backup.example.invalid/v1/chat/completions", api_key_set: mode !== "new-key", model: "synthetic-backup" }, revision: "revision-before" };
  if (mode === "user") await page.route("**/api/auth/me", route => route.fulfill({ headers, json: { user: { id: "fallback-user", email: "user@example.invalid", role: "user" } } }));
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
      snapshot = { ok: true, config: { enabled: body.enabled, base_url: body.base_url, model: body.model, api_key_set: snapshot.config.api_key_set || Boolean(body.api_key) }, revision: "revision-after" };
    }
    return route.fulfill({ headers, json: snapshot });
  });
  await page.goto(`${uniformOrigin}/admin/ai-fallback`, { waitUntil: "domcontentloaded" });
  return { base, saves, releaseSave: () => releaseSave(), get reads() { return reads; } };
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
    await expect(page.getByRole("heading", { name: "备用AI", exact: true })).toBeVisible();
    await expect(page.getByLabel("API 地址", { exact: true })).toHaveValue("https://backup.example.invalid/v1/chat/completions");
    await expect(page.getByLabel("API 密钥", { exact: true })).toHaveValue("");
    await expect(page.getByText("已配置。留空会保留原密钥，填写后才会替换。", { exact: true })).toBeVisible();
    const save = page.getByRole("button", { name: /保存配置/ });
    await expect(save).toBeEnabled();
    if (width < 600) await page.getByRole("button", { name: "打开菜单", exact: true }).click();
    await expect(page.getByRole("menuitem", { name: "备用AI", exact: true })).toBeVisible();
    if (width < 600) await page.getByRole("menuitem", { name: "备用AI", exact: true }).click();
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
    await expectFitsViewport(page);
    expectUniformNetwork(fixture.base);
  });

  test(`备用AI ${width}px：读取失败不可编辑或覆盖`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await mockFallback(page, "read-failed");
    await expect(page.getByText("合成读取失败，请稍后重试", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "保存配置", exact: true })).toBeDisabled();
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
    await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await expect(page.getByRole("link", { name: "重新打开配置", exact: true })).toBeVisible();
    await expect(page.getByLabel("模型名称", { exact: true })).toHaveValue("keep-unsaved-draft");
    await expect(page.getByRole("button", { name: "保存配置", exact: true })).toBeDisabled();
    expect(fixture.saves.length).toBe(1);
    expectUniformNetwork(fixture.base);
  });

  test(`备用AI ${width}px：普通用户无菜单且不读配置`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const fixture = await mockFallback(page, "user");
    await expect(page.getByText("只有超级管理员可以查看和设置备用AI", { exact: true })).toBeVisible();
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
  await expect(page.getByRole("button", { name: "保存配置", exact: true })).toBeDisabled();
  expect(fixture.saves).toEqual([]);
  expectUniformNetwork(fixture.base);
});

test("备用AI：首次启用必须填密钥，保存后清空输入且不回显", async ({ page }) => {
  const fixture = await mockFallback(page, "new-key");
  await expect(page.getByLabel("API 地址", { exact: true })).toBeEnabled();
  await page.getByRole("switch", { name: "启用备用AI", exact: true }).click();
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByText("启用前请填好备用密钥", { exact: true })).toBeVisible();
  expect(fixture.saves).toEqual([]);
  await page.getByLabel("API 密钥", { exact: true }).fill("synthetic-secret-not-real");
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByText("备用AI已保存", { exact: true })).toBeVisible();
  expect(fixture.saves[0].api_key).toBe("synthetic-secret-not-real");
  await expect(page.getByLabel("API 密钥", { exact: true })).toHaveValue("");
  await expect(page.getByText("已配置。留空会保留原密钥，填写后才会替换。", { exact: true })).toBeVisible();
  expectUniformNetwork(fixture.base);
});
