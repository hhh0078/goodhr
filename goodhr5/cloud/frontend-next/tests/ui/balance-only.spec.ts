/** 文件作用说明：以合成账号验证余额胶囊、充值模型、旧岗位AI默认及自定义接口确认，所有业务请求只在测试内存处理。 */
import { test, expect } from "@playwright/test";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "balance-only-synthetic-token" }] }] } });
test.setTimeout(90_000);
const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
const models = [{ id: "synthetic-a", name: "合成低价模型", description: "合成模型说明", input_price_per_1m_cents: 10, output_price_per_1m_cents: 20 }, { id: "synthetic-b", name: "合成视觉模型", description: "仅用于图片能力界面验证", input_price_per_1m_cents: 30, output_price_per_1m_cents: 40 }];

for (const width of [1366, 390]) {
  test(`余额入口${width}px无会员天数，直接充值并保存动态模型`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 });
    const state = await mockUniformAdmin(page);
    let config = { base_url: "https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions", api_key: "ghai_synthetic", api_key_set: true, model: models[0].id };
    const writes: unknown[] = [];
    await page.route("**/api/config/user-ai", async route => {
      if (route.request().method() === "PUT") { writes.push(route.request().postDataJSON()); config = { ...config, ...route.request().postDataJSON() }; }
      return route.fulfill({ headers, json: { config } });
    });
    await page.route("**/api/ai-wallet", route => route.fulfill({ headers, json: { balance: "12.3456", models, default_model: models[0].id, recharge_promotions: [] } }));
    await page.route("**/api/subscription/status", route => route.fulfill({ headers, json: { subscription: { active: false, member_type: "free", remaining_days: 0 } } }));
    await page.goto("/admin/positions");
    const balance = page.getByRole("button", { name: "AI余额充值", exact: true });
    await expect(balance).toBeVisible();
    await expect(balance).toHaveText("AI余额 ￥12");
    await balance.click();
    const dialog = page.getByRole("dialog", { name: "充值 AI 余额", exact: true });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText("合成低价模型 (synthetic-a)", { exact: true })).toBeVisible();
    await dialog.getByLabel("模型名称", { exact: true }).click();
    await page.locator(".ant-select-item-option").filter({ hasText: "合成视觉模型" }).click();
    await expect(dialog.getByText("合成视觉模型 (synthetic-b)", { exact: true })).toBeVisible();
    await expect.poll(() => writes.length).toBe(1);
    expect(config.model).toBe(models[1].id);
    await expect(dialog.getByRole("button", { name: "去支付", exact: true })).toBeEnabled();
    await page.screenshot({ path: info.outputPath(`recharge-model-${width}.png`), fullPage: true, animations: "disabled" });
    const box = await dialog.boundingBox();
    expect(box && box.x >= 0 && box.x + box.width <= width).toBeTruthy();
    await dialog.getByRole("button", { name: "先不充", exact: true }).click();
    expect(state.calls.some(call => call.path === "/api/payment/ai-balance")).toBe(false);
    expect(state.errors).toEqual([]);
  });

  test(`自定义接口${width}px只在主动切换成功后关闭提醒`, async ({ page }, info) => {
    await page.setViewportSize({ width, height: 900 });
    const state = await mockUniformAdmin(page);
    let config = { base_url: "https://custom.invalid/v1", api_key: "synthetic-key", api_key_set: true, model: "custom-model" };
    let attempts = 0;
    await page.route("**/api/config/user-ai", route => route.fulfill({ headers, json: { config } }));
    await page.route("**/api/ai-wallet", route => route.fulfill({ headers, json: { balance: "1.0000", models, default_model: models[0].id } }));
    await page.route("**/api/ai-wallet/use-builtin", route => {
      attempts++;
      if (attempts === 1) return route.fulfill({ headers, status: 503, json: { error: "模拟切换失败，请重试" } });
      config = { ...config, base_url: "https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions", api_key: "ghai_synthetic", model: models[0].id };
      return route.fulfill({ headers, json: { config, models } });
    });
    await page.goto("/admin/personal-config");
    const dialog = page.getByRole("dialog", { name: "作者来小声求个支持", exact: true });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("不需要额外购买会员");
    await expect(dialog).toContainText("0.10 元");
    await expect(dialog.locator(".ant-modal-close")).toHaveCount(0);
    expect(attempts).toBe(0);
    await page.keyboard.press("Escape");
    await page.mouse.click(2, 2);
    await expect(dialog).toBeVisible();
    await page.screenshot({ path: info.outputPath(`builtin-notice-${width}.png`), fullPage: true, animations: "disabled" });
    const button = dialog.getByRole("button", { name: "好吧，那就使用内置 AI 吧", exact: true });
    await button.click();
    await expect(button).toBeEnabled();
    await expect(dialog).toBeVisible();
    await button.click();
    await expect(dialog).toBeHidden();
    expect(attempts).toBe(2);
    await expect(page.getByLabel("API 地址", { exact: true })).toHaveValue(config.base_url);
    expect(state.errors).toEqual([]);
  });
}

/** 余额启动回归：用页面已有12元、服务器最新0.0522元验证先读最新余额，充值只打开既有弹框。 */
for (const width of [1366, 390]) for (const entry of ["岗位", "自动回复"]) test(`${entry}开始前余额不足直接充值 ${width}`, async ({ page }, info) => {
  await page.setViewportSize({ width, height: 900 });
  const state = await mockUniformAdmin(page);
  let balance = "12.0000";
  await page.route("**/api/ai-wallet", route => route.fulfill({ headers, json: { balance, balance_units: Math.round(Number(balance) * 10000), models, default_model: models[0].id } }));
  await page.route("**/api/config/user-ai", route => route.fulfill({ headers, json: { config: { base_url: "https://goodhr.58it.cn/api/ai-compatible/v1/chat/completions", api_key_set: true, model: models[0].id } } }));
  await page.route(/\/api\/(?:auto-reply\/|v1\/local\/auto-reply\/)/, async route => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (path.endsWith("/auto-reply/status")) return route.fulfill({ headers, json: { ok: true, data: { status: "stopped" } } });
    if (path.endsWith("/questions/summary")) return route.fulfill({ headers, json: { pending_count: 0, positions: [] } });
    if (path.endsWith("/logs")) return route.fulfill({ headers, json: { items: [], next_cursor: "" } });
    if (/\/positions\/[^/]+\/status$/.test(path)) return route.fulfill({ headers, json: { configured_enabled: false, enabled: false, version: 1 } });
    throw new Error(`余额不足不应调用 ${path}`);
  });
  await page.goto("/admin/positions");
  await expect(page.getByRole("button", { name: "AI余额充值", exact: true })).toHaveText("AI余额 ￥12");
  balance = "0.0522";
  const before = state.calls.length;
  if (entry === "岗位") await page.getByRole("button", { name: "开始", exact: true }).first().click();
  else {
    await page.getByTestId("auto-reply-global").click();
    await page.getByRole("dialog", { name: "自动回复", exact: true }).getByRole("button", { name: "开始自动回复", exact: true }).click();
  }
  const insufficient = page.getByRole("dialog", { name: "余额不足", exact: true });
  await expect(insufficient).toBeVisible();
  await expect(insufficient).toHaveCSS("transform", "none");
  await expect(insufficient).toContainText("余额不足0.1元，请充值后再试。");
  await expect(page.getByRole("dialog", { name: "岗位还没启动成功", exact: true })).toHaveCount(0);
  await expect(page.getByRole("dialog", { name: "开始招聘岗位", exact: true })).toHaveCount(0);
  await expect(page.getByRole("dialog", { name: "确认当前招聘账号", exact: true })).toHaveCount(0);
  await page.screenshot({ path: info.outputPath(`start-balance-${entry}-${width}.png`), animations: "disabled" });
  await insufficient.getByRole("button", { name: "充值", exact: true }).click();
  const recharge = page.getByRole("dialog", { name: "充值 AI 余额", exact: true });
  await expect(recharge).toBeVisible();
  await expect(recharge.getByRole("button", { name: "去支付", exact: true })).toBeVisible();
  expect(state.calls.slice(before).filter(call => /\/(?:run|start|page\/open)$/.test(call.path))).toEqual([]);
  expect(state.calls.some(call => call.path === "/api/payment/ai-balance")).toBe(false);
  expect(state.errors).toEqual([]);
  await recharge.getByRole("button", { name: "先不充", exact: true }).click();
  balance = "0.1000";
  if (entry === "岗位") {
    await page.getByRole("button", { name: "开始", exact: true }).first().click();
    await expect(page.getByRole("dialog", { name: "开始招聘岗位", exact: true })).toBeVisible();
  } else {
    await page.getByRole("dialog", { name: "自动回复", exact: true }).getByRole("button", { name: "开始自动回复", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "确认当前招聘账号", exact: true })).toBeVisible();
  }
});

test("余额不足仍可停止自动回复，不额外检查启动余额", async ({ page }) => {
  await mockUniformAdmin(page);
  let walletReads = 0, stopped = 0, running = true;
  await page.route("**/api/ai-wallet", route => { walletReads++; return route.fulfill({ headers, json: { balance: "0.0522", balance_units: 522 } }); });
  await page.route(/\/api\/(?:auto-reply\/|v1\/local\/auto-reply\/)/, async route => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (path.endsWith("/auto-reply/status")) return route.fulfill({ headers, json: { ok: true, data: { status: running ? "running" : "stopped" } } });
    if (path.endsWith("/auto-reply/stop")) { stopped++; running = false; return route.fulfill({ headers, json: { ok: true, data: { status: "stopped" } } }); }
    if (path.endsWith("/questions/summary")) return route.fulfill({ headers, json: { pending_count: 0, positions: [] } });
    if (path.endsWith("/logs")) return route.fulfill({ headers, json: { items: [], next_cursor: "" } });
    if (/\/positions\/[^/]+\/status$/.test(path)) return route.fulfill({ headers, json: { enabled: false, version: 1 } });
    throw new Error(`停止不应调用 ${path}`);
  });
  await page.goto("/admin/positions");
  await page.getByTestId("auto-reply-global").click();
  const workspace = page.getByRole("dialog", { name: "自动回复", exact: true });
  const readsBeforeStop = walletReads;
  await workspace.getByRole("button", { name: "停止自动回复", exact: true }).click();
  await expect.poll(() => stopped).toBe(1);
  expect(walletReads).toBe(readsBeforeStop);
  await expect(page.getByRole("dialog", { name: "余额不足", exact: true })).toHaveCount(0);
});
