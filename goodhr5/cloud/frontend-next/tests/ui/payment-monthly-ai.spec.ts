/** 本文件通过模拟接口验证支付记录月消费的四位小数、重新进入页面和查询失败显示，不访问真实账号或支付服务。 */
import { test, expect, type Page } from "@playwright/test";

const previewOrigin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
test.use({ storageState: { cookies: [], origins: [{ origin: previewOrigin, localStorage: [
  { name: "goodhr5_access_token", value: "test-only-monthly-ai-token" },
] }] } });

/** mockMonthlyPayments 提供隔离的管理员会话和支付统计，屏蔽全部真实业务请求。 */
async function mockMonthlyPayments(page: Page) {
  const state = { amount: "127.5328", fail: false, errors: [] as string[] };
  page.on("pageerror", (error) => state.errors.push(error.message));
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url()), path = url.pathname;
    if (url.origin === previewOrigin && !path.startsWith("/api/") && path !== "/health") return route.continue();
    if (!["127.0.0.1", "localhost", "goodhr.58it.cn"].includes(url.hostname)) return route.abort();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    let data: any = { ok: true, config: {}, items: [] };
    if (path === "/api/auth/me") data = { user: { email: "monthly-ai@example.invalid", role: "super_admin", id: "monthly-ai-test", has_openid: true } };
    else if (path === "/api/subscription/status") data = { subscription: { active: true, member_type: "max", remaining_days: 30 } };
    else if (path === "/api/config/notification-profile") data = { profile: { completed: true } };
    else if (path === "/api/wechat-official/status") data = { enabled: false, configured: false, bound: true, followed: true, gift_granted: true };
    else if (path === "/api/tenants/invitations/pending") data = { invitations: [] };
    else if (path === "/api/user-flow" || path === "/api/user-flow/state") data = { state: "completed", flow: { version: 2, state: "completed", stage: "completed", steps: {} } };
    else if (path === "/health") data = { ok: true, version: "6.2.10.1" };
    else if (path === "/api/v1/runtime/status") data = { version: "6.2.10.1", node_installed: true, camoufox_installed: true, system_runtime: { ready: true } };
    else if (path === "/api/ai-wallet") data = { balance: "30.0000", balance_units: 300000 };
    else if (path === "/api/admin/payment/orders") {
      if (state.fail) return route.fulfill({ status: 500, headers, contentType: "application/json", body: JSON.stringify({ error: "本月 AI 消费暂时没查到，请稍后刷新。" }) });
      data = { orders: [{ order_no: "test-only-payment", user_email: "hr@example.invalid", plan_name: "AI 余额充值", amount_cents: 10000, bonus_amount_cents: 2000, status: "paid", created_at: "2026-10-13T10:00:00+08:00" }],
        stats: { monthly_ai_consumption: state.amount, month: "2026-10", timezone: "Asia/Shanghai" } };
    }
    return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify(data) });
  });
  return state;
}

for (const width of [1440, 390]) {
  test(`支付记录 ${width}px 显示四位小数月消费，重新进入时更新且无独立刷新`, async ({ page }, testInfo) => {
    const state = await mockMonthlyPayments(page);
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/admin/payment-records", { waitUntil: "domcontentloaded" });
    await expect(page.getByText(/本月 AI 消费 ¥127\.5328（北京时间）/)).toBeVisible();
    await expect(page.getByRole("columnheader", { name: "实付金额", exact: true })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`monthly-ai-${width}.png`), animations: "disabled" });
    state.amount = "127.5432";
    await expect(page.getByRole("main").getByRole("button", { name: /刷新/ })).toHaveCount(0);
    await page.reload();
    await expect(page.getByText(/本月 AI 消费 ¥127\.5432（北京时间）/)).toBeVisible();
    expect(state.errors).toEqual([]);
  });
}

test("月消费读取失败时明确提示，重新进入后可以显示零消费", async ({ page }) => {
  const state = await mockMonthlyPayments(page);
  await page.goto("/admin/payment-records", { waitUntil: "domcontentloaded" });
  await expect(page.getByText(/本月 AI 消费 ¥127\.5328（北京时间）/)).toBeVisible();
  state.fail = true;
  await page.reload();
  await expect(page.getByText(/本月 AI 消费 暂未读取/)).toBeVisible();
  await expect(page.getByText(/本月 AI 消费 ¥0\.0000/)).toHaveCount(0);
  state.fail = false;
  state.amount = "0.0000";
  await page.reload();
  await expect(page.getByText(/本月 AI 消费 ¥0\.0000（北京时间）/)).toBeVisible();
  expect(state.errors).toEqual([]);
});
