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
