/** 本文件用合成云端与本机接口验证 WorkBuddy 新用户授权、安装重查和登录恢复，不接触真实账号或招聘网站。 */
import { expect, test, type Page } from "@playwright/test";

const oauth = {
  client_id: "workbuddy-synthetic-client",
  redirect_uri: "http://localhost:18899/oauth/callback",
  state: "保留原始状态+&=",
  scope: "goodhr:recruiting",
  code_challenge: "synthetic-pkce-challenge",
  code_challenge_method: "S256",
};
const authorizePath = `/workbuddy/authorize?${new URLSearchParams(oauth)}`;
type Fixture = { installed: boolean; online: boolean; status: number; consentStatus: number; calls: string[]; consent: Record<string, unknown> | null };

/** mockFlow 拦截全部外部请求，只返回当前用例的合成连接状态和授权回调。 */
async function mockFlow(page: Page, patch: Partial<Fixture> = {}) {
  const fixture: Fixture = { installed: false, online: false, status: 200, consentStatus: 200, calls: [], consent: null, ...patch };
  await page.addInitScript(() => { localStorage.setItem("goodhr5_access_token", "synthetic-workbuddy-token"); });
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    /** reply 输出合成 JSON，并允许当前本机预览读取跨端口响应。 */
    const reply = (value: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", headers: { "Access-Control-Allow-Origin": "*" }, body: JSON.stringify(value) });
    if (route.request().method() === "OPTIONS") return reply({});
    if (path === "/api/agents/ws-status") {
      fixture.calls.push("ws-status");
      return reply(fixture.status === 200 ? { ok: true, connected: fixture.online } : { ok: false, error: "合成连接查询错误" }, fixture.status);
    }
    if (url.port === "43129") {
      fixture.calls.push(path);
      if (!fixture.installed) return route.abort("connectionrefused");
      if (path === "/health") return reply({ ok: true, version: "synthetic" });
      if (path === "/api/v1/cloud/session") fixture.online = true;
      return reply({ ok: true, data: { machine_id: "synthetic-machine" } });
    }
    if (path === "/api/workbuddy/authorize") {
      fixture.consent = route.request().postDataJSON();
      return reply(fixture.consentStatus === 200 ? { ok: true, redirect_url: `${oauth.redirect_uri}?code=synthetic-code&state=${encodeURIComponent(oauth.state)}` } : { ok: false, error: "session is invalid or expired" }, fixture.consentStatus);
    }
    if (path === "/login" || path === "/oauth/callback") return route.fulfill({ contentType: "text/html", body: "<main>合成登录或回调</main>" });
    if (url.hostname !== "127.0.0.1" || url.port === "8084") return route.abort();
    return route.continue();
  });
  return fixture;
}

test("未安装也能先授权，下载另开标签并完整透传 OAuth 参数", async ({ page }) => {
  const fixture = await mockFlow(page);
  await page.goto(authorizePath);
  await expect(page.getByText("本地程序还没连接", { exact: true })).toBeVisible();
  const download = page.getByRole("link", { name: "下载安装 GoodHR" });
  await expect(download).toHaveAttribute("href", "/download");
  await expect(download).toHaveAttribute("target", "_blank");
  await expect(page.getByRole("link", { name: "打开控制台准备" })).toHaveAttribute("target", "_blank");
  await page.getByRole("button", { name: "同意授权" }).click();
  await expect(page).toHaveURL(/localhost:18899\/oauth\/callback/);
  expect(fixture.consent).toEqual({ ...oauth, decision: "approve" });
  expect(fixture.calls).not.toContain("/api/v1/session/bind");
});

test("安装后重查复用绑定和云端同步，真实通道在线才显示连接成功", async ({ page }) => {
  const fixture = await mockFlow(page);
  await page.addInitScript(() => { Object.defineProperty(AbortSignal, "timeout", { value: undefined }); });
  await page.goto(authorizePath);
  await expect(page.getByText("本地程序还没连接", { exact: true })).toBeVisible();
  fixture.installed = true;
  await page.getByRole("button", { name: "安装后重新检查" }).click();
  await expect(page.getByText("本地程序已连接 WorkBuddy 任务通道", { exact: true })).toBeVisible();
  await expect(page.getByText("首次使用还要在控制台完成组件安装", { exact: false })).toBeVisible();
  const bind = fixture.calls.indexOf("/api/v1/session/bind");
  const sync = fixture.calls.indexOf("/api/v1/cloud/session");
  expect(bind).toBeGreaterThan(-1);
  expect(sync).toBeGreaterThan(bind);
  expect(fixture.calls[fixture.calls.length - 1]).toBe("ws-status");
});

test("云端状态字段缺失时提示重新检查，不误报离线或就绪", async ({ page }) => {
  await mockFlow(page);
  await page.route("**/api/agents/ws-status", (route) => route.fulfill({ contentType: "application/json", headers: { "Access-Control-Allow-Origin": "*" }, body: '{"ok":true}' }));
  await page.goto(authorizePath);
  await expect(page.getByText("连接状态暂时没查到", { exact: true })).toBeVisible();
  await expect(page.getByText("连接状态没读完整，请重新检查。", { exact: true })).toBeVisible();
});

test("检查期间在控制台重新登录后，可使用新令牌重新检查", async ({ page }) => {
  const fixture = await mockFlow(page);
  let releaseFirstStatus: () => void = () => {};
  const firstStatus = new Promise<void>((resolve) => { releaseFirstStatus = resolve; });
  let holdFirst = true;
  const credentials: string[] = [];
  await page.route("**/api/agents/ws-status", async (route) => {
    credentials.push(route.request().headers().authorization || "");
    if (!holdFirst) return route.fallback();
    holdFirst = false;
    await firstStatus;
    return route.fulfill({ contentType: "application/json", headers: { "Access-Control-Allow-Origin": "*" }, body: '{"ok":true,"connected":false}' });
  });
  await page.goto(authorizePath);
  await expect(page.getByText("我在看看你的本地程序连上了没有……", { exact: true })).toBeVisible();
  // 仅修改当前合成授权页的存储，模拟新标签完成登录，不访问任何招聘页面。
  await page.evaluate(() => { localStorage.setItem("goodhr5_access_token", "synthetic-replacement-token"); });
  releaseFirstStatus();
  await expect(page.getByText("登录状态已经更新，点“安装后重新检查”，我再确认一次连接。", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "安装后重新检查" })).toBeEnabled();
  fixture.online = true;
  await page.getByRole("button", { name: "安装后重新检查" }).click();
  await expect(page.getByText("本地程序已连接 WorkBuddy 任务通道", { exact: true })).toBeVisible();
  expect(credentials).toEqual(["Bearer synthetic-workbuddy-token", "Bearer synthetic-replacement-token"]);
  expect(fixture.calls).not.toContain("/api/v1/session/bind");
});

for (const stage of ["check", "consent"] as const) {
  test(`${stage} 收到401后重新登录，完整保留授权返回地址`, async ({ page }) => {
    await mockFlow(page, stage === "check" ? { status: 401 } : { online: true, consentStatus: 401 });
    await page.goto(authorizePath);
    if (stage === "consent") {
      await expect(page.getByText("本地程序已连接 WorkBuddy 任务通道", { exact: true })).toBeVisible();
      await page.getByRole("button", { name: "同意授权" }).click();
    }
    await expect(page).toHaveURL(/\/login\?next=/);
    expect(new URL(page.url()).searchParams.get("next")).toBe(authorizePath);
  });
}

test("云端查询失败显示可恢复错误，重新检查成功后不会保留旧错误", async ({ page }) => {
  const fixture = await mockFlow(page, { status: 503 });
  await page.goto(authorizePath);
  await expect(page.getByText("连接状态暂时没查到", { exact: true })).toBeVisible();
  await expect(page.getByText("本地程序还没连接", { exact: true })).toHaveCount(0);
  fixture.status = 200;
  fixture.online = true;
  await page.getByRole("button", { name: "安装后重新检查" }).click();
  await expect(page.getByText("本地程序已连接 WorkBuddy 任务通道", { exact: true })).toBeVisible();
  await expect(page.getByText("合成连接查询错误", { exact: true })).toHaveCount(0);
});

for (const width of [1366, 390]) {
  test(`未安装授权页在 ${width}px 宽度下保持操作可见`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await mockFlow(page);
    await page.goto(authorizePath);
    await expect(page.getByText("本地程序还没连接", { exact: true })).toBeVisible();
    for (const control of [page.getByRole("link", { name: "下载安装 GoodHR" }), page.getByRole("link", { name: "打开控制台准备" }), page.getByRole("button", { name: "安装后重新检查" }), page.getByRole("button", { name: "同意授权" })]) {
      await expect(control).toBeVisible();
      const box = await control.boundingBox();
      expect(box?.x).toBeGreaterThanOrEqual(0);
      expect((box?.x || 0) + (box?.width || 0)).toBeLessThanOrEqual(width);
    }
    await page.screenshot({ path: testInfo.outputPath(`workbuddy-authorize-${width}.png`), fullPage: true });
  });
}
