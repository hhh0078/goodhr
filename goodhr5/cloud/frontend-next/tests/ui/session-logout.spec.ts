/** 文件作用：用合成接口验证网页先撤销云端会话，本地离线、失败重试与退出期间切号均不会恢复或清错账号。 */
import { expect, test, type Page } from "@playwright/test";

const previewOrigin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
const token = "synthetic-logout-current";
type Fixture = { offline: boolean; status: number; gate?: Promise<void>; calls: Array<{ path: string; authorization: string }> };

/** mockLogout 仅放行本机前端静态资源，所有云端及 Agent 请求都由合成结果接管。 */
async function mockLogout(page: Page, offline = true) {
  const state: Fixture = { offline, status: 200, calls: [] };
  await page.addInitScript((value) => {
    localStorage.setItem("goodhr5_access_token", value);
    localStorage.setItem("goodhr5_session_email", "synthetic@example.invalid");
  }, token);
  await page.route("**/*", async (route) => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    if (url.origin === previewOrigin && !path.startsWith("/api/") && path !== "/health") return route.continue();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (!path.startsWith("/api/") && path !== "/health") return route.abort();
    state.calls.push({ path, authorization: request.headers().authorization || "" });
    /** reply 返回本轮合成 JSON，绝不连接真实云端或用户本地程序。 */
    const reply = (json: unknown, status = 200) => route.fulfill({ status, headers, json });
    if (url.port === "43139" || url.port === "43129") {
      if (state.offline) return route.abort("connectionrefused");
      if (path === "/health") return reply({ version: "6.3.4.14" });
      if (path === "/api/v1/runtime/status") return reply({ data: { version: "6.3.4.14", node_installed: true, camoufox_installed: true, system_runtime: { required: false, ready: true } } });
      return reply({ data: { ok: true } });
    }
    if (path === "/api/auth/logout") {
      await state.gate;
      return reply(state.status === 200 ? { ok: true } : { error: "合成云端退出暂未完成" }, state.status);
    }
    if (path === "/api/auth/me") return reply({ user: { id: "synthetic-logout", email: "synthetic@example.invalid", role: "admin" } });
    if (path === "/api/user-flow") return reply({ flow: { state: "completed", stage: "completed", steps: {} } });
    if (path === "/api/config/notification-profile") return reply({ profile: { completed: true } });
    if (path === "/api/auth/wechat/status") return reply({ followed: true, reward_claimed: true });
    if (path === "/api/runtime/config" || path === "/api/system/app-config") return reply({ config: {} });
    if (path === "/api/subscription/status") return reply({ subscription: { active: true } });
    return reply({ ok: true, positions: [], invitations: [], accounts: [], platforms: [], messages: [] });
  });
  await page.goto("/admin/positions");
  await expect(page.locator("[data-admin-root]")).toBeVisible();
  if (offline) {
    const notice = page.getByRole("dialog", { name: "请先安装本地程序", exact: true });
    await expect(notice).toBeVisible();
    await notice.getByRole("button", { name: "关闭", exact: true }).click();
    await expect(notice).toBeHidden();
  } else await expect(page.getByTestId("local-agent-status")).toContainText("6.3.4.14");
  if (page.viewportSize()!.width < 600) {
    await page.getByRole("button", { name: "打开菜单", exact: true }).click();
    await expect(page.locator(".ant-drawer-content-wrapper")).toHaveCSS("transform", "none");
  }
  await expect(page.getByRole("button", { name: "退出登录", exact: true }).filter({ visible: true })).toBeVisible();
  return state;
}

/** storedToken 用浏览器原生存储快照检查凭证，不把真实数据或令牌写入验收输出。 */
async function storedToken(page: Page) {
  return (await page.context().storageState()).origins.find((entry) => entry.origin === previewOrigin)?.localStorage.find((item) => item.name === "goodhr5_access_token")?.value;
}

for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
  test(`本地程序关闭时仍先撤销云端再退出网页 ${viewport.width}`, async ({ page }, testInfo) => {
    await page.setViewportSize(viewport);
    const state = await mockLogout(page);
    await page.screenshot({ path: testInfo.outputPath(`logout-${viewport.width}.png`), fullPage: true });
    await page.getByRole("button", { name: "退出登录", exact: true }).filter({ visible: true }).click();
    await expect(page).toHaveURL(/\/login(?:\?|$)/);
    expect(await storedToken(page)).toBeUndefined();
    expect(state.calls.filter((call) => call.path === "/api/auth/logout")).toEqual([{ path: "/api/auth/logout", authorization: `Bearer ${token}` }]);
    expect(state.calls.filter((call) => call.path === "/api/v1/session/unbind")).toEqual([]);
  });
}

test("本地程序在退出前关闭，通知失败仍保留云端撤销结果", async ({ page }) => {
  const state = await mockLogout(page, false);
  state.offline = true;
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  expect(await storedToken(page)).toBeUndefined();
  const logoutIndex = state.calls.findIndex((call) => call.path === "/api/auth/logout");
  expect(state.calls.findIndex((call) => call.path === "/api/v1/session/unbind")).toBeGreaterThan(logoutIndex);
});

test("云端退出失败保留账号并支持重试，不提前通知本地退出", async ({ page }) => {
  const state = await mockLogout(page);
  state.status = 503;
  const button = page.getByRole("button", { name: "退出登录", exact: true });
  await button.click();
  await expect(page.getByText("云端退出还没确认好，账号先保留着，请再点一次退出。", { exact: true })).toBeVisible();
  expect(await storedToken(page)).toBe(token);
  await expect(page).toHaveURL(/\/admin\/positions/);
  expect(state.calls.filter((call) => call.path === "/api/v1/session/unbind")).toEqual([]);
  state.status = 200;
  await button.click();
  await expect(page).toHaveURL(/\/login(?:\?|$)/);
  expect(await storedToken(page)).toBeUndefined();
});

test("退出等待期间切到新账号，不清除新账号或重复发送退出", async ({ page }) => {
  const state = await mockLogout(page);
  let release!: () => void;
  state.gate = new Promise<void>((resolve) => { release = resolve; });
  const button = page.getByRole("button", { name: "退出登录", exact: true });
  await button.click();
  await expect.poll(() => state.calls.filter((call) => call.path === "/api/auth/logout").length).toBe(1);
  await page.keyboard.press("Enter");
  // 只操作合成 GoodHR 页面缓存，模拟另一个标签页完成登录；不访问招聘网站。
  await page.evaluate(() => localStorage.setItem("goodhr5_access_token", "synthetic-new-account"));
  release();
  await expect(button).not.toHaveClass(/ant-btn-loading/);
  expect(await storedToken(page)).toBe("synthetic-new-account");
  await expect(page).toHaveURL(/\/admin\/positions/);
  expect(state.calls.filter((call) => call.path === "/api/auth/logout")).toHaveLength(1);
  expect(state.calls.filter((call) => call.path === "/api/v1/session/unbind")).toEqual([]);
});
