/** 本文件用本机页面和完全模拟接口验证专用附件提示词编辑、旧配置展示、字段保留及手机布局，不连接真实账号或 AI。 */
import { test, expect, type Page } from "@playwright/test";

const previewOrigin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
const defaultPrompt = "合成默认基础识别提示词：只提取原文明示事实。";
test.use({ storageState: { cookies: [], origins: [{ origin: previewOrigin, localStorage: [{ name: "goodhr5_access_token", value: "test-only-prompt-session" }] }] } });

/** mockPromptConfig 拦截全部业务请求，提示词保存仅更新测试内存，外部地址全部拒绝。 */
async function mockPromptConfig(page: Page, options: { defaultPromptFailure?: boolean } = {}) {
  const original = { filter_prompt: "原筛选提示词", open_detail_prompt: "原详情提示词", review_prompt: "原复核提示词", future_setting: { keep: true } };
  const state = { config: structuredClone(original) as Record<string, unknown>, errors: [] as string[], saves: 0 };
  page.on("pageerror", (error) => state.errors.push(error.message));
  await page.route("**/*", async (route) => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    if (url.origin === previewOrigin && !path.startsWith("/api/")) return route.continue();
    if (!["127.0.0.1", "localhost", "goodhr.58it.cn"].includes(url.hostname)) return route.abort();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
    if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (path === "/api/system/default-prompts" && options.defaultPromptFailure) return route.fulfill({ status: 503, headers, json: { error: "合成默认提示词读取失败" } });
    let data: Record<string, unknown> = { ok: true, items: [], config: {} };
    if (path === "/api/auth/me") data = { user: { id: "prompt-test", email: "prompt@example.invalid", role: "super_admin", has_openid: true } };
    else if (path === "/api/subscription/status") data = { subscription: { active: true, member_type: "max", remaining_days: 30 } };
    else if (path === "/api/wechat-official/status") data = { enabled: false, configured: false, bound: true, followed: true, gift_granted: true };
    else if (path === "/api/config/notification-profile") data = { profile: { completed: true } };
    else if (path === "/api/tenants/invitations/pending") data = { invitations: [] };
    else if (path === "/api/user-flow") data = { flow: { version: 2, state: "completed", stage: "completed", steps: {} } };
    else if (path === "/api/user-flow/state") data = { state: "completed", steps: {} };
    else if (path === "/api/config/user-ai") data = { config: { model: "qwen-test", enabled: true } };
    else if (path === "/api/v1/runtime/status") data = { version: "6.2.10.1", node_installed: true, camoufox_installed: true, system_runtime: { ready: true } };
    else if (path === "/api/v1/app-update/status") data = { stage: "idle", current_version: "6.2.10.1" };
    else if (path === "/health") data = { ok: true, version: "6.2.10.1" };
    else if (path === "/api/system/default-prompts") data = { prompts: { ...state.config, boss_attachment_basic_prompt: state.config.boss_attachment_basic_prompt || defaultPrompt } };
    else if (path === "/api/admin/system/configs/ai.default_prompts" && request.method() === "PUT") {
      state.config = JSON.parse(request.postDataJSON().config_value);
      state.saves++;
      data = { config: { config_key: "ai.default_prompts", config_value: JSON.stringify(state.config), enabled: true } };
    } else if (path === "/api/admin/system/configs/") data = { configs: [{ config_key: "ai.default_prompts", config_value: JSON.stringify(state.config), enabled: true }] };
    return route.fulfill({ status: 200, headers, json: data });
  });
  return { state, original };
}

test("公共默认提示词读取失败时仍加载并保存现有系统配置", async ({ page }) => {
  const { state, original } = await mockPromptConfig(page, { defaultPromptFailure: true });
  await page.goto(`${previewOrigin}/admin/system-config`, { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("menuitem", { name: /AI 默认提示词/ })).toBeVisible();
  const field = page.getByLabel("BOSS 附件基础识别提示词", { exact: true });
  await expect(field).toHaveValue("");
  await expect(field).toBeEnabled();
  await field.fill("默认接口失败后仍可保存的合成提示词");
  await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
  await expect.poll(() => state.saves).toBe(1);
  expect(state.config).toEqual({ ...original, boss_attachment_basic_prompt: "默认接口失败后仍可保存的合成提示词" });
  await expect(field).toHaveValue("默认接口失败后仍可保存的合成提示词");
  expect(state.errors).toEqual([]);
});

for (const width of [1440, 390]) {
  test(`附件基础提示词在现有管理项编辑并保留其它字段 ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    const { state, original } = await mockPromptConfig(page);
    await page.goto(`${previewOrigin}/admin/system-config`, { waitUntil: "domcontentloaded" });
    const field = page.getByLabel("BOSS 附件基础识别提示词", { exact: true });
    await expect(field).toHaveValue(defaultPrompt);
    await expect(page.getByRole("button", { name: "保存当前配置", exact: true })).toBeDisabled();
    await field.fill("合成管理员新基础提示词");
    await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
    await expect.poll(() => state.saves).toBe(1);
    expect(state.config).toEqual({ ...original, boss_attachment_basic_prompt: "合成管理员新基础提示词" });
    await expect(field).toHaveValue("合成管理员新基础提示词");
    const bounds = await field.boundingBox();
    expect(bounds).not.toBeNull();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
    await page.screenshot({ path: testInfo.outputPath(`boss-attachment-prompt-${width}.png`), fullPage: true, animations: "disabled" });
    await field.fill("字".repeat(20001));
    await expect(page.getByText("BOSS 附件基础识别提示词请填写文字，最多2万字；留空使用系统默认值", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "保存当前配置", exact: true })).toBeDisabled();
    expect(state.saves).toBe(1);
    expect(state.errors).toEqual([]);
  });
}
