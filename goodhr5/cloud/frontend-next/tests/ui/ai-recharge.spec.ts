/** 本文件使用完全模拟接口回归充值赠送、活动配置保存、订单展示及赠送余额退款边界。 */
import { test, expect, type Page } from "@playwright/test";

const previewOrigin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
const activities = [
  { amount_cents: 5000, bonus_cents: 500 },
  { amount_cents: 10000, bonus_cents: 2000 },
  { amount_cents: 20000, bonus_cents: 5000 },
];
const policy = "充值后可随时退款，赠送的余额不可退款，赠送的金额将在最后使用。";
type MockState = {
  appConfig: Record<string, unknown>;
  calls: { path: string; method: string; body: any }[];
  errors: string[];
};
test.use({
  storageState: { cookies: [], origins: [{ origin: previewOrigin, localStorage: [
    { name: "goodhr5_access_token", value: "test-only-recharge-token" },
  ] }] },
});

/** mockRecharge 拦截全部业务接口和外部资源，所有订单与退款仅写入测试内存。 */
async function mockRecharge(page: Page, options: { noActivities?: boolean; refundable?: number; historicalRefund?: boolean; omitLedgerVersion?: boolean } = {}): Promise<MockState> {
  const state: MockState = {
    appConfig: { builtin_ai: { api_key: "test-only-key", default_model: "qwen-test" },
      admin_banners: [{ title: "原有横幅", enabled: false }], future_setting: { keep: true },
      ai_recharge_promotions: options.noActivities ? [] : activities },
    calls: [], errors: [],
  };
  page.on("pageerror", (error) => state.errors.push(error.message));
  await page.route("**/*", async (route) => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    if (url.origin === previewOrigin && !path.startsWith("/api/")) return route.continue();
    if (!["127.0.0.1", "localhost", "goodhr.58it.cn"].includes(url.hostname)) return route.abort();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
    if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    const method = request.method(), body = request.postData() ? request.postDataJSON() : null;
    state.calls.push({ path, method, body });
    let data: any = { ok: true, config: {}, items: [] };
    if (path === "/api/auth/me") data = { user: { id: "recharge-test", email: "recharge@example.invalid", role: "super_admin", has_openid: true } };
    else if (path === "/api/subscription/status") data = { subscription: { active: true, member_type: "max", remaining_days: 30, allow_ai: true, allow_auto_reply: true } };
    else if (path === "/api/ai-wallet") data = { balance: "30.0000", balance_units: 300000, bonus_balance: "20.0000", bonus_balance_units: 200000,
      refundable_balance: "10.0000", default_model: "qwen-test", models: [{ id: "qwen-test", name: "测试模型" }],
      recharge_promotions: options.noActivities ? [] : activities };
    else if (path === "/api/wechat-official/status") data = { enabled: false, configured: false, bound: true, followed: true, gift_granted: true };
    else if (path === "/api/config/notification-profile") data = { profile: { completed: true } };
    else if (path === "/api/tenants/invitations/pending") data = { invitations: [] };
    else if (path === "/api/config/user-ai") data = { config: { model: "qwen-test", enabled: true } };
    else if (path === "/api/user-flow") data = { flow: { version: 2, state: "completed", stage: "completed", stage_name: "流程完成", steps: {} } };
    else if (path === "/api/user-flow/state") data = { state: "completed", steps: {} };
    else if (path === "/api/subscription/plans") data = { plans: [] };
    else if (path === "/api/positions") data = { positions: [] };
    else if (path === "/api/candidates") data = { candidates: [], total: 0 };
    else if (path === "/api/v1/runtime/status") data = { version: "6.2.10.1", node_installed: true, camoufox_installed: true, system_runtime: { ready: true } };
    else if (path === "/api/v1/app-update/status") data = { stage: "idle", current_version: "6.2.10.1" };
    else if (path === "/health") data = { ok: true, version: "6.2.10.1" };
    else if (path === "/api/payment/orders" || path === "/api/admin/payment/orders") data = { orders: [{ order_no: "test-paid", order_type: "ai_balance", plan_name: "AI 余额充值", amount_cents: 10000, bonus_amount_cents: 2000, status: "paid" }] };
    else if (path.startsWith("/api/payment/orders/")) data = { order: { status: "pending" } };
    else if (path === "/api/payment/ai-balance") data = { order: { order_no: "test-pending", plan_name: "AI 余额充值", amount: body.amount_yuan, amount_cents: Number(body.amount_yuan) * 100,
      bonus_amount: "20.00", bonus_amount_cents: 2000 }, payment: { code_url: "weixin://wxpay/test-only" } };
    else if (path === "/api/ai-wallet/records") data = { records: [options.historicalRefund
      ? { id: "legacy-refund", category: "refund", ...(options.omitLedgerVersion ? {} : { wallet_ledger_version: 0 }), change_units: -50000, change: "-5.0000", bonus_change_units: -7000,
        balance_after: "0.0000", reason: "历史退款", created_at: "2026-10-01T10:00:00+08:00" }
      : { id: "refund-record", category: "refund", wallet_ledger_version: 1, change_units: -300000, change: "-30.0000", bonus_change_units: -200000,
        balance_after: "0.0000", reason: "退款并收回赠送", created_at: "2026-10-12T10:00:00+08:00" }], total: 1 };
    else if (path === "/api/payment/refund") {
      const refundable = options.refundable ?? 1000;
      data = method === "POST" ? { message: "测试退款已提交" } : {
        membership: { available: false, max_amount_cents: 0, max_amount: "0.00" },
        ai_balance: { available: refundable > 0, max_amount_cents: refundable, max_amount: (refundable / 100).toFixed(2), message: "赠送余额不可退款，当前没有可退金额。" },
      };
    } else if (path === "/api/admin/system/configs/") data = { configs: [
      { config_key: "system.app_config", config_value: JSON.stringify(state.appConfig), enabled: true },
      { config_key: "system.wechat_official_account", config_value: '{"enabled":false}', enabled: true },
    ] };
    else if (path === "/api/admin/system/configs/system.app_config" && method === "PUT") state.appConfig = JSON.parse(body.config_value);
    return route.fulfill({ status: 200, headers, json: data });
  });
  return state;
}

for (const [name, path] of [["会员页", "/admin/subscription"]]) {
  test(`${name}选择活动并自定义 150 元时赠送 20 元，订单只提交实付金额`, async ({ page }, testInfo) => {
    const state = await mockRecharge(page);
    await page.goto(`${previewOrigin}${path}`, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: /^充\s*值$/ }).click();
    const dialog = page.getByRole("dialog", { name: "充值 AI 余额", exact: true });
    await expect(dialog.getByText(policy, { exact: true })).toBeVisible();
    await dialog.getByRole("radio", { name: /充值 100 元/ }).check();
    await expect(dialog.getByLabel("充值金额（元）", { exact: true })).toHaveValue("100.00");
    await expect(dialog.getByTestId("ai-recharge-estimate")).toContainText("预计到账 ￥120.00");
    await dialog.getByRole("radio", { name: /自定义金额/ }).check();
    await dialog.getByLabel("充值金额（元）", { exact: true }).fill("150");
    await dialog.getByLabel("充值金额（元）", { exact: true }).press("Tab");
    await expect(dialog.getByTestId("ai-recharge-estimate")).toContainText("赠送 ￥20.00");
    await expect(dialog.getByTestId("ai-recharge-estimate")).toContainText("预计到账 ￥170.00");
    await page.screenshot({ path: testInfo.outputPath(`${name}-recharge.png`), animations: "disabled" });
    await dialog.getByRole("button", { name: "先不充", exact: true }).click();
    await page.getByRole("button", { name: /^充\s*值$/ }).click();
    await expect(dialog.getByLabel("充值金额（元）", { exact: true })).toHaveValue(/150(?:\.00)?/);
    await dialog.getByRole("button", { name: "去支付", exact: true }).click();
    const confirmation = page.getByRole("dialog", { name: "支付前请确认", exact: true });
    await expect(confirmation).toBeVisible();
    await confirmation.getByRole("button", { name: name === "会员页" ? "我已理解，继续支付" : "确认", exact: true }).click();
    const payment = page.getByRole("dialog", { name: "微信扫码支付", exact: true });
    await expect(payment).toBeVisible();
    await expect(payment.getByText("支付成功后另赠 ￥20.00，赠送余额不可退款。", { exact: true })).toBeVisible();
    expect(state.calls.findLast((call) => call.path === "/api/payment/ai-balance")?.body).toEqual({ amount_yuan: "150" });
    expect(state.errors).toEqual([]);
  });
}

test("无活动时保留 10 元默认金额和自定义充值，手机弹框完整可见", async ({ page }, testInfo) => {
  const state = await mockRecharge(page, { noActivities: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`${previewOrigin}/admin/subscription`, { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: /^充\s*值$/ }).click();
  const dialog = page.getByRole("dialog", { name: "充值 AI 余额", exact: true });
  await expect(dialog.getByRole("radio")).toHaveCount(0);
  await expect(dialog.getByLabel("充值金额（元）", { exact: true })).toHaveValue(/10(?:\.00)?/);
  await dialog.getByLabel("充值金额（元）", { exact: true }).fill("");
  await expect(dialog.getByRole("button", { name: "去支付", exact: true })).toBeDisabled();
  await dialog.getByLabel("充值金额（元）", { exact: true }).fill("12.34");
  await expect(dialog.getByRole("button", { name: "去支付", exact: true })).toBeEnabled();
  await expect(dialog.getByText("退款会收回该笔充值剩余的赠送余额。", { exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("recharge-mobile.png"), animations: "disabled" });
  expect(state.errors).toEqual([]);
});

test("其他配置中新增和删除充值活动，保存保留全部其他系统字段", async ({ page }, testInfo) => {
  const state = await mockRecharge(page, { noActivities: true });
  const original = structuredClone(state.appConfig);
  await page.goto(`${previewOrigin}/admin/system-config`, { waitUntil: "domcontentloaded" });
  await page.getByRole("tab", { name: "其他配置", exact: true }).click();
  await page.getByRole("menuitem", { name: /充值活动配置/ }).click();
  await page.getByRole("button", { name: "新增活动", exact: true }).click();
  await page.getByLabel("充值门槛 1（元）", { exact: true }).fill("100");
  await page.getByLabel("赠送金额 1（元）", { exact: true }).fill("20");
  await page.getByLabel("赠送金额 1（元）", { exact: true }).press("Tab");
  await page.getByRole("button", { name: "新增活动", exact: true }).click();
  await page.getByLabel("充值门槛 2（元）", { exact: true }).fill("100");
  await page.getByLabel("赠送金额 2（元）", { exact: true }).fill("5");
  await page.getByLabel("赠送金额 2（元）", { exact: true }).press("Tab");
  await expect(page.getByText("第 2 个活动的充值门槛重复了。", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "保存当前配置", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "删除活动 2", exact: true }).click();
  await page.screenshot({ path: testInfo.outputPath("recharge-config.png"), animations: "disabled" });
  await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
  await expect.poll(() => state.appConfig.ai_recharge_promotions).toEqual([{ amount_cents: 10000, bonus_cents: 2000 }]);
  expect(state.appConfig).toEqual({ ...original, ai_recharge_promotions: [{ amount_cents: 10000, bonus_cents: 2000 }] });
  await page.getByRole("button", { name: "删除活动 1", exact: true }).click();
  await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
  await expect.poll(() => state.appConfig.ai_recharge_promotions).toEqual([]);
  expect(state.appConfig).toEqual(original);
  expect(state.errors).toEqual([]);
});

test("退款额度由后端核定，流水区分 10 元实际退款和 20 元赠送收回", async ({ page }) => {
  const state = await mockRecharge(page);
  await page.goto(`${previewOrigin}/admin/subscription`, { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("columnheader", { name: "实付金额", exact: true })).toBeVisible();
  await expect(page.getByText("赠送 ￥20.00", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "退款", exact: true }).click();
  const refund = page.getByRole("dialog", { name: "申请退款", exact: true });
  await expect(refund.getByLabel("退款金额（元）", { exact: true })).toHaveValue("10.00");
  await expect(refund.getByText(/退款会收回该笔充值剩余的赠送余额/)).toBeVisible();
  await refund.getByRole("button", { name: "先不退", exact: true }).click();
  await page.getByRole("tab", { name: "AI 使用记录", exact: true }).click();
  await expect(page.getByText("实际退款 ￥10.00", { exact: true })).toBeVisible();
  await expect(page.getByText("收回赠送 ￥20.0000", { exact: true })).toBeVisible();
  expect(state.errors).toEqual([]);
});

test("仅剩赠送余额时退款按钮禁止提交", async ({ page }) => {
  const state = await mockRecharge(page, { refundable: 0 });
  await page.goto(`${previewOrigin}/admin/subscription`, { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "退款", exact: true }).click();
  const refund = page.getByRole("dialog", { name: "申请退款", exact: true });
  await expect(refund.getByText("赠送余额不可退款，当前没有可退金额。", { exact: true })).toBeVisible();
  await expect(refund.getByRole("button", { name: "确认退款", exact: true })).toBeDisabled();
  expect(state.calls.filter((call) => call.path === "/api/payment/refund" && call.method === "POST")).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

test("管理员支付记录分列显示 100 元实付和 20 元赠送", async ({ page }) => {
  const state = await mockRecharge(page);
  await page.goto(`${previewOrigin}/admin/payment-records`, { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("columnheader", { name: "实付金额", exact: true })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "赠送余额", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "￥100.00", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "￥20.00", exact: true })).toBeVisible();
  expect(state.errors).toEqual([]);
});

for (const omitLedgerVersion of [false, true]) {
test(`历史退款${omitLedgerVersion ? "未提供版本" : "版本 0"}的迁移赠送变化不用于猜测实际退款现金`, async ({ page }) => {
  const state = await mockRecharge(page, { historicalRefund: true, omitLedgerVersion });
  await page.goto(`${previewOrigin}/admin/subscription`, { waitUntil: "domcontentloaded" });
  await page.getByRole("tab", { name: "AI 使用记录", exact: true }).click();
  await expect(page.getByText("￥-5.0000", { exact: true })).toBeVisible();
  await expect(page.getByText("历史退款，金额请以支付渠道记录为准。", { exact: true })).toBeVisible();
  await expect(page.getByText(/实际退款 ￥/)).toHaveCount(0);
  await expect(page.getByText(/收回赠送 ￥/)).toHaveCount(0);
  expect(state.errors).toEqual([]);
});
}
