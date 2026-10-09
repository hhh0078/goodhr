/** 本文件回归验证会员主题、激活与本地绑定、表单取消、嵌套图片预览和不可关闭的系统弹窗。 */
import { test, expect, type Page } from "@playwright/test";

const {
  runHeavyChecks,
}: {
  runHeavyChecks: (baseURL: string) => Promise<string[]>;
} = require("./flows/heavy.cjs");
const {
  runAdminPageChecks,
}: {
  runAdminPageChecks: (baseURL: string) => Promise<string[]>;
} = require("./flows/admin-pages.cjs");
const {
  runFloatingChecks,
}: {
  runFloatingChecks: (baseURL: string) => Promise<string[]>;
} = require("./flows/floating.cjs");

type MockOptions = {
  signedOut?: boolean;
  diagnostic?: boolean;
  member?: string;
  disconnected?: boolean;
  missingRuntime?: boolean;
  update?: string;
  passwordSetup?: boolean;
  invitation?: boolean;
  delayInvitation?: boolean;
};

const fullAIReply = `测试开头\n${"完整的 AI 实际回复\n".repeat(300)}测试原文末尾`;

/** mockSession 拦截全部云端和本地程序请求，避免测试写入真实账号或操作浏览器。 */
async function mockSession(page: Page, options: MockOptions = {}) {
  const calls: { path: string; method: string }[] = [];
  await page.route("**/*", (route) => {
    const url = new URL(route.request().url());
    return ["127.0.0.1", "localhost", "goodhr.58it.cn"].includes(url.hostname)
      ? route.continue()
      : route.abort();
  });
  if (!options.signedOut) {
    await page.addInitScript(() =>
      localStorage.setItem("goodhr5_access_token", "test-only-token"),
    );
  }
  await page.route(
    /^https?:\/\/(127\.0\.0\.1:(8084|43129)|goodhr\.58it\.cn)\//,
    async (route) => {
      const request = route.request();
      const { pathname: path, port } = new URL(request.url());
      const method = request.method();
      if (method === "OPTIONS")
        return route.fulfill({
          status: 204,
          headers: {
            "access-control-allow-origin": "*",
            "access-control-allow-headers": "*",
            "access-control-allow-methods": "*",
          },
        });
      calls.push({ path, method });
      let data: Record<string, unknown> = { ok: true };
      if (port === "43129") {
        if (options.disconnected) return route.abort("connectionrefused");
        if (path === "/health") data = { ok: true, version: "6.2.10.1" };
        else if (path === "/api/v1/runtime/status")
          data = {
            version: "6.2.10.1",
            node_installed: !options.missingRuntime,
            camoufox_installed: !options.missingRuntime,
            system_runtime: { ready: true, required: false },
          };
        else if (path === "/api/v1/app-update/status")
          data = { current_version: "6.2.10.1", stage: "idle" };
      } else {
        if (
          (path === "/api/admin/logs" ||
            path.startsWith("/api/public/position-log-shares/")) &&
          options.diagnostic
        ) {
          data = {
            logs: [
              {
                id: "test-ai-log",
                category: "错误",
                level: "error",
                position_name: "测试诊断岗位",
                created_at: "2026-10-09T10:00:00+08:00",
                message: "AI 解析失败",
                event: {
                  task_id: "test-only-task",
                  is_error: true,
                  message: "AI 解析失败",
                  error_message: "测试解析错误",
                  input: {},
                  output: {
                    status: "failed",
                    ai_response_diagnostic: {
                      model: "qwen-test",
                      response: fullAIReply,
                      response_kind: "model_content",
                      parse_error: "返回不符合 JSON 格式",
                      parse_offset: 0,
                      response_complete: false,
                    },
                  },
                },
              },
            ],
            total: 1,
            expires_at: "2026-10-10T10:00:00+08:00",
          };
        } else if (path === "/api/auth/me")
          data = {
            user: {
              email: "ui-test@example.invalid",
              role: "super_admin",
              id: "test-user",
            },
            requires_password_setup: Boolean(options.passwordSetup),
          };
        else if (path === "/api/subscription/status")
          data = {
            subscription: {
              active: options.member !== "free",
              member_type: options.member || "max",
              member_name: "测试会员",
              remaining_days: 20,
              allow_ai: true,
              allow_auto_reply: true,
              features: [],
            },
          };
        else if (path === "/api/system/app-config") data = { config: {} };
        else if (path === "/api/runtime/config")
          data = {
            config: options.update
              ? {
                  local_agent: [
                    {
                      version: options.update,
                      url: "https://example.invalid/test-only-installer.exe",
                    },
                  ],
                }
              : {},
          };
        else if (path === "/api/ai-wallet")
          data = { wallet: { balance: "12.00" } };
        else if (path === "/api/tenants/invitations/pending")
          data = {
            invitations: options.invitation
              ? [{ id: "test-invite", tenant_name: "测试团队", role: "member" }]
              : [],
          };
        else if (
          path.includes("/invitations/test-invite/") &&
          options.delayInvitation
        )
          await new Promise((resolve) => setTimeout(resolve, 700));
        else if (path === "/api/platform-accounts")
          data = {
            accounts: [
              {
                id: "test-account",
                platform_id: "boss",
                display_name: "测试账号",
                status: "available",
              },
            ],
          };
        else if (path === "/api/platforms") data = { platforms: [] };
        else if (path === "/api/config/notification-profile")
          data = { profile: { completed: true } };
        else
          data = {
            ok: true,
            items: [],
            records: [],
            users: [],
            resumes: [],
            orders: [],
            plans: [],
            positions: [],
            templates: [],
            tenants: [],
            accounts: [],
            config: {},
          };
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        headers: { "access-control-allow-origin": "*" },
        body: JSON.stringify(data),
      });
    },
  );
  return calls;
}

test("会员激活使用后端已有兑换接口，提交前去除首尾空格", async ({ page }) => {
  const calls = await mockSession(page);
  const bodies: unknown[] = [];
  await page.route("**/api/activation-codes/redeem", async (route) => {
    expect(route.request().method()).toBe("POST");
    bodies.push(route.request().postDataJSON());
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ ok: true }),
    });
  });
  await page.goto("/admin/subscription");
  const input = page.getByLabel("会员激活码", { exact: true });
  await input.fill("  TEST-ONLY-CODE  ");
  await page.getByRole("button", { name: "激活", exact: true }).click();
  await expect.poll(() => bodies.length).toBe(1);
  expect(bodies).toEqual([{ code: "TEST-ONLY-CODE" }]);
  expect(calls.some((call) => call.path === "/api/subscription/redeem")).toBe(
    false,
  );
  await expect(input).toHaveValue("");
  await expect(
    page.getByText("激活成功，会员时间已到账。", { exact: true }),
  ).toBeVisible();
});

test("本地绑定被拒绝时显示原因，手动重试不会产生未处理错误", async ({
  page,
}) => {
  await mockSession(page);
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  let bindCalls = 0;
  await page.route("**/api/v1/session/bind", async (route) => {
    bindCalls += 1;
    await route.fulfill({
      status: 409,
      contentType: "application/json",
      body: JSON.stringify({
        ok: false,
        code: "AGENT_ALREADY_BOUND",
        message: "模拟本地绑定冲突，请检查当前账号",
      }),
    });
  });
  await page.goto("/admin/accounts");
  await expect(
    page.getByText("模拟本地绑定冲突，请检查当前账号", { exact: true }).first(),
  ).toBeVisible();
  const previousCalls = bindCalls;
  await page
    .locator(".goodhr-admin-toolbar")
    .getByRole("button", { name: /端口 43129/ })
    .click();
  await expect.poll(() => bindCalls).toBeGreaterThan(previousCalls);
  await expect(
    page.getByText("模拟本地绑定冲突，请检查当前账号", { exact: true }).first(),
  ).toBeVisible();
  expect(errors).toEqual([]);
});

for (const [member, accent] of [
  ["free", "#159a62"],
  ["plus", "#242424"],
  ["max", "#8a6518"],
]) {
  test(`${member} 会员主题在正文和弹窗中一致`, async ({ page }) => {
    await mockSession(page, { member });
    await page.goto("/admin/accounts");
    await expect(
      page.getByRole("heading", { name: "平台账号", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "当前会员主题", exact: true })
      .click();
    const dialog = page.getByRole("dialog", { name: "当前会员主题" });
    await expect(dialog).toBeVisible();
    const color = await dialog.evaluate((element) =>
      getComputedStyle(element).getPropertyValue("--goodhr-primary").trim(),
    );
    expect(color).toBe(accent);
    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();
  });
}

test("新增账号取消后保留草稿，空名称不能提交", async ({ page }) => {
  const calls = await mockSession(page);
  await page.goto("/admin/accounts");
  await page.getByRole("button", { name: "新增账号", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "新增平台账号" });
  await expect(
    dialog.getByRole("button", { name: "创建并登录" }),
  ).toBeDisabled();
  await dialog.getByPlaceholder("例如：成都招聘账号").fill("保留这个草稿");
  await dialog.getByRole("radio", { name: /智联招聘/ }).check();
  await page.screenshot({
    path: "artifacts/antd-account-modal-desktop.png",
    animations: "disabled",
  });
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await page.getByRole("button", { name: "新增账号", exact: true }).click();
  await expect(dialog.getByPlaceholder("例如：成都招聘账号")).toHaveValue(
    "保留这个草稿",
  );
  await expect(dialog.getByRole("radio", { name: /智联招聘/ })).toBeChecked();
  await page.keyboard.press("Escape");
  expect(
    calls.filter(
      (call) =>
        call.path === "/api/platform-accounts" && call.method === "POST",
    ),
  ).toHaveLength(0);
});

test("删除确认的取消与确认分别返回正确结果", async ({ page }) => {
  const calls = await mockSession(page);
  await page.goto("/admin/accounts");
  await page.getByRole("button", { name: "删除", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  expect(calls.filter((call) => call.method === "DELETE")).toHaveLength(0);
  await page.getByRole("button", { name: "删除", exact: true }).click();
  await dialog.getByRole("button", { name: "确认", exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect
    .poll(() => calls.filter((call) => call.method === "DELETE").length)
    .toBe(1);
});

test("嵌套图片预览关闭后仍保留父弹窗", async ({ page }) => {
  await mockSession(page, { disconnected: true });
  await page.goto("/admin/accounts");
  const parent = page.getByRole("dialog", { name: "请先安装本地程序" });
  await expect(parent).toBeVisible();
  await parent
    .getByRole("img", { name: "Chrome 设备上的应用权限开关位置" })
    .click();
  await expect(page.locator(".ant-image-preview")).toBeVisible();
  await page
    .getByRole("dialog", {
      name: "Chrome 设备上的应用权限开关位置",
      exact: true,
    })
    .getByRole("button", { name: "close", exact: true })
    .click();
  await expect(page.locator(".ant-image-preview")).not.toBeVisible();
  await expect(parent).toBeVisible();
  await parent
    .getByRole("img", { name: "Chrome 设备上的应用权限开关位置" })
    .click();
  await expect(page.locator(".ant-image-preview")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".ant-image-preview")).not.toBeVisible();
  await expect(parent).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(parent).not.toBeVisible();
});

test("必要组件缺失时不能用遮罩或 Escape 关闭弹窗", async ({ page }) => {
  await mockSession(page, { missingRuntime: true });
  await page.goto("/admin/accounts");
  const dialog = page.getByRole("dialog", { name: "准备必要组件" });
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  await page.mouse.click(3, 3);
  await expect(dialog).toBeVisible();
  await expect(dialog.locator(".ant-modal-close")).toHaveCount(0);
});

test("补设密码不能关闭且不匹配的输入不能保存", async ({ page }) => {
  const calls = await mockSession(page, { passwordSetup: true });
  await page.goto("/admin/accounts");
  const dialog = page.getByRole("dialog", { name: "给老账号补一个密码" });
  await expect(dialog).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  await dialog.getByLabel("新密码", { exact: true }).fill("test-only-password");
  await dialog
    .getByLabel("再输入一次", { exact: true })
    .fill("different-password");
  await dialog.getByRole("button", { name: "设置密码并继续" }).click();
  await expect(dialog.getByText("两次密码不一样，我先不敢保存")).toBeVisible();
  expect(
    calls.filter((call) => call.path === "/api/auth/password/setup"),
  ).toHaveLength(0);
});

test("团队邀请提交期间按钮和关闭操作都被锁住", async ({ page }) => {
  const calls = await mockSession(page, {
    invitation: true,
    delayInvitation: true,
  });
  await page.goto("/admin/accounts");
  const dialog = page.getByRole("dialog", { name: "有人想拉你进团队" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "拒绝邀请" }).click();
  await expect(dialog.getByRole("button", { name: "拒绝邀请" })).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  await expect(dialog).not.toBeVisible();
  expect(
    calls.filter(
      (call) =>
        call.path.includes("/invitations/test-invite/") &&
        call.method === "POST",
    ),
  ).toHaveLength(1);
});

test("手机上的长表单和菜单都能打开关闭", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockSession(page);
  await page.goto("/admin/accounts");
  await page.getByRole("button", { name: "打开菜单", exact: true }).click();
  const menu = page.getByRole("dialog", { name: "GoodHR 菜单" });
  await expect(menu).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(menu).not.toBeVisible();
  await page.getByRole("button", { name: "新增账号", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "新增平台账号" });
  await expect(dialog).toHaveCSS("animation-name", "none");
  await expect(dialog).toHaveCSS("transform", "none");
  await expect(dialog).toHaveCSS("opacity", "1");
  await expect(dialog.locator(".goodhr-choice-grid")).toHaveCSS(
    "display",
    "grid",
  );
  const choiceBox = await dialog
    .locator(".goodhr-choice")
    .first()
    .boundingBox();
  expect(choiceBox!.width).toBeGreaterThanOrEqual(270);
  const box = await dialog.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.width).toBeGreaterThanOrEqual(300);
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(391);
  await page.screenshot({ path: "artifacts/antd-account-modal-mobile.png" });
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).not.toBeVisible();
});

test("必要升级无法用遮罩或 Escape 关闭", async ({ page }) => {
  await mockSession(page, { update: "6.2.11.0" });
  await page.goto("/admin/accounts");
  const required = page.getByRole("dialog", { name: "更新本地程序" });
  await expect(required).toBeVisible();
  await expect(required.getByRole("button", { name: "暂时忽略" })).toHaveCount(
    0,
  );
  await page.keyboard.press("Escape");
  await expect(required).toBeVisible();
  await page.mouse.click(3, 3);
  await expect(required).toBeVisible();
});

test("岗位、会员、支付和个人配置的 59 项交互", async ({ baseURL }) => {
  test.setTimeout(120_000);
  expect((await runHeavyChecks(baseURL!)).length).toBeGreaterThanOrEqual(59);
});

test("后台业务弹框和表格的 24 项交互", async ({ baseURL }) => {
  test.setTimeout(120_000);
  expect((await runAdminPageChecks(baseURL!)).length).toBeGreaterThanOrEqual(
    24,
  );
});

test("置顶小窗在独立窗口内显示样式，并正确更新和关闭", async ({ baseURL }) => {
  test.setTimeout(120_000);
  expect((await runFloatingChecks(baseURL!)).length).toBeGreaterThanOrEqual(15);
});

test("AI 诊断折叠后能完整显示长原文和零字节位置", async ({ page }) => {
  await mockSession(page, { diagnostic: true });
  await page.goto("/admin/logs");
  await page.getByRole("row").filter({ hasText: "测试诊断岗位" }).click();
  await page
    .getByText("查看 AI 实际返回（qwen-test）", { exact: true })
    .click();
  await expect(
    page.getByText("返回不符合 JSON 格式", { exact: false }),
  ).toBeVisible();
  await expect(page.getByText(/第 0 字节/)).toBeVisible();
  const original = page.locator("pre").filter({ hasText: "测试原文末尾" });
  await expect(original).toHaveText(fullAIReply);
  await expect(
    page.getByText(/连接中断，以下是已收到的全部内容/),
  ).toBeVisible();
});

test("分享日志的参数、任务编号和 AI 原文都能独立展开", async ({ page }) => {
  await mockSession(page, { signedOut: true, diagnostic: true });
  await page.goto(`/diagnostics/${"a".repeat(64)}`);
  await expect(
    page.getByRole("heading", { name: "岗位排障日志", exact: true }),
  ).toBeVisible();
  await page.getByText("查看入参、出参和任务编号", { exact: true }).click();
  await expect(
    page.getByText("任务编号：test-only-task", { exact: true }),
  ).toBeVisible();
  await expect(page.locator(".public-diagnostic-panel.ant-card")).toHaveCount(
    2,
  );
  const normalOutput = page
    .locator("pre")
    .filter({ hasText: '"status": "failed"' });
  await expect(normalOutput).toBeVisible();
  await expect(normalOutput).not.toContainText("ai_response_diagnostic");
  await page
    .getByText("查看 AI 实际返回（qwen-test）", { exact: true })
    .click();
  await expect(
    page.locator("pre").filter({ hasText: "测试原文末尾" }),
  ).toHaveText(fullAIReply);
});

test("手机协议弹窗可滚动，读到底部前不能勾选", async ({ page }) => {
  await mockSession(page, { signedOut: true });
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto("/login");
  await page.getByRole("button", { name: "阅读协议", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "GoodHR 使用协议与隐私说明",
  });
  await expect(dialog.getByRole("checkbox")).toBeDisabled();
  const box = await dialog.boundingBox();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(376);
  await dialog.locator(".public-agreement-content").press("Control+End");
  await expect(dialog.getByRole("checkbox")).toBeEnabled();
  await expect(
    dialog.getByRole("button", { name: "同意并继续", exact: true }),
  ).toBeDisabled();
  await page.screenshot({
    path: "artifacts/antd-login-agreement-mobile.png",
    animations: "disabled",
  });
  await dialog.getByRole("button", { name: "先不登录", exact: true }).click();
  await expect(dialog).not.toBeVisible();
});

test("重置密码校验错误时不提交，取消重开会清空输入", async ({ page }) => {
  const calls = await mockSession(page, { signedOut: true });
  await page.goto("/login");
  await page.getByLabel("邮箱", { exact: true }).fill("test@example.invalid");
  await page.getByRole("button", { name: "忘记密码", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "忘记密码", exact: true });
  await dialog.getByRole("button", { name: "重设密码", exact: true }).click();
  await expect(
    dialog.getByText("请输入 4 位验证码", { exact: true }),
  ).toBeVisible();
  await dialog.getByLabel("验证码", { exact: true }).fill("1234");
  await dialog.getByLabel("新密码", { exact: true }).fill("test-only-password");
  await dialog
    .getByLabel("再输入一次新密码", { exact: true })
    .fill("different-password");
  await dialog.getByRole("button", { name: "重设密码", exact: true }).click();
  await expect(
    dialog.getByText("两次新密码不一样，再核对一下", { exact: true }),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "先不换了", exact: true }).click();
  await page.getByRole("button", { name: "忘记密码", exact: true }).click();
  await expect(dialog.getByLabel("验证码", { exact: true })).toHaveValue("");
  await expect(dialog.getByLabel("新密码", { exact: true })).toHaveValue("");
  expect(calls.filter((call) => call.method === "POST")).toHaveLength(0);
});

test("小补丁允许忽略，并记住当前版本的选择", async ({ page }) => {
  await mockSession(page, { update: "6.2.10.2" });
  await page.goto("/admin/accounts");
  const optional = page.getByRole("dialog", { name: "更新本地程序" });
  await expect(optional).toBeVisible();
  await optional.getByRole("button", { name: "暂时忽略" }).click();
  await expect(optional).not.toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "平台账号", exact: true }),
  ).toBeVisible();
  await expect(optional).not.toBeVisible();
});

test("官网原始 HTML 保留正文、元信息及抓取入口", async ({ request }) => {
  for (const path of [
    "/",
    "/features",
    "/pricing",
    "/contact",
    "/download",
    "/videos",
  ]) {
    const response = await request.get(path);
    expect(response.status()).toBe(200);
    const html = await response.text();
    expect(html).toMatch(/<h1[ >]/);
    expect(html).toMatch(/<title>/);
    expect(html).toMatch(/name="description"/);
    expect(html).toMatch(/rel="canonical"/);
    expect(html).toContain("application/ld+json");
    expect(html).toContain("antd-cssinjs");
    expect(html).not.toContain("MuiButton-root");
  }
  for (const path of ["/sitemap.xml", "/robots.txt", "/llms.txt"])
    expect((await request.get(path)).status()).toBe(200);
  const oldURL = await request.get("/features.html", { maxRedirects: 0 });
  expect(oldURL.status()).toBe(308);
  expect(oldURL.headers().location).toBe("/features");
});
