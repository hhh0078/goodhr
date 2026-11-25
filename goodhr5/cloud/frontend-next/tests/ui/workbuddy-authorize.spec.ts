/** 本文件用合成云端验证 WorkBuddy 先选账号、严格注册或登录后自动绑定、失败重试及响应布局，不接触真实账号。 */
import { expect, test, type Page } from "@playwright/test";

const oauth = {
  client_id: "workbuddy-synthetic-client", redirect_uri: "http://localhost:18899/oauth/callback",
  state: "保留原始状态+&=", scope: "goodhr:recruiting", code_challenge: "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", code_challenge_method: "S256",
};
const authorizePath = `/workbuddy/authorize?${new URLSearchParams(oauth)}`;
type Fixture = {
  cached: boolean; installed: boolean; online: boolean; newAccount: boolean; authStatus: number; authError: string;
  consentStatus: number; requiresPasswordSetup: boolean; wechatApproved: boolean; requests: Array<{ path: string; body: Record<string, unknown>; authorization: string }>;
  preflightStatus: number; preflightError: string; clientName: string; malformedMetadata: boolean; preflights: Array<Record<string, string>>;
  wsStatus: number; syncStatus: number; syncGate?: Promise<void>; onSyncStarted?: () => void;
};

/** mockFlow 完全隔离认证、验证码、本机连接与OAuth请求，旧缓存只属于当前合成页面。 */
async function mockFlow(page: Page, patch: Partial<Fixture> = {}) {
  const fixture: Fixture = { cached: true, installed: false, online: false, newAccount: true, authStatus: 200, authError: "合成注册暂未完成，请重试", consentStatus: 200, requiresPasswordSetup: false, wechatApproved: false, requests: [], preflightStatus: 200, preflightError: "这个客户端没有注册过，请重新发起连接。", clientName: "合成招聘助手", malformedMetadata: false, preflights: [], wsStatus: 200, syncStatus: 200, ...patch };
  await page.addInitScript((cached) => {
    if (cached) localStorage.setItem("goodhr5_access_token", "synthetic-old-token");
    else localStorage.removeItem("goodhr5_access_token");
    localStorage.setItem("goodhr5_invite_id", "synthetic-inviter");
  }, fixture.cached);
  await page.route("**/*", async (route) => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    /** reply 只返回合成接口结果，避免浏览器连接实际服务。 */
    const reply = (json: unknown, status = 200) => route.fulfill({ status, headers, json });
    if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (path.startsWith("/api/") || path === "/health") {
      if (path === "/api/workbuddy/authorize" && request.method() === "GET") {
        fixture.preflights.push(Object.fromEntries(url.searchParams));
        return reply(fixture.preflightStatus !== 200 ? { ok: false, error: fixture.preflightError } : fixture.malformedMetadata ? { ok: true, client_name: fixture.clientName } : { ok: true, client_name: fixture.clientName, redirect_uri: url.searchParams.get("redirect_uri"), callback_origin: "http://localhost:18899" }, fixture.preflightStatus);
      }
      fixture.requests.push({ path, body: request.postDataJSON() || {}, authorization: request.headers().authorization || "" });
      if (path === "/api/auth/send-code") return reply({ ok: true, is_new_account: fixture.newAccount });
      if (path === "/api/auth/agreement-status") return reply({ agreement_accepted: false });
      if (path === "/api/auth/login" || path === "/api/auth/password-login") return reply(fixture.authStatus === 200 ? { access_token: "synthetic-new-token", user: { email: "synthetic@example.invalid" }, requires_password_setup: fixture.requiresPasswordSetup } : { ok: false, error: fixture.authError }, fixture.authStatus);
      if (path === "/api/auth/password/setup") return reply({ ok: true });
      if (path === "/api/auth/wechat/create") return reply({ ticket: "a".repeat(64), login_code: "12345678", qr_image_url: "/icon.png", expires_at: new Date(Date.now() + 60000).toISOString() });
      if (path === "/api/auth/wechat/poll") {
        if (!fixture.wechatApproved) return reply({ status: "waiting" });
        if (!request.postDataJSON()?.agreement_accepted) return reply({ status: "agreement_required" });
        return reply({ access_token: "synthetic-new-token", user: { email: "synthetic@example.invalid" }, requires_password_setup: false });
      }
      if (path === "/api/agents/ws-status") return reply(fixture.wsStatus === 200 ? { ok: true, connected: fixture.online } : { ok: false, error: "合成会话已过期" }, fixture.wsStatus);
      if (path === "/api/workbuddy/authorize") return reply(fixture.consentStatus === 200 ? { ok: true, redirect_url: `${oauth.redirect_uri}?code=synthetic-code&state=${encodeURIComponent(oauth.state)}` } : { ok: false, error: "合成授权暂未完成，可以再试一次" }, fixture.consentStatus);
      if (url.port === "43129") {
        if (!fixture.installed) return route.abort("connectionrefused");
        if (path === "/api/v1/cloud/session") {
          fixture.onSyncStarted?.();
          await fixture.syncGate;
          if (fixture.syncStatus !== 200) return reply({ ok: false, error: "本机同步暂时没完成，可以稍后再试" }, fixture.syncStatus);
          fixture.online = true;
        }
        return reply({ ok: true, data: { machine_id: "synthetic-device" }, version: "synthetic" });
      }
      return route.abort();
    }
    if (path === "/oauth/callback") return route.fulfill({ contentType: "text/html; charset=utf-8", body: "<main>合成 WorkBuddy 回调</main>" });
    if (url.hostname !== "127.0.0.1") return route.abort();
    return route.continue();
  });
  return fixture;
}

/** confirmAuthorization 核对服务端返回的实际接收方后，显式同意本次应用权限。 */
async function confirmAuthorization(page: Page) {
  await page.getByRole("checkbox", { name: "我确认上方应用和回调地址，并同意以上权限", exact: true }).check();
}

/** fillExisting 填好用户明确选择的邮箱密码登录，不提前授予OAuth权限。 */
async function fillExisting(page: Page) {
  await confirmAuthorization(page);
  await page.getByRole("button", { name: "我有账号", exact: true }).click();
  await page.getByLabel("邮箱", { exact: true }).fill("synthetic@example.invalid");
  await page.getByLabel("密码", { exact: true }).fill("synthetic-password");
  await page.getByRole("checkbox", { name: "我已阅读并同意", exact: true }).check();
}

/** fillRegistration 复用现有验证码与密码确认流程，所有邮件请求均被合成接口接管。 */
async function fillRegistration(page: Page) {
  await confirmAuthorization(page);
  await page.getByRole("button", { name: "我还没有账号", exact: true }).click();
  await page.getByLabel("邮箱", { exact: true }).fill("synthetic@example.invalid");
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await page.getByLabel("验证码", { exact: true }).fill("1234");
  await page.getByLabel("设置登录密码", { exact: true }).fill("synthetic-password");
  await page.getByLabel("再输入一次密码", { exact: true }).fill("synthetic-password");
}

/** fillExistingCode 填写既有账号的验证码登录，供补密码及切换账号的边界验收复用。 */
async function fillExistingCode(page: Page) {
  await confirmAuthorization(page);
  await page.getByRole("button", { name: "我有账号", exact: true }).click();
  await page.getByRole("button", { name: "邮箱验证码", exact: true }).click();
  await page.getByLabel("邮箱", { exact: true }).fill("synthetic@example.invalid");
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await page.getByLabel("验证码", { exact: true }).fill("1234");
  await page.getByRole("checkbox", { name: "我已阅读并同意", exact: true }).check();
}

for (const cached of [false, true]) {
  test(`浏览器${cached ? "有" : "无"}旧会话都先询问账号，未选择前不登录或授权`, async ({ page }) => {
    const fixture = await mockFlow(page, { cached });
    await page.goto(authorizePath);
    await expect(page.getByText("你已经有 GoodHR 账号了吗？", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "我有账号", exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "我还没有账号", exact: true })).toBeVisible();
    await expect(page.getByRole("checkbox", { name: "我确认上方应用和回调地址，并同意以上权限", exact: true })).not.toBeChecked();
    expect(fixture.preflights).toEqual([oauth]);
    expect(fixture.requests).toEqual([]);
    await expect(page).toHaveURL(new RegExp("/workbuddy/authorize\\?"));
  });
}

for (const scenario of [
  { name: "未知客户端", status: 404, error: "这个客户端没有注册过，请重新发起连接。", overrides: { client_id: "unknown-client" } },
  { name: "危险回调", status: 400, error: "这个回调地址暂时不能用于授权。", overrides: { redirect_uri: "javascript:alert(1)" } },
  { name: "元数据服务故障", status: 503, error: "授权接收方暂时没查到，请稍后重试。", overrides: {} },
]) {
  test(`${scenario.name}预检失败不展示账号入口，也不发起注册登录`, async ({ page }) => {
    const fixture = await mockFlow(page, { preflightStatus: scenario.status, preflightError: scenario.error });
    await page.goto(`/workbuddy/authorize?${new URLSearchParams({ ...oauth, ...scenario.overrides })}`);
    await expect(page.getByText(scenario.error, { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "我有账号", exact: true })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "我还没有账号", exact: true })).toHaveCount(0);
    await expect(page.getByLabel("邮箱", { exact: true })).toHaveCount(0);
    expect(fixture.requests).toEqual([]);
  });
}

test("预检返回不完整元数据时保持关闭，不能先注册再发现回调缺失", async ({ page }) => {
  const fixture = await mockFlow(page, { malformedMetadata: true });
  await page.goto(authorizePath);
  await expect(page.getByText("授权接收方信息没有读完整，请重新检查授权链接。", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "我有账号", exact: true })).toHaveCount(0);
  expect(fixture.requests).toEqual([]);
});

test("客户端自报名称不冒充官方身份，未确认授权时不加载登录或微信认证", async ({ page }) => {
  const fixture = await mockFlow(page, { clientName: "WorkBuddy 官方 <img src=x>" });
  await page.goto(authorizePath);
  await expect(page.getByRole("heading", { name: "连接你的 GoodHR 账号", exact: true })).toBeVisible();
  await expect(page.getByText("WorkBuddy 官方 <img src=x>（客户端自报）", { exact: false })).toBeVisible();
  await expect(page.getByText("回调来源：http://localhost:18899", { exact: true })).toBeVisible();
  await expect(page.locator('img[src="x"]')).toHaveCount(0);
  await page.getByRole("button", { name: "我有账号", exact: true }).click();
  await expect(page.getByText("请先确认上方授权接收方、回调地址和权限，再继续。", { exact: true })).toBeVisible();
  await expect(page.getByLabel("邮箱", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "登录并绑定", exact: true })).toHaveCount(0);
  expect(fixture.requests).toEqual([]);
  await confirmAuthorization(page);
  await expect(page.getByRole("button", { name: "登录并绑定", exact: true })).toBeVisible();
  await page.getByRole("checkbox", { name: "我确认上方应用和回调地址，并同意以上权限", exact: true }).uncheck();
  await expect(page.getByLabel("邮箱", { exact: true })).toHaveCount(0);
  expect(fixture.requests).toEqual([]);
});

test("已有账号密码登录成功后使用新会话自动继续OAuth绑定，完整保留参数", async ({ page }) => {
  const fixture = await mockFlow(page, { newAccount: false });
  await page.goto(authorizePath);
  await fillExisting(page);
  await expect(page.getByRole("button", { name: "微信扫码", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "邮箱验证码", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
  await expect(page.getByText("合成 WorkBuddy 回调", { exact: true })).toBeVisible();
  const login = fixture.requests.filter((item) => item.path === "/api/auth/password-login");
  const consent = fixture.requests.filter((item) => item.path === "/api/workbuddy/authorize");
  expect(login).toHaveLength(1);
  expect(login[0].body).toMatchObject({ email: "synthetic@example.invalid", password: "synthetic-password", agreement_accepted: true });
  expect(consent).toHaveLength(1);
  expect(consent[0].body).toEqual({ ...oauth, decision: "approve" });
  expect(consent[0].authorization).toBe("Bearer synthetic-new-token");
});

test("已有账号验证码登录需补密码时，设置完成后才自动绑定", async ({ page }) => {
  const fixture = await mockFlow(page, { cached: false, newAccount: false, requiresPasswordSetup: true });
  await page.goto(authorizePath);
  await fillExistingCode(page);
  await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
  const setup = page.getByRole("dialog", { name: "给老账号补一个密码", exact: true });
  await expect(setup).toBeVisible();
  expect(fixture.requests.some((item) => item.path === "/api/workbuddy/authorize")).toBe(false);
  await setup.getByLabel("新密码", { exact: true }).fill("synthetic-new-password");
  await setup.getByLabel("再输入一次", { exact: true }).fill("synthetic-new-password");
  await setup.getByRole("button", { name: "设置密码并继续", exact: true }).click();
  await expect(page.getByText("合成 WorkBuddy 回调", { exact: true })).toBeVisible();
  const login = fixture.requests.find((item) => item.path === "/api/auth/login");
  expect(login?.body.password).toBe("");
  expect(login?.body).not.toHaveProperty("register_only");
});

test("补密码时其他标签换成B账号，不对B设置密码或授权", async ({ page }) => {
  const fixture = await mockFlow(page, { newAccount: false, requiresPasswordSetup: true });
  await page.goto(authorizePath);
  await fillExistingCode(page);
  await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
  const setup = page.getByRole("dialog", { name: "给老账号补一个密码", exact: true });
  await expect(setup).toBeVisible();
  await page.evaluate(() => localStorage.setItem("goodhr5_access_token", "synthetic-account-b-token"));
  await setup.getByLabel("新密码", { exact: true }).fill("synthetic-new-password");
  await setup.getByLabel("再输入一次", { exact: true }).fill("synthetic-new-password");
  await setup.getByRole("button", { name: "设置密码并继续", exact: true }).click();
  await expect(page.getByText("登录账号已经变化，请重新选择账号并确认授权。", { exact: true })).toBeVisible();
  expect(fixture.requests.some((item) => item.path === "/api/auth/password/setup" || item.path === "/api/workbuddy/authorize")).toBe(false);
  expect(await page.evaluate(() => localStorage.getItem("goodhr5_access_token"))).toBe("synthetic-account-b-token");
});

test("已有账号登录失败不使用旧令牌授权，可保留密码重试", async ({ page }) => {
  const fixture = await mockFlow(page, { newAccount: false, authStatus: 401, authError: "邮箱或密码不正确" });
  await page.goto(authorizePath);
  await fillExisting(page);
  await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
  await expect(page.getByText("邮箱或密码不正确", { exact: true })).toBeVisible();
  await expect(page.getByLabel("密码", { exact: true })).toHaveValue("synthetic-password");
  expect(fixture.requests.some((item) => item.path === "/api/workbuddy/authorize")).toBe(false);
  fixture.authStatus = 200;
  await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
  await expect(page.getByText("合成 WorkBuddy 回调", { exact: true })).toBeVisible();
});

test("微信登录沿用扫码确认和协议确认，成功后接回同一绑定流程", async ({ page }) => {
  const fixture = await mockFlow(page);
  await page.goto(authorizePath);
  await confirmAuthorization(page);
  await page.getByRole("button", { name: "我有账号", exact: true }).click();
  await page.getByRole("button", { name: "微信扫码", exact: true }).click();
  await expect(page.getByText("验证码 12345678", { exact: true })).toBeVisible();
  fixture.wechatApproved = true;
  const agreement = page.getByRole("button", { name: "微信已确认，读完并同意协议就能进入", exact: true });
  await expect(agreement).toBeVisible();
  expect(fixture.requests.some((item) => item.path === "/api/workbuddy/authorize")).toBe(false);
  await agreement.click();
  await page.getByRole("dialog", { name: "GoodHR 使用协议与隐私说明", exact: true }).getByRole("button", { name: "我已阅读", exact: true }).click();
  await expect(page.getByText("合成 WorkBuddy 回调", { exact: true })).toBeVisible();
  expect(fixture.requests.filter((item) => item.path === "/api/workbuddy/authorize")).toHaveLength(1);
});

test("新账号验证码注册成功后自动绑定，保留邀请码并使用register_only", async ({ page }) => {
  const fixture = await mockFlow(page, { cached: false });
  await page.goto(authorizePath);
  await fillRegistration(page);
  await expect(page.getByRole("checkbox", { name: "我已阅读并同意", exact: true })).not.toBeChecked();
  await page.getByRole("checkbox", { name: "我已阅读并同意", exact: true }).check();
  await page.getByRole("button", { name: "注册并绑定", exact: true }).click();
  await expect(page.getByText("合成 WorkBuddy 回调", { exact: true })).toBeVisible();
  const registration = fixture.requests.filter((item) => item.path === "/api/auth/login");
  expect(registration).toHaveLength(1);
  expect(registration[0].body).toEqual({ email: "synthetic@example.invalid", code: "1234", password: "synthetic-password", inviter_id: "synthetic-inviter", agreement_accepted: true, register_only: true });
  expect(fixture.requests.filter((item) => item.path === "/api/auth/password-login")).toHaveLength(0);
  const stored = await page.evaluate(() => ({ token: localStorage.getItem("goodhr5_access_token"), inviter: localStorage.getItem("goodhr5_invite_id") }));
  // 回调使用不同源，浏览器会将账号令牌留在GoodHR原站，不带到回调URL。
  expect(new URL(page.url()).searchParams.has("access_token")).toBe(false);
  expect(stored.token).toBeNull();
});

test("注册重复邮箱明确提示，不自动登录或拿旧缓存会话授权", async ({ page }) => {
  const fixture = await mockFlow(page, { authStatus: 409, authError: "这个邮箱已经有账号了，请选择“已有账号”登录。" });
  await page.goto(authorizePath);
  await fillRegistration(page);
  await page.getByRole("checkbox", { name: "我已阅读并同意", exact: true }).check();
  await page.getByRole("button", { name: "注册并绑定", exact: true }).click();
  await expect(page.getByText(fixture.authError, { exact: true })).toBeVisible();
  await expect(page.getByLabel("设置登录密码", { exact: true })).toHaveValue("synthetic-password");
  expect(fixture.requests.filter((item) => item.path === "/api/auth/password-login" || item.path === "/api/workbuddy/authorize")).toHaveLength(0);
  expect(await page.evaluate(() => localStorage.getItem("goodhr5_access_token"))).toBe("synthetic-old-token");
});

test("注册失败保留输入和邀请码，再点一次即可重试并绑定", async ({ page }) => {
  const fixture = await mockFlow(page, { cached: false, authStatus: 503 });
  await page.goto(authorizePath);
  await fillRegistration(page);
  await page.getByRole("checkbox", { name: "我已阅读并同意", exact: true }).check();
  await page.getByRole("button", { name: "注册并绑定", exact: true }).click();
  await expect(page.getByText(fixture.authError, { exact: true })).toBeVisible();
  expect(fixture.requests.some((item) => item.path === "/api/workbuddy/authorize")).toBe(false);
  expect(await page.evaluate(() => localStorage.getItem("goodhr5_invite_id"))).toBe("synthetic-inviter");
  fixture.authStatus = 200;
  await page.getByRole("button", { name: "注册并绑定", exact: true }).click();
  await expect(page.getByText("合成 WorkBuddy 回调", { exact: true })).toBeVisible();
  expect(fixture.requests.filter((item) => item.path === "/api/auth/login")).toHaveLength(2);
});

test("注册协议预览不提交，提交后的协议确认会继续注册绑定", async ({ page }) => {
  const fixture = await mockFlow(page, { cached: false });
  await page.goto(authorizePath);
  await fillRegistration(page);
  await page.getByRole("button", { name: "《使用协议与隐私说明》", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "GoodHR 使用协议与隐私说明", exact: true });
  await expect(dialog.getByRole("button", { name: "我已阅读", exact: true })).toBeEnabled();
  await dialog.getByRole("button", { name: "先不登录", exact: true }).click();
  expect(fixture.requests.some((item) => item.path === "/api/auth/login")).toBe(false);
  await page.getByRole("button", { name: "注册并绑定", exact: true }).click();
  await expect(dialog).toBeVisible();
  expect(fixture.requests.some((item) => item.path === "/api/workbuddy/authorize")).toBe(false);
  await dialog.getByRole("button", { name: "我已阅读", exact: true }).click();
  await expect(page.getByText("合成 WorkBuddy 回调", { exact: true })).toBeVisible();
});

test("注册完成但OAuth失败时只重试绑定，不重新注册", async ({ page }) => {
  const fixture = await mockFlow(page, { cached: false, consentStatus: 503 });
  await page.goto(authorizePath);
  await fillRegistration(page);
  await page.getByRole("checkbox", { name: "我已阅读并同意", exact: true }).check();
  await page.getByRole("button", { name: "注册并绑定", exact: true }).click();
  await expect(page.getByText("合成授权暂未完成，可以再试一次", { exact: true })).toBeVisible();
  await expect(page.getByText("本地程序还没连接", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "下载安装 GoodHR" })).toHaveAttribute("target", "_blank");
  fixture.consentStatus = 200;
  await page.getByRole("button", { name: "同意授权", exact: true }).click();
  await expect(page.getByText("合成 WorkBuddy 回调", { exact: true })).toBeVisible();
  expect(fixture.requests.filter((item) => item.path === "/api/auth/login")).toHaveLength(1);
  expect(fixture.requests.filter((item) => item.path === "/api/workbuddy/authorize")).toHaveLength(2);
});

test("OAuth失败后授权确认恢复可撤回，撤勾不能再次approve", async ({ page }) => {
  const fixture = await mockFlow(page, { consentStatus: 503 });
  await page.goto(authorizePath);
  await fillExisting(page);
  await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
  await expect(page.getByText("合成授权暂未完成，可以再试一次", { exact: true })).toBeVisible();
  const confirmation = page.getByRole("checkbox", { name: "我确认上方应用和回调地址，并同意以上权限", exact: true });
  await expect(confirmation).toBeEnabled();
  await confirmation.uncheck();
  await expect(page.getByRole("button", { name: "同意授权", exact: true })).toBeDisabled();
  expect(fixture.requests.filter((item) => item.path === "/api/workbuddy/authorize")).toHaveLength(1);
});

test("A账号授权失败后其他标签切到B，重试必须重选身份且不清B会话", async ({ page }) => {
  const fixture = await mockFlow(page, { consentStatus: 503 });
  await page.goto(authorizePath);
  await fillExisting(page);
  await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
  await expect(page.getByText("合成授权暂未完成，可以再试一次", { exact: true })).toBeVisible();
  await page.evaluate(() => localStorage.setItem("goodhr5_access_token", "synthetic-account-b-token"));
  await page.getByRole("button", { name: "同意授权", exact: true }).click();
  await expect(page.getByText("登录账号已经变化，请重新选择账号并确认授权。", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "我有账号", exact: true })).toBeVisible();
  expect(fixture.requests.filter((item) => item.path === "/api/workbuddy/authorize")).toHaveLength(1);
  expect(await page.evaluate(() => localStorage.getItem("goodhr5_access_token"))).toBe("synthetic-account-b-token");
});

test("认证后安装GoodHR可以重新检查，沿用绑定和云端同步", async ({ page }) => {
  const fixture = await mockFlow(page, { cached: false, consentStatus: 503 });
  await page.goto(authorizePath);
  await fillExisting(page);
  await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
  await expect(page.getByText("本地程序还没连接", { exact: true })).toBeVisible();
  fixture.installed = true;
  await page.getByRole("button", { name: "安装后重新检查", exact: true }).click();
  await expect(page.getByText("本地程序已连接任务通道", { exact: true })).toBeVisible();
  const paths = fixture.requests.map((item) => item.path);
  expect(paths.indexOf("/api/v1/session/bind")).toBeGreaterThan(-1);
  expect(paths.indexOf("/api/v1/cloud/session")).toBeGreaterThan(paths.indexOf("/api/v1/session/bind"));
});

test("已安装且已有在线通道时，也等当前会话同步受理后才跳回调", async ({ page }) => {
  let releaseSync: () => void = () => {};
  let markSyncStarted: () => void = () => {};
  const syncGate = new Promise<void>((resolve) => { releaseSync = resolve; });
  const syncStarted = new Promise<void>((resolve) => { markSyncStarted = resolve; });
  const fixture = await mockFlow(page, { installed: true, online: true, syncGate, onSyncStarted: markSyncStarted });
  await page.goto(authorizePath);
  await fillExisting(page);
  await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
  await syncStarted;
  expect(fixture.requests.some((item) => item.path === "/api/workbuddy/authorize")).toBe(false);
  await expect(page).toHaveURL(new RegExp("/workbuddy/authorize\\?"));
  releaseSync();
  await expect(page.getByText("合成 WorkBuddy 回调", { exact: true })).toBeVisible();
  const paths = fixture.requests.map((item) => item.path);
  expect(paths.indexOf("/api/v1/session/bind")).toBeLessThan(paths.indexOf("/api/v1/cloud/session"));
  expect(paths.indexOf("/api/v1/cloud/session")).toBeLessThan(paths.indexOf("/api/workbuddy/authorize"));
  expect(fixture.requests.find((item) => item.path === "/api/v1/cloud/session")?.body).toEqual({ token: "synthetic-new-token" });
});

test("已安装但本机同步失败时提示可后续处理，不阻断OAuth绑定", async ({ page }) => {
  const fixture = await mockFlow(page, { installed: true, syncStatus: 503 });
  await page.goto(authorizePath);
  await fillExisting(page);
  await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
  await expect(page.getByText("合成 WorkBuddy 回调", { exact: true })).toBeVisible();
  expect(fixture.requests.some((item) => item.path === "/api/v1/cloud/session")).toBe(true);
  expect(fixture.requests.filter((item) => item.path === "/api/workbuddy/authorize")).toHaveLength(1);
});

for (const stage of ["connection", "consent"]) {
  test(`${stage}返回401时留在授权页重新选择账号，不循环跳登录页`, async ({ page }) => {
    const fixture = await mockFlow(page, stage === "connection" ? { wsStatus: 401 } : { consentStatus: 401 });
    await page.goto(authorizePath);
    await fillExisting(page);
    await page.getByRole("button", { name: "登录并绑定", exact: true }).click();
    await expect(page.getByText("登录状态过期了，请重新选择账号并确认本次授权。", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "我有账号", exact: true })).toBeVisible();
    await expect(page.getByRole("checkbox", { name: "我确认上方应用和回调地址，并同意以上权限", exact: true })).not.toBeChecked();
    await expect(page).toHaveURL(new RegExp("/workbuddy/authorize\\?"));
    expect(fixture.requests.filter((item) => item.path === "/api/workbuddy/authorize")).toHaveLength(stage === "connection" ? 0 : 1);
  });
}

for (const cached of [false, true]) {
  test(`普通登录页${cached ? "已有会话直接进入控制台" : "邮箱密码登录仍进入控制台"}，不走WorkBuddy绑定`, async ({ page }) => {
    const fixture = await mockFlow(page, { cached, newAccount: false });
    await page.route("**/admin/", (route) => route.fulfill({ contentType: "text/html; charset=utf-8", body: "<main>合成控制台</main>" }));
    await page.goto("/login");
    if (!cached) {
      await page.getByRole("button", { name: "邮箱密码", exact: true }).click();
      await page.getByLabel("邮箱", { exact: true }).fill("synthetic@example.invalid");
      await page.getByLabel("密码", { exact: true }).fill("synthetic-password");
      await page.getByRole("checkbox", { name: "我已阅读并同意", exact: true }).check();
      await page.getByRole("button", { name: "密码登录", exact: true }).click();
    }
    await expect(page.getByText("合成控制台", { exact: true })).toBeVisible();
    expect(fixture.requests.some((item) => item.path === "/api/workbuddy/authorize")).toBe(false);
  });
}

test("已存在邮箱在验证码阶段提示返回登录，不绕过严格注册", async ({ page }) => {
  const fixture = await mockFlow(page, { newAccount: false });
  await page.goto(authorizePath);
  await confirmAuthorization(page);
  await page.getByRole("button", { name: "我还没有账号", exact: true }).click();
  await page.getByLabel("邮箱", { exact: true }).fill("existing@example.invalid");
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await expect(page.getByText("这个邮箱已经有账号了，请返回选择“我有账号”登录。", { exact: true })).toBeVisible();
  expect(fixture.requests.some((item) => item.path === "/api/auth/login" || item.path === "/api/workbuddy/authorize")).toBe(false);
  await page.getByRole("button", { name: "重新选择", exact: true }).click();
  await expect(page.getByRole("button", { name: "我有账号", exact: true })).toBeVisible();
});

for (const width of [1366, 390]) {
  test(`账号选择与注册绑定表单在${width}px可见且不越界`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 900 });
    await mockFlow(page);
    await page.goto(authorizePath);
    await expect(page.getByRole("button", { name: "我有账号", exact: true })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`workbuddy-choice-${width}.png`), fullPage: true, animations: "disabled" });
    await confirmAuthorization(page);
    await page.getByRole("button", { name: "我还没有账号", exact: true }).click();
    await expect(page.getByLabel("设置登录密码", { exact: true })).toBeVisible();
    for (const control of [page.getByLabel("邮箱", { exact: true }), page.getByLabel("验证码", { exact: true }), page.getByRole("button", { name: "注册并绑定", exact: true })]) {
      const box = await control.boundingBox();
      expect(box?.x).toBeGreaterThanOrEqual(0);
      expect((box?.x || 0) + (box?.width || 0)).toBeLessThanOrEqual(width);
    }
    await expect(page.getByRole("checkbox", { name: "我已阅读并同意", exact: true })).not.toBeChecked();
    await page.screenshot({ path: testInfo.outputPath(`workbuddy-register-${width}.png`), fullPage: true, animations: "disabled" });
  });
}
