/** 文件作用：用合成账号与模拟接口验证协议自动续登、注册/密码登录边界和手机布局，不接触生产数据。 */
import { test, expect, type Page } from "@playwright/test";

/** mockLogin 隔离邮件、云端和第三方请求，记录真实提交的登录请求体。 */
async function mockLogin(page: Page, options: { existing?: boolean; accepted?: boolean; rejectLogin?: boolean; failCode?: boolean } = {}) {
  const calls: Array<{ path: string; body: Record<string, unknown> }> = [];
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith("/api/auth/")) {
      if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers: { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" } });
      calls.push({ path: url.pathname, body: route.request().postDataJSON() || {} });
      const headers = { "access-control-allow-origin": "*" };
      if (url.pathname.endsWith("send-code")) return route.fulfill({ status: options.failCode ? 500 : 200, headers, json: options.failCode ? { error: "failed to send code" } : { ok: true, is_new_account: !options.existing } });
      if (url.pathname.endsWith("agreement-status")) return route.fulfill({ headers, json: { agreement_accepted: Boolean(options.accepted) } });
      if (url.pathname.endsWith("login")) return route.fulfill({ status: options.rejectLogin ? 401 : 200, headers, json: options.rejectLogin ? { error: "验证码错误或已过期" } : { access_token: "synthetic-token", requires_password_setup: false } });
      return route.fulfill({ headers, json: { ok: true } });
    }
    if (url.pathname === "/admin/" || url.pathname === "/admin") return route.fulfill({ contentType: "text/html; charset=utf-8", body: "<html lang='zh-CN'><meta charset='utf-8'><body>模拟控制台已到达</body></html>" });
    if (!["127.0.0.1", "localhost"].includes(url.hostname)) return route.abort();
    return route.continue();
  });
  await page.goto("/login");
  return calls;
}

/** fillRegistration 填写合成注册表单，不发送真实邮件。 */
async function fillRegistration(page: Page) {
  await page.getByLabel("邮箱", { exact: true }).fill("new-user@example.invalid");
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await page.getByLabel("验证码", { exact: true }).fill("1234");
  await page.getByLabel("设置登录密码", { exact: true }).fill("synthetic-password");
  await page.getByLabel("再输入一次密码", { exact: true }).fill("synthetic-password");
}

test("注册只点一次提交，协议不滚动也能确认并自动进入后台", async ({ page }) => {
  const calls = await mockLogin(page);
  await fillRegistration(page);
  await page.getByRole("button", { name: "注册并进入控制台", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "GoodHR 使用协议与隐私说明" });
  await expect(dialog).toBeVisible();
  expect(calls.filter((call) => call.path.endsWith("/login"))).toHaveLength(0);
  await expect(dialog.getByRole("button", { name: "我已阅读", exact: true })).toBeEnabled();
  await dialog.getByRole("button", { name: "我已阅读", exact: true }).click();
  await expect(page.getByText("模拟控制台已到达")).toBeVisible();
  const submits = calls.filter((call) => call.path.endsWith("/login"));
  expect(submits).toHaveLength(1);
  expect(submits[0].body.agreement_accepted).toBe(true);
});

test("取消协议不提交并保留表单，只预览确认也不会登录", async ({ page }) => {
  const calls = await mockLogin(page);
  await fillRegistration(page);
  await page.getByRole("button", { name: "注册并进入控制台", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "GoodHR 使用协议与隐私说明" });
  await dialog.getByRole("button", { name: "先不登录", exact: true }).click();
  await expect(page.getByLabel("验证码", { exact: true })).toHaveValue("1234");
  await page.getByRole("button", { name: "《使用协议与隐私说明》", exact: true }).click();
  await dialog.getByRole("button", { name: "我已阅读", exact: true }).click();
  await expect(page.getByRole("checkbox", { name: "我已阅读并同意", exact: true })).toBeChecked();
  expect(calls.filter((call) => call.path.endsWith("/login"))).toHaveLength(0);
});

test("已确认协议的老账号密码登录直接继续，不重复弹框", async ({ page }) => {
  const calls = await mockLogin(page, { existing: true, accepted: true });
  await page.getByRole("button", { name: "密码登录", exact: true }).first().click();
  await page.getByLabel("邮箱", { exact: true }).fill("existing@example.invalid");
  await page.getByLabel("密码", { exact: true }).fill("synthetic-password");
  await page.getByRole("button", { name: "密码登录", exact: true }).last().click();
  await expect(page.getByText("模拟控制台已到达")).toBeVisible();
  expect(calls.filter((call) => call.path.endsWith("password-login"))).toHaveLength(1);
});

test("协议确认后验证码失败保留输入，可以直接重试", async ({ page }) => {
  const calls = await mockLogin(page, { rejectLogin: true });
  await fillRegistration(page);
  await page.getByRole("button", { name: "注册并进入控制台", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "我已阅读", exact: true }).click();
  await expect(page.getByText("验证码错误或已过期", { exact: true })).toBeVisible();
  await expect(page.getByLabel("验证码", { exact: true })).toHaveValue("1234");
  await page.getByRole("button", { name: "注册并进入控制台", exact: true }).click();
  await expect.poll(() => calls.filter((call) => call.path.endsWith("/login")).length).toBe(2);
  await expect(page.getByRole("dialog")).not.toBeVisible();
});

test("发送验证码失败不会出现注册成功或进入后台", async ({ page }) => {
  const calls = await mockLogin(page, { failCode: true });
  await page.getByLabel("邮箱", { exact: true }).fill("new-user@example.invalid");
  await page.getByRole("button", { name: "发送验证码", exact: true }).click();
  await expect(page.getByText("验证码发送失败，请稍后重试", { exact: true })).toBeVisible();
  expect(calls.filter((call) => call.path.endsWith("/login"))).toHaveLength(0);
});

for (const width of [1440, 375]) {
  test(`协议勾选与链接在${width}宽度下可见且不越界`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockLogin(page);
    const link = page.getByRole("button", { name: "《使用协议与隐私说明》", exact: true });
    await expect(link).toHaveCSS("text-decoration-line", "underline");
    await expect(page.getByRole("checkbox", { name: "我已阅读并同意", exact: true })).not.toBeChecked();
    await page.screenshot({ path: `artifacts/login-conversion-${width}.png`, fullPage: true, animations: "disabled" });
    await link.click();
    const dialog = page.getByRole("dialog");
    const box = await dialog.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
    await expect(dialog.getByRole("button", { name: "我已阅读", exact: true })).toBeEnabled();
    await page.screenshot({ path: `artifacts/login-agreement-${width}.png`, fullPage: true, animations: "disabled" });
  });
}
