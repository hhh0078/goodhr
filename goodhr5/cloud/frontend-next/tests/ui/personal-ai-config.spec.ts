/** 本文件使用隔离接口验证个人 AI 配置整合、站内模型选择和外部模型自由输入。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

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
    else if (path === "/api/user-flow" || path === "/api/user-flow/state")
      data = { flow: { version: 2, state: "completed", stage: "completed", tutorial_completed: true, steps: {} } };
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

test("鼠标速度默认快速档，五档范围齐全并保存选择且保留其他配置", async ({ page }) => {
  const screenshotDirectory = resolve(process.env.UI_MOUSE_SPEED_ARTIFACT_DIR || "../../../output/mouse-speed");
  mkdirSync(screenshotDirectory, { recursive: true });
  for (const viewport of [{ width: 1600, height: 1000 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport);
    const calls = await mockPersonalAI(page);
    await page.getByRole("tab", { name: "随机时间", exact: true }).click();
    const speed = page.getByRole("combobox", { name: "鼠标移动速度", exact: true });
    await expect(speed).toBeVisible();
    await expect(page.getByText("快速（默认）· 180–360 ms", { exact: true })).toBeVisible();
    await expect(page.getByText("每次移动在所选范围内随机。保存后，关闭并重新打开 GoodHR 浏览器生效。", { exact: true })).toBeVisible();
    await speed.click();
    const dropdown = page.locator(".ant-select-dropdown").filter({ visible: true });
    for (const label of ["超快 · 90–210 ms", "快速（默认）· 180–360 ms", "标准 · 350–650 ms", "舒缓 · 650–1100 ms", "慢速 · 1100–1800 ms"]) {
      await expect(dropdown.getByText(label, { exact: true })).toBeVisible();
    }
    await dropdown.getByText("超快 · 90–210 ms", { exact: true }).click();
    await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await expect(page.getByText("个人配置已保存", { exact: true })).toBeVisible();
    const saved = calls.find((call) => call.path === "/api/config/user-preferences")?.body;
    expect(saved?.mouse_speed).toBe("ultra_fast");
    expect(saved?.candidate_read_concurrency).toBe(100);
    expect(saved?.detail_close_delay_min).toBe(0);
    const box = await speed.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width);
    await page.screenshot({ path: resolve(screenshotDirectory, `mouse-speed-${viewport.width}.png`), fullPage: true });
  }
});

test("个人配置分类和内容同一白色面板，无顶部标题且通知保存及所有分类可用", async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 1000 });
  const calls = await mockPersonalAI(page);
  await expect(page.getByRole("heading", { name: "个人配置", exact: true })).toHaveCount(0);
  const panel = page.locator(".goodhr-admin-content .goodhr-section-panel");
  await expect(panel).toHaveCount(1);
  await expect(panel).toHaveCSS("background-color", "rgb(255, 255, 255)");
  await expect(panel.getByRole("tab", { name: "AI配置", exact: true })).toBeVisible();
  await expect(panel.getByLabel("API 地址", { exact: true })).toBeVisible();
  await expect(panel.getByRole("button", { name: "使用内置 AI", exact: true })).toBeEnabled();
  await panel.getByRole("button", { name: "先测试 AI", exact: true }).click();
  expect(calls.some((call) => call.path.includes("test"))).toBe(true);
  for (const category of [
    { tab: "随机时间", title: "操作节奏" }, { tab: "其他配置", title: "候选人读取" }, { tab: "账号安全", title: "修改登录密码" },
  ]) {
    await panel.getByRole("tab", { name: category.tab, exact: true }).click();
    await expect(panel.getByText(category.title, { exact: true })).toBeVisible();
    await expect(page.locator(".goodhr-admin-content .goodhr-section-panel")).toHaveCount(1);
  }
  await panel.getByRole("tab", { name: "AI配置", exact: true }).click();
  await page.getByRole("button", { name: "通知偏好", exact: true }).click();
  const notification = page.getByRole("dialog", { name: "小小打扰一下", exact: true });
  await expect(notification).toBeVisible();
  await notification.locator("button.ant-modal-close").click();
  await expect(notification).toBeHidden();
  await page.getByRole("button", { name: "保存配置", exact: true }).click();
  await expect(page.getByText("个人配置已保存", { exact: true })).toBeVisible();
  while (await page.locator(".ant-notification-notice").count()) {
    const previousCount = await page.locator(".ant-notification-notice").count();
    await page.locator(".ant-notification-notice-close").first().click();
    await expect(page.locator(".ant-notification-notice")).toHaveCount(previousCount - 1);
  }
  const screenshotDirectory = resolve("../../../output/resume-folders");
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "personal-config-neutral.png"), fullPage: true, animations: "disabled", mask: [page.getByLabel("API Key", { exact: true })], maskColor: "#eef1ef" });
});

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
