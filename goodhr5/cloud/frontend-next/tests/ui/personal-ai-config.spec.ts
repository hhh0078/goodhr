/** 本文件使用隔离接口验证个人 AI 配置整合、站内模型选择和外部模型自由输入。 */
import { test, expect, type Page } from "@playwright/test";

const previewOrigin = new URL(
  process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190",
).origin;
const builtinAddress = `${previewOrigin}/api/ai-compatible/v1/chat/completions`;
const models = [
  {
    id: "qwen3.8-flash",
    name: "通义千问3.8-flash-默认",
    description: "非常推荐",
    input_price_per_1m_cents: 200,
    output_price_per_1m_cents: 540,
  },
  {
    id: "qwen-plus",
    name: "通义千问 Plus",
    description: "适合复杂分析",
    input_price_per_1m_cents: 400,
    output_price_per_1m_cents: 1200,
  },
];
type SavedCall = { path: string; body: Record<string, unknown> };
const pageErrors = new WeakMap<Page, string[]>();

test.beforeEach(({ page }) => {
  const errors: string[] = [];
  pageErrors.set(page, errors);
  page.on("pageerror", (error) => errors.push(error.message));
});

test.afterEach(({ page }) => {
  expect(pageErrors.get(page)).toEqual([]);
});

test.use({
  storageState: {
    cookies: [],
    origins: [
      {
        origin: previewOrigin,
        localStorage: [{ name: "goodhr5_access_token", value: "test-only-ai-token" }],
      },
    ],
  },
});

/** mockPersonalAI 拦截全部接口，记录模拟保存且不访问真实账号或模型供应商。 */
async function mockPersonalAI(
  page: Page,
  options: { external?: boolean; walletFailure?: boolean; emptyModels?: boolean } = {},
) {
  const calls: SavedCall[] = [];
  let ai = {
    base_url: options.external ? "https://custom.example.invalid/v1/chat/completions" : builtinAddress,
    model: options.external ? "my-own-model" : "retired-model",
    api_key: "test-only-custom-key",
    api_key_set: true,
    enabled: true,
  };
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.origin === previewOrigin && !url.pathname.startsWith("/api/"))
      return route.continue();
    if (!url.pathname.startsWith("/api/") && url.pathname !== "/health")
      return route.abort();
    const headers = {
      "access-control-allow-origin": "*",
      "access-control-allow-headers": "*",
      "access-control-allow-methods": "GET,POST,PUT,OPTIONS",
    };
    if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    const path = url.pathname;
    if (request.method() !== "GET") calls.push({ path, body: request.postDataJSON() || {} });
    let data: Record<string, unknown> = { ok: true, config: {}, items: [] };
    if (path === "/api/auth/me")
      data = { user: { email: "ai-test@example.invalid", role: "user", id: "test-user" } };
    else if (path === "/api/subscription/status")
      data = { subscription: { active: true, member_type: "plus", allow_ai: true, remaining_days: 10 } };
    else if (path === "/api/ai-wallet") {
      if (options.walletFailure) return route.fulfill({ status: 503, headers, json: { error: "测试模型列表暂时不可用" } });
      data = { ok: true, balance: "12.00", public_base_url: builtinAddress, default_model: models[0].id, models: options.emptyModels ? [] : models };
    } else if (path === "/api/config/user-ai") {
      if (request.method() === "PUT") ai = { ...ai, ...request.postDataJSON() };
      data = { config: ai };
    } else if (path === "/api/config/user-preferences")
      data = { config: { ai_model: ai.model, candidate_read_concurrency: 100 } };
    else if (path === "/api/ai-wallet/use-builtin") {
      ai = { ...ai, base_url: builtinAddress, model: models[0].id, api_key: "ghai_test-only-key" };
      data = { config: ai };
    } else if (path === "/api/tenants/invitations/pending") data = { invitations: [] };
    else if (path === "/api/config/notification-profile") data = { profile: { completed: true } };
    else if (path === "/api/v1/runtime/status")
      data = { version: "test-only", node_installed: true, camoufox_installed: true, system_runtime: { ready: true } };
    else if (path === "/api/v1/app-update/status") data = { stage: "idle", current_version: "test-only" };
    else if (path === "/health") data = { ok: true, version: "test-only" };
    return route.fulfill({ status: 200, headers, json: data });
  });
  await page.goto(`${previewOrigin}/admin/personal-config`, { waitUntil: "domcontentloaded" });
  await expect(page.getByLabel("API 地址", { exact: true })).toHaveValue(ai.base_url);
  if (options.walletFailure || options.emptyModels)
    await expect(page.getByRole("button", { name: "保存配置", exact: true })).toBeDisabled();
  else await expect(page.getByRole("button", { name: "保存配置", exact: true })).toBeEnabled();
  return calls;
}

test("合并 AI 页面，站内模型显示价格与提示并保存选项", async ({ page }, testInfo) => {
  const calls = await mockPersonalAI(page);
  await expect(page.getByRole("tab", { name: "AI配置", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "内置AI", exact: true })).toHaveCount(0);
  await expect(page.getByRole("tab", { name: "自定义AI", exact: true })).toHaveCount(0);
  await expect(page.getByText("查看视频教程", { exact: true })).toHaveCount(0);
  await expect(page.getByText("获取 AI 接口", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "使用内置 AI", exact: true })).toBeVisible();
  const select = page.getByLabel("模型名称", { exact: true });
  await expect(select).toHaveAttribute("role", "combobox");
  await expect(page.getByText(`${models[0].name} (${models[0].id})`, { exact: true })).toBeVisible();
  await select.click();
  const dropdown = page.locator(".ant-select-dropdown").filter({ visible: true });
  await expect(dropdown).toHaveCSS("opacity", "1");
  await expect(dropdown).toContainText("非常推荐");
  await expect(dropdown).toContainText("¥2.00");
  await expect(dropdown).toContainText("¥5.40");
  await expect(dropdown).toContainText("适合复杂分析");
  await page.locator(".ant-select-item-option").filter({ hasText: models[1].name }).click();
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByText("个人配置已保存", { exact: true })).toBeVisible();
  expect(calls.find((call) => call.path === "/api/config/user-ai")?.body.model).toBe(models[1].id);
  expect(calls.find((call) => call.path === "/api/config/user-preferences")?.body.ai_model).toBe(models[1].id);
  await page.screenshot({ path: testInfo.outputPath("personal-ai-desktop.png"), fullPage: true });
});

test("外部地址可填写任意模型，切回内置地址后改为单选", async ({ page }) => {
  const calls = await mockPersonalAI(page, { external: true });
  const model = page.getByLabel("模型名称", { exact: true });
  await expect(model).toHaveValue("my-own-model");
  await model.fill("external-unlisted-model");
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByText("个人配置已保存", { exact: true })).toBeVisible();
  expect(calls.find((call) => call.path === "/api/config/user-ai")?.body.model).toBe("external-unlisted-model");
  await page.getByRole("button", { name: "使用内置 AI", exact: true }).click();
  await expect(page.getByLabel("API 地址", { exact: true })).toHaveValue(builtinAddress);
  await expect(model).toHaveAttribute("role", "combobox");
  await expect(page.getByText(`${models[0].name} (${models[0].id})`, { exact: true })).toBeVisible();
  await expect(page.getByLabel("API Key", { exact: true })).toHaveValue("ghai_test-only-key");
  expect(calls.filter((call) => call.path === "/api/ai-wallet/use-builtin")).toHaveLength(1);
  await page.getByLabel("API 地址", { exact: true }).fill("https://another.example.invalid/v1");
  await expect(model).not.toHaveAttribute("role", "combobox");
  await model.fill("another-free-model");
  await page.getByRole("button", { name: "先测试 AI", exact: true }).click();
  await expect(page.getByText("AI 接口测试成功", { exact: true })).toBeVisible();
  expect(calls.find((call) => call.path === "/api/config/test-ai")?.body.model).toBe("another-free-model");
});

test("模型列表读取失败不覆写旧模型，外部地址仍可保存", async ({ page }) => {
  const calls = await mockPersonalAI(page, { walletFailure: true });
  await expect(page.getByRole("button", { name: "保存配置", exact: true })).toBeDisabled();
  expect(calls.filter((call) => call.path === "/api/config/user-ai")).toHaveLength(0);
  await page.getByLabel("API 地址", { exact: true }).fill("https://custom.example.invalid/v1");
  await page.getByLabel("模型名称", { exact: true }).fill("still-free-model");
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByText("个人配置已保存", { exact: true })).toBeVisible();
  expect(calls.find((call) => call.path === "/api/config/user-ai")?.body.model).toBe("still-free-model");
});

test("空模型列表阻止站内保存并提供重新读取", async ({ page }) => {
  const calls = await mockPersonalAI(page, { emptyModels: true });
  await expect(page.getByLabel("模型名称", { exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "保存配置", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "重新读取模型", exact: true })).toBeVisible();
  expect(calls.filter((call) => call.path === "/api/config/user-ai")).toHaveLength(0);
});

test("手机端能展开模型说明与价格", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockPersonalAI(page);
  await page.getByLabel("模型名称", { exact: true }).click();
  await expect(page.locator(".ant-select-dropdown").filter({ visible: true })).toHaveCSS("opacity", "1");
  const option = page.locator(".ant-select-item-option").filter({ hasText: models[0].name });
  await expect(option).toContainText("非常推荐");
  await expect(option).toContainText("¥5.40");
  const optionBox = await option.boundingBox();
  expect(optionBox).not.toBeNull();
  expect(optionBox!.x).toBeGreaterThanOrEqual(0);
  expect(optionBox!.x + optionBox!.width).toBeLessThanOrEqual(390);
  await page.screenshot({ path: testInfo.outputPath("personal-ai-mobile.png"), fullPage: true });
});
