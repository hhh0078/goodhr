/** 本文件通过隔离浏览器和模拟 API 回归后台业务弹框、表格展开及手机布局，禁止请求真实业务服务。 */
const { chromium } = require("@playwright/test");
const fs = require("node:fs");
const path = require("node:path");

/** buildMockResponse 为后台页面生成固定测试数据，不读取或写入真实账户。 */
function buildMockResponse(url, method, candidate) {
  const route = url.pathname;
  if (route === "/api/auth/me")
    return { user: { email: "qa@example.invalid", role: "super_admin" } };
  if (route === "/api/subscription/status")
    return {
      subscription: {
        active: true,
        member_type: "max",
        remaining_days: 30,
        allow_ai: true,
        allow_auto_reply: true,
      },
    };
  if (route === "/api/runtime/config" || route === "/api/system/app-config")
    return { config: {} };
  if (route === "/api/ai-wallet") return { wallet: { balance: "100.00" } };
  if (route === "/api/tenants/invitations/pending") return { invitations: [] };
  if (route === "/api/config/notification-profile")
    return { profile: { completed: true } };
  if (route === "/api/admin/users")
    return {
      users: [
        {
          email: "member@example.invalid",
          role: "user",
          status: "active",
          ai_balance: "20.00",
          subscription: {
            active: true,
            member_type: "max",
            expires_at: "2026-11-01",
          },
        },
      ],
      total: 1,
    };
  if (route === "/api/admin/activation-codes")
    return {
      codes: [
        {
          id: "code1",
          code: "QA-ANTD",
          days: 30,
          status: "unused",
          created_at: "2026-10-09",
        },
      ],
    };
  if (route === "/api/candidates")
    return { candidates: [candidate], total: 1, page: 1 };
  if (route.includes("/notes"))
    return method === "POST"
      ? {
          note: {
            id: "note2",
            content: "弹框测试新备注",
            author_email: "qa@example.invalid",
            created_at: "2026-10-09",
          },
        }
      : { notes: candidate.notes };
  if (route.startsWith("/api/candidates/")) return { candidate };
  if (route === "/api/positions") return { positions: [] };
  if (route === "/api/tenants/members")
    return {
      members: [
        {
          email: "member@example.invalid",
          role: "user",
          status: "active",
          today_greeted_count: 9,
          created_at: "2026-10-09",
        },
      ],
      can_manage: true,
      today_greeted_count: 9,
    };
  if (route === "/api/tenants/invitations") return { invitations: [] };
  if (route === "/api/platform-accounts")
    return {
      accounts: [
        {
          id: "qa-account",
          platform_id: "boss",
          display_name: "测试账号",
          status: "available",
          updated_at: "2026-10-09",
        },
      ],
    };
  if (route === "/api/platforms/config/") return { platforms: [] };
  if (route === "/health") return { ok: true, version: "qa", ready: true };
  if (route === "/api/v1/runtime/status")
    return {
      node_installed: true,
      camoufox_installed: true,
      version: "qa",
      installed_versions: {},
      data_dir: "C:/qa",
    };
  if (route === "/api/v1/app-update/status")
    return { stage: "idle", current_version: "qa" };
  if (route === "/api/admin/logs")
    return {
      logs: [
        {
          id: "log1",
          created_at: "2026-10-09",
          category: "错误",
          account: "member@example.invalid",
          position_name: "测试岗位",
          message: "测试错误",
          event: {
            error_message: "测试详情",
            input: { a: 1 },
            output: { b: 2 },
          },
        },
      ],
      total: 1,
    };
  if (route === "/api/invitations/summary")
    return {
      invite_id: "qa",
      config: {},
      invitees: [
        {
          id: "invite1",
          email: "friend@example.invalid",
          created_at: "2026-10-09",
        },
      ],
    };
  if (route === "/api/admin/system/configs/")
    return {
      configs: [
        {
          config_key: "system.guide",
          description: "帮助中心",
          config_value: "{}",
          enabled: true,
        },
      ],
    };
  if (route === "/api/ai-wallet/records")
    return {
      records: [
        {
          id: "r1",
          category: "admin_adjust",
          reason: "测试调整",
          change: "10.00",
          balance_after: "100.00",
          created_at: "2026-10-09",
        },
      ],
      total: 1,
    };
  if (route === "/api/team/stats")
    return {
      totals: {},
      members: [{ email: "member@example.invalid", greeted_count: 9 }],
    };
  return { ok: true };
}

/** runAdminPageChecks 验证业务表单和弹框，将结果与稳定截图保存到项目 artifacts 目录。 */
async function runAdminPageChecks(baseURL = "http://127.0.0.1:5190") {
  const previewOrigin = new URL(baseURL).origin;
  const output = path.resolve(__dirname, "../../../artifacts/antd-admin-qa");
  fs.mkdirSync(output, { recursive: true });
  const writes = [];
  const errors = [];
  const checks = [];
  const candidate = {
    id: "qa-candidate",
    candidate_name: "测试候选人",
    work_region: "成都",
    expected_position: "产品经理",
    work_years: "5年",
    education_level: "本科",
    work_experiences: [{ company_name: "测试公司", position_name: "产品经理" }],
    notes: [
      {
        id: "note1",
        content: "测试备注",
        author_email: "qa@example.invalid",
        created_at: "2026-10-09T01:00:00Z",
      },
    ],
  };
  let failNextAdjustment = false;
  const browser = await chromium.launch({ channel: "chrome", headless: true });
  let page;

  /** verify 保存通过的检查，失败时输出明确的业务条件。 */
  function verify(condition, message) {
    if (!condition) throw new Error(message);
    checks.push(message);
  }

  try {
    const context = await browser.newContext({
      viewport: { width: 1440, height: 1000 },
    });
    await context.addInitScript(() =>
      localStorage.setItem("goodhr5_access_token", "qa-only-token"),
    );
    await context.route("**/*", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const isAPI =
        url.pathname.startsWith("/api/") || url.pathname === "/health";
      if (url.origin === previewOrigin && !isAPI) return route.continue();
      if (!isAPI) return route.abort();
      const headers = {
        "access-control-allow-origin": "*",
        "access-control-allow-methods": "GET,POST,PUT,DELETE,OPTIONS",
        "access-control-allow-headers": "content-type,authorization",
      };
      const method = request.method();
      if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
      if (method !== "GET") {
        writes.push({
          path: url.pathname,
          method,
          body: request.postDataJSON(),
        });
        await new Promise((resolve) => setTimeout(resolve, 550));
        if (failNextAdjustment && url.pathname === "/api/admin/users") {
          failNextAdjustment = false;
          return route.fulfill({
            status: 500,
            json: { message: "测试保存失败" },
            headers,
          });
        }
      }
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        json: buildMockResponse(url, method, candidate),
        headers,
      });
    });
    page = await context.newPage();
    page.on("pageerror", (error) => errors.push(error.message));

    /** open 等待后台初始化完成后再开始操作，避免将加载动画当成页面状态。 */
    async function open(route) {
      await page.goto(`${previewOrigin}${route}`, {
        waitUntil: "domcontentloaded",
      });
      await page.locator("[data-admin-root]").waitFor({ timeout: 20000 });
      await page.waitForTimeout(500);
    }

    /** close 关闭当前弹框并等待退出动画完成。 */
    async function close(dialog) {
      await dialog.getByRole("button", { name: "取消", exact: true }).click();
      await dialog.waitFor({ state: "hidden" });
    }

    await open("/admin/users");
    await page
      .getByRole("button", { name: "天数", exact: true })
      .first()
      .click();
    let dialog = page.getByRole("dialog");
    await dialog.waitFor();
    verify(
      (await dialog.getByRole("textbox").count()) >= 2 &&
        (await dialog.getByRole("spinbutton").count()) === 1,
      "用户天数弹框显示邮箱、天数和原因",
    );
    await close(dialog);
    verify(
      (await page.getByRole("dialog").count()) === 0,
      "用户天数弹框取消关闭",
    );
    await page
      .getByRole("button", { name: "天数", exact: true })
      .first()
      .click();
    dialog = page.getByRole("dialog");
    await dialog.getByLabel("调整天数", { exact: true }).fill("0");
    await dialog.getByRole("button", { name: "确认调整", exact: true }).click();
    await page.waitForTimeout(150);
    verify(
      !writes.some((write) => write.path === "/api/admin/users"),
      "零天数未提交请求",
    );
    await dialog.getByLabel("调整天数", { exact: true }).fill("7");
    failNextAdjustment = true;
    await dialog.getByRole("button", { name: "确认调整", exact: true }).click();
    verify(
      await dialog
        .getByRole("button", { name: "处理中", exact: false })
        .isDisabled(),
      "天数提交期间阻止重复提交",
    );
    await page.waitForTimeout(750);
    verify(await dialog.isVisible(), "保存失败后弹框保留草稿");
    verify(
      (await dialog.getByLabel("调整天数", { exact: true }).inputValue()) ===
        "7",
      "保存失败保留天数",
    );
    await close(dialog);
    await page
      .getByRole("button", { name: "调余额", exact: true })
      .first()
      .click();
    dialog = page.getByRole("dialog");
    verify(
      (await dialog.getByRole("spinbutton").inputValue()) === "10",
      "AI余额弹框默认值正确",
    );
    await page.keyboard.press("Escape");
    await dialog.waitFor({ state: "hidden" });
    verify(
      (await page.getByRole("dialog").count()) === 0,
      "AI余额弹框 Escape 关闭",
    );

    await open("/admin/activation-codes");
    await page.getByRole("button", { name: "生成激活码", exact: true }).click();
    dialog = page.getByRole("dialog");
    verify(
      (await dialog.getByRole("spinbutton").count()) === 2,
      "激活码弹框显示数值字段",
    );
    await dialog.getByLabel("生成数量", { exact: true }).fill("1.5");
    await dialog.getByRole("button", { name: "生成", exact: true }).click();
    await page.waitForTimeout(150);
    verify(
      !writes.some((write) => write.path === "/api/admin/activation-codes"),
      "激活码非整数数量不提交",
    );
    await close(dialog);

    await open("/admin/resumes");
    await page
      .getByRole("button", { name: "备注", exact: false })
      .first()
      .click();
    dialog = page.getByRole("dialog");
    const noteInput = dialog.getByPlaceholder("比如：意向不错，周五再约一次。", {
      exact: true,
    });
    await noteInput.waitFor();
    verify(
      await dialog
        .getByRole("button", { name: "加备注", exact: true })
        .isDisabled(),
      "备注空内容阻止提交",
    );
    await noteInput.fill("弹框测试新备注");
    await dialog.getByRole("button", { name: "加备注", exact: true }).click();
    await page.waitForTimeout(750);
    verify(
      (await noteInput.inputValue()) === "",
      "备注提交成功后清空输入",
    );
    verify(
      (await dialog.getByText("弹框测试新备注", { exact: true }).count()) === 1,
      "备注提交成功后列表展示新增内容",
    );
    await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    await dialog.waitFor({ state: "hidden" });

    await open("/admin/accounts");
    await page.getByRole("button", { name: "新增账号", exact: true }).click();
    dialog = page.getByRole("dialog");
    verify(
      await dialog
        .getByRole("button", { name: "创建并登录", exact: true })
        .isDisabled(),
      "平台账号名称空白阻止创建",
    );
    await dialog.getByLabel("账号名称", { exact: true }).fill("测试账号");
    verify(
      await dialog
        .getByRole("button", { name: "创建并登录", exact: true })
        .isEnabled(),
      "填写账号名后创建按钮启用",
    );
    await close(dialog);

    await open("/admin/team");
    await page.getByRole("button", { name: "邀请成员", exact: true }).click();
    dialog = page.getByRole("dialog");
    verify(
      await dialog
        .getByRole("button", { name: "发送邀请", exact: true })
        .isDisabled(),
      "团队邀请邮箱为空时不能提交",
    );
    await dialog
      .getByLabel("成员邮箱", { exact: true })
      .fill("coworker@example.invalid");
    await close(dialog);
    await page.getByRole("button", { name: "邀请成员", exact: true }).click();
    dialog = page.getByRole("dialog");
    verify(
      (await dialog.getByLabel("成员邮箱", { exact: true }).inputValue()) ===
        "coworker@example.invalid",
      "取消团队邀请后保留草稿",
    );
    await close(dialog);
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    dialog = page.getByRole("dialog");
    await dialog.getByRole("button", { name: "保存身份", exact: true }).click();
    verify(
      await dialog
        .getByRole("button", { name: "移出团队", exact: true })
        .isDisabled(),
      "团队身份提交期间禁用移出操作",
    );
    await page.keyboard.press("Escape");
    verify(await dialog.isVisible(), "团队身份提交期间 Escape 不关闭弹框");
    await dialog.waitFor({ state: "hidden" });
    verify(
      writes.filter(
        (write) =>
          write.path === "/api/tenants/members/member%40example.invalid" &&
          write.method === "PUT",
      ).length === 1,
      "团队身份只提交一次",
    );

    await open("/admin/logs");
    await page.getByRole("row").filter({ hasText: "测试岗位" }).click();
    verify(
      (await page.getByText("测试详情", { exact: true }).count()) >= 1,
      "日志表格展开后显示完整错误",
    );
    await page.getByText("查看原始日志", { exact: true }).click();
    verify(
      (await page.locator("pre").count()) >= 3,
      "日志原始数据使用折叠面板展示",
    );
    await page.screenshot({
      path: path.join(output, "logs-desktop.png"),
      fullPage: true,
    });

    for (const route of [
      "/admin/invitations",
      "/admin/team-stats",
      "/admin/system-config",
      "/admin/ai-records",
      "/admin/agent-download",
    ])
      await open(route);
    await page.setViewportSize({ width: 390, height: 844 });
    await open("/admin/users");
    await page
      .getByRole("button", { name: "天数", exact: true })
      .first()
      .click();
    dialog = page.getByRole("dialog");
    await dialog.waitFor();
    await page.waitForTimeout(400);
    const bounds = await dialog.boundingBox();
    verify(
      bounds && bounds.x >= 0 && bounds.x + bounds.width <= 392,
      "手机端用户弹框未超出屏幕",
    );
    await page.screenshot({
      path: path.join(output, "users-mobile-modal.png"),
      fullPage: true,
    });
    await close(dialog);
    verify(errors.length === 0, "所测页面无浏览器运行错误");
    fs.writeFileSync(
      path.join(output, "result.json"),
      JSON.stringify({ checks, writes, errors }, null, 2),
    );
    return checks;
  } catch (error) {
    if (page)
      await page
        .screenshot({ path: path.join(output, "failure.png"), fullPage: true })
        .catch(() => undefined);
    fs.writeFileSync(
      path.join(output, "failure.json"),
      JSON.stringify(
        { error: String(error.stack || error), checks, writes, errors },
        null,
        2,
      ),
    );
    throw error;
  } finally {
    await browser.close();
  }
}

module.exports = { runAdminPageChecks };
