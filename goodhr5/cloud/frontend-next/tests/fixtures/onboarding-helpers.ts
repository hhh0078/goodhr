/** 文件作用：为教程端到端测试注册实际本机后端用户、控制隔离 Agent，并阻断全部外部业务请求。 */
import { expect, type APIRequestContext, type BrowserContext, type Page } from "@playwright/test";

export const CLOUD_URL = process.env.UI_TEST_CLOUD_URL || "http://127.0.0.1:8084";
export const AGENT_URL = "http://127.0.0.1:43130";
export const PREVIEW_URL = process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5173";
// Docker 教程预览使用独立端口 8085；拒绝将测试账号注册到非本机云端。
if (!["http://127.0.0.1:8084", "http://127.0.0.1:8085"].includes(CLOUD_URL)) throw new Error("教程测试只允许使用本机 8084 或 8085 后端");
export type TestUser = { email: string; password: string; token: string; id: string };

/** registerUser 通过实际 Go 验证码登录创建独立账号，开发发信器不会发送真实邮件。 */
export async function registerUser(request: APIRequestContext, prefix = "onboarding"): Promise<TestUser> {
  const email = `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}@qq.com`;
  const password = "tutorial-local-test-123";
  const send = await request.post(`${CLOUD_URL}/api/auth/send-code`, { data: { email } });
  expect(send.ok(), await send.text()).toBeTruthy();
  const sent = await send.json();
  expect(sent.debug_code, "测试只能连接使用开发验证码的本地后端").toBeTruthy();
  const login = await request.post(`${CLOUD_URL}/api/auth/login`, { data: { email, password, code: sent.debug_code, agreement_accepted: true } });
  expect(login.ok(), await login.text()).toBeTruthy();
  const value = await login.json();
  expect(value.access_token).toBeTruthy();
  return { email, password, token: value.access_token, id: value.user.id };
}

/** userStorageState 使用浏览器原生存储快照准备登录态，保留账号切换时既有教程缓存。 */
export function userStorageState(user: TestUser, existingStorage: { name: string; value: string }[] = []) {
  return { cookies: [], origins: [{ origin: new URL(PREVIEW_URL).origin, localStorage: [...existingStorage.filter((entry) => entry.name !== "goodhr5_access_token"), { name: "goodhr5_access_token", value: user.token }] }] };
}

/** agentControl 更新独立模拟程序的测试状态，控制接口即使离线状态也保持可用。 */
export async function agentControl(request: APIRequestContext, patch: Record<string, unknown>) {
  const result = await request.post(`${AGENT_URL}/__onboarding/control`, { data: patch });
  expect(result.ok(), await result.text()).toBeTruthy();
  return result.json();
}

/** readAgentState 查询去除凭证的接口调用记录，用于确认未运行真实组件。 */
export async function readAgentState(request: APIRequestContext) {
  const result = await request.get(`${AGENT_URL}/__onboarding/state`);
  expect(result.ok()).toBeTruthy();
  return result.json();
}

/** protectNetwork 只放行本机前端、实际 Go 后端和模拟 Agent，记录任何误探测正式端口。 */
export async function protectNetwork(context: BrowserContext, forbidden: string[]) {
  const allowed = new Set([new URL(PREVIEW_URL).origin, CLOUD_URL, AGENT_URL]);
  await context.route("**/*", (route) => {
    const url = new URL(route.request().url());
    if (allowed.has(url.origin)) return route.continue();
    if (url.port === "43129") forbidden.push(url.href);
    return route.abort("blockedbyclient");
  });
}

/** openAdmin 打开带隔离端口的实际后台，并等待首屏渲染完成。 */
export async function openAdmin(page: Page, path = "/admin") {
  await page.goto(`${PREVIEW_URL}${path}?local_port=43130`);
  await expect(page.locator("[data-admin-root]")).toBeVisible();
}

/** readUserFlow 读取实际云端教程事实，避免把界面动画当成完成证据。 */
export async function readUserFlow(request: APIRequestContext, user: TestUser) {
  const response = await request.get(`${CLOUD_URL}/api/user-flow`, { headers: { authorization: `Bearer ${user.token}` } });
  expect(response.ok()).toBeTruthy();
  return (await response.json()).flow;
}
