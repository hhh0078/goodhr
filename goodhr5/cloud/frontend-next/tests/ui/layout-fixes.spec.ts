/** 本文件使用完全拦截的接口数据验证胶囊按钮、余额卡、长简历、用户列宽、弹框靠右和下拉筛选。 */
import { test, expect, type Locator, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";

const previewOrigin = new URL(
  process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190",
).origin;
const longEmail = `招聘顾问.${"very-long-department-".repeat(8)}@example.invalid`;
const longInviter = `inviter.${"another-long-team-".repeat(8)}@example.invalid`;
const flowMessage = `${"步骤😀等待连接，".repeat(32)}${"ERROR_WITHOUT_SPACES".repeat(8)}流程全文结束`;
const fullFlowLabel = `卡在：启动本地程序 · ${flowMessage}`;
const longModel = `${"qwen-long-model-name-".repeat(18)}完整模型末尾`;
const longAIReason = `${"具备相关招聘产品经验，判断原因需要保留完整文字。".repeat(16)}AI理由结束`;
const longNote = `${"候选人沟通记录与后续跟进计划，备注全文需要能够查看。".repeat(14)}备注全文结束`;
const longRawText = `${"这是完整原始简历文本，包含工作内容、项目结果与团队协作经历。\n".repeat(40)}原始简历结束`;
const longCompany = `长公司名称${"招聘科技有限公司".repeat(15)}`;
const candidateName = "布局测试候选人😀";
const candidate = {
  id: "layout-candidate",
  engagement_id: "layout-engagement",
  engagement_status: "greeted",
  candidate_name: candidateName,
  birth_ym: "1995-01",
  work_region: "成都 / 上海 / 深圳",
  work_years: "8年",
  education_level: "本科",
  expected_position: `资深招聘产品经理${"兼用户体验及业务流程优化".repeat(18)}`,
  expected_salary_min: 20,
  expected_salary_max: 35,
  work_status: "在职，考虑机会",
  online_status: "近期活跃",
  personal_description: `${"个人优势：具备招聘产品和团队协作经验。".repeat(30)}个人优势全文结束`,
  work_experiences: [
    {
      company_name: longCompany,
      position_name: "资深产品经理",
      start_ym: "2018-03",
      end_ym: "2026-09",
      content: `${"负责招聘产品设计与团队协作。".repeat(30)}工作经历全文结束`,
    },
  ],
  project_experiences: [
    {
      project_name: "长项目说明",
      role_name: "项目负责人",
      content: `${"项目改进取得具体结果。".repeat(30)}项目经历全文结束`,
    },
  ],
  educations: [
    {
      school_name: "测试大学",
      major_name: "软件工程",
      education_level: "本科",
      start_ym: "2014-09",
      end_ym: "2018-06",
    },
  ],
  raw_text: longRawText,
  ai: {
    detail: { score: 0, reason: longAIReason },
    greet: { score: 88, reason: longAIReason },
  },
  notes: [
    {
      id: "layout-note",
      content: longNote,
      author_email: longInviter,
      created_at: "2026-10-09T10:00:00+08:00",
    },
  ],
  user_email: longEmail,
  created_at: "2026-10-09T10:00:00+08:00",
};

type RecordedCall = {
  path: string;
  query: string;
  method: string;
  body?: Record<string, unknown>;
};
type LayoutMock = {
  model: string;
  calls: RecordedCall[];
  errors: string[];
  phase: string;
  notes: typeof candidate.notes;
  userStats: Record<string, unknown>;
};
const mocks = new WeakMap<Page, LayoutMock>();

test.use({
  actionTimeout: 12_000,
  storageState: {
    cookies: [],
    origins: [
      {
        origin: previewOrigin,
        localStorage: [
          { name: "goodhr5_access_token", value: "layout-test-only-token" },
        ],
      },
    ],
  },
});
test.setTimeout(90_000);

/** mockLayoutSession 用内存数据替代全部业务接口，其他外部请求终止，真实账号和招聘平台不会被写入。 */
async function mockLayoutSession(page: Page) {
  const state: LayoutMock = {
    model: "qwen-test",
    calls: [],
    errors: [],
    phase: "页面加载",
    notes: [...candidate.notes],
    userStats: { today_registered_count: 1, agent_binding_count: 1 },
  };
  page.on("pageerror", (error) =>
    state.errors.push(`[${state.phase}] ${error.stack || error.message}`),
  );
  await page.context().route("**/*", async (route) => {
    const url = new URL(route.request().url());
    const method = route.request().method();
    if (url.origin === previewOrigin && !url.pathname.startsWith("/api/"))
      return route.continue();
    if (
      url.origin !== previewOrigin &&
      !["127.0.0.1:8084", "127.0.0.1:43129", "goodhr.58it.cn"].includes(
        url.host,
      )
    )
      return route.abort();
    const headers = {
      "access-control-allow-origin": "*",
      "access-control-allow-headers": "*",
      "access-control-allow-methods": "*",
    };
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    let body: Record<string, unknown> | undefined;
    try {
      body = route.request().postDataJSON();
    } catch {
      /* GET 请求没有请求体。 */
    }
    state.calls.push({ path: url.pathname, query: url.search, method, body });
    let data: Record<string, unknown> = { ok: true };
    if (url.port === "43129") {
      if (url.pathname === "/health") data = { ok: true, version: "6.2.10.1" };
      else if (url.pathname === "/api/v1/runtime/status")
        data = {
          version: "6.2.10.1",
          node_installed: true,
          camoufox_installed: true,
          system_runtime: { ready: true, required: false },
        };
      else if (url.pathname === "/api/v1/app-update/status")
        data = { stage: "idle", current_version: "6.2.10.1" };
    } else {
      if (url.pathname === "/api/auth/me")
        data = {
          user: {
            id: "layout-user",
            email: "layout@example.invalid",
            role: "super_admin",
            tenant_id: "layout-team",
          },
        };
      else if (url.pathname === "/api/subscription/status")
        data = {
          subscription: {
            active: true,
            member_type: "max",
            member_name: "Max 全能版",
            remaining_days: 30,
            allow_ai: true,
            allow_auto_reply: true,
            expires_at: "2026-11-09T10:00:00+08:00",
          },
        };
      else if (
        url.pathname === "/api/runtime/config" ||
        url.pathname === "/api/system/app-config"
      )
        data = { config: {} };
      else if (url.pathname === "/api/config/notification-profile")
        data = { profile: { completed: true } };
      else if (
        url.pathname === "/api/tenants/invitations/pending" ||
        url.pathname === "/api/tenants/invitations"
      )
        data = { invitations: [] };
      else if (url.pathname === "/api/user-flow/state")
        data = { state: "completed", steps: {} };
      else if (url.pathname === "/api/user-flow")
        data = {
          flow: {
            version: 2,
            state: "completed",
            stage: "completed",
            stage_name: "流程完成",
            steps: {},
          },
        };
      else if (url.pathname === "/api/ai-wallet") {
        const wallet = {
          balance: "88.0000",
          bonus_balance: "0.7000",
          bonus_balance_units: 7000,
          default_model: state.model,
          models: [
            { id: state.model, name: "本地模拟模型" },
            { id: "vision-test", name: "视觉模型" },
          ],
        };
        data = { ...wallet, wallet };
      } else if (url.pathname === "/api/config/user-ai")
        data = {
          config: {
            model: state.model,
            enabled: true,
            base_url: "https://example.invalid/mock-only",
            api_key_set: true,
          },
        };
      else if (url.pathname === "/api/subscription/plans") data = { plans: [] };
      else if (url.pathname === "/api/payment/orders") data = { orders: [] };
      else if (url.pathname === "/api/ai-wallet/records")
        data = { records: [], total: 0 };
      else if (url.pathname === "/api/payment/refund")
        data = {
          membership: {
            available: true,
            max_amount_cents: 500,
            max_amount: "5.00",
          },
          ai_balance: {
            available: true,
            max_amount_cents: 8800,
            max_amount: "88.00",
          },
        };
      else if (url.pathname === "/api/admin/logs")
        data = { logs: [], total: 0 };
      else if (url.pathname === "/api/admin/emails") data = { batches: [] };
      else if (url.pathname === "/api/team/stats")
        data = {
          totals: {},
          members: [],
          start_date: "2026-10-01",
          end_date: "2026-10-09",
        };
      else if (url.pathname === "/api/candidates")
        data = {
          candidates: [{ ...candidate, notes: state.notes }],
          total: 1,
          page: 1,
        };
      else if (url.pathname.endsWith("/notes")) {
        if (method === "POST") {
          const note = {
            id: "layout-added-note",
            content: String(body?.content || ""),
            author_email: "layout@example.invalid",
            created_at: "2026-10-09T11:00:00+08:00",
          };
          state.notes = [note, ...state.notes];
          data = { note };
        } else data = { notes: state.notes };
      } else if (url.pathname.startsWith("/api/candidates/"))
        data = { candidate: { ...candidate, notes: state.notes } };
      else if (url.pathname === "/api/positions")
        data = {
          positions: [
            {
              id: "layout-position",
              name: "成都招聘产品经理",
              platform_id: "boss",
              status: "stopped",
            },
            {
              id: "another-position",
              name: "上海测试工程师",
              platform_id: "boss",
              status: "stopped",
            },
          ],
        };
      else if (url.pathname === "/api/admin/users")
        data = {
          users: [
            {
              email: longEmail,
              inviter_email: longInviter,
              role: "user",
              status: "active",
              ai_balance: "12345.67",
              ai_balance_units: 123456700,
              created_at: "2026-10-09T10:00:00+08:00",
              last_login_at: "2026-10-09T12:34:56+08:00",
              subscription: {
                active: true,
                member_type: "max",
                expires_at: "2026-11-09T10:00:00+08:00",
              },
              agent: {
                machine_id: "layout-machine-with-long-id",
                agent_version: "6.2.10.1",
              },
              flow: {
                state: "blocked",
                stage_name: "启动本地程序",
                message: flowMessage,
              },
            },
          ],
          total: 1,
          page: 1,
          stats: state.userStats,
        };
      else if (url.pathname === "/api/platform-accounts")
        data = {
          accounts: [
            {
              id: "layout-account",
              display_name: "布局测试账号",
              platform_id: "boss",
              status: "available",
            },
          ],
        };
      else if (url.pathname === "/api/platforms") data = { platforms: [] };
      else if (url.pathname === "/api/tenants/members")
        data = {
          members: [
            {
              email: "member@example.invalid",
              role: "member",
              status: "active",
              created_at: "2026-10-09T10:00:00+08:00",
            },
            {
              email: "pending@example.invalid",
              role: "user",
              status: "pending",
              invitation_id: "layout-pending-invitation",
              created_at: "2026-10-09T10:00:00+08:00",
            },
          ],
          can_manage: true,
        };
    }
    return route.fulfill({
      status: 200,
      headers,
      contentType: "application/json",
      body: JSON.stringify(data),
    });
  });
  mocks.set(page, state);
  return state;
}

/** visibleBox 返回可见元素的真实边界，避免把未渲染的节点当成布局通过。 */
async function visibleBox(locator: Locator) {
  await expect(locator).toBeVisible();
  const box = await locator.boundingBox();
  expect(box).not.toBeNull();
  return box!;
}

/** expectPillButtons 通过原生计算样式断言每个常规按钮四个圆角不小于按钮高度的一半。 */
async function expectPillButtons(buttons: Locator) {
  const count = await buttons.count();
  expect(count).toBeGreaterThan(0);
  for (let index = 0; index < count; index += 1) {
    const button = buttons.nth(index);
    const box = await visibleBox(button);
    const minimum = Math.ceil(box.height / 2 - 0.1);
    const tooSmall = Array.from({ length: minimum }, (_, value) => value).join(
      "|",
    );
    const capsuleRadius = new RegExp(
      `^(?!(?:${tooSmall})(?:\\.\\d+)?px$)\\d+(?:\\.\\d+)?px$`,
    );
    for (const property of [
      "border-top-left-radius",
      "border-top-right-radius",
      "border-bottom-left-radius",
      "border-bottom-right-radius",
    ])
      await expect(button).toHaveCSS(property, capsuleRadius);
  }
}

/** expectWithinViewport 检查实际元素边界在手机或桌面可见宽度内，不读取或注入页面脚本。 */
async function expectWithinViewport(page: Page, locator: Locator) {
  const box = await visibleBox(locator);
  const width = page.viewportSize()!.width;
  expect(box.x).toBeGreaterThanOrEqual(-1);
  expect(box.x + box.width).toBeLessThanOrEqual(width + 1);
}

/** expectFooterRight 检查最终操作按钮与弹框底部右边界对齐，覆盖默认、编辑和额外操作场景。 */
async function expectFooterRight(dialog: Locator) {
  await expect(dialog).toHaveCSS("animation-name", "none");
  await expect(dialog).toHaveCSS("transform", "none");
  await expect(dialog).toHaveCSS("opacity", "1");
  const footer = dialog.locator(".ant-modal-footer");
  const footerBox = await visibleBox(footer);
  const buttons = footer.locator(".ant-btn:visible");
  const lastButtonBox = await visibleBox(buttons.last());
  expect(
    Math.abs(
      footerBox.x + footerBox.width - lastButtonBox.x - lastButtonBox.width,
    ),
  ).toBeLessThanOrEqual(2);
  for (let index = 1; index < (await buttons.count()); index += 1) {
    const previous = await visibleBox(buttons.nth(index - 1)),
      current = await visibleBox(buttons.nth(index));
    if (Math.abs(previous.y - current.y) <= 1)
      expect(current.x - previous.x - previous.width).toBeLessThanOrEqual(12);
    else
      expect(
        Math.abs(footerBox.x + footerBox.width - previous.x - previous.width),
      ).toBeLessThanOrEqual(2);
  }
  await expectPillButtons(buttons);
}

/** businessMutations 排除页面自动连接和流程心跳，仅统计用户操作可能触发的业务提交。 */
function businessMutations(page: Page) {
  return mocks
    .get(page)!
    .calls.filter(
      (call) =>
        call.method !== "GET" &&
        !["/api/v1/session/bind", "/api/user-flow"].includes(call.path),
    );
}

/** saveLayoutScreenshot 保存忽略目录中的稳定截图，供检查真实视觉密度和长文本边界。 */
async function saveLayoutScreenshot(page: Page, name: string) {
  mkdirSync("artifacts/antd-layout-qa", { recursive: true });
  await page.screenshot({
    path: `artifacts/antd-layout-qa/${name}.png`,
    fullPage: true,
    animations: "disabled",
  });
}

/** 测试前统一安装接口拦截，每个浏览器上下文使用独立内存数据。 */
test.beforeEach(async ({ page }) => {
  await mockLayoutSession(page);
});
/** 测试后检查页面运行错误，避免只通过样式断言却遗漏组件崩溃。 */
test.afterEach(async ({ page }) => {
  expect(mocks.get(page)?.errors || []).toEqual([]);
});

test.describe("公开页面胶囊按钮", () => {
  test.use({ storageState: { cookies: [], origins: [] } });
  test("官网、登录和登录弹框的常规按钮圆角达到高度一半", async ({ page }) => {
    await page.setViewportSize({ width: 1390, height: 1000 });
    await page.goto("/");
    await expectPillButtons(page.locator(".public-home-15 .ant-btn:visible"));
    await page.goto("/login");
    await expect(
      page.getByRole("heading", { name: "欢迎回来", exact: true }),
    ).toBeVisible();
    await expectPillButtons(page.locator(".public-login-1 .ant-btn:visible"));
    await page
      .getByRole("button", { name: "密码登录", exact: true })
      .first()
      .click();
    await page.getByRole("button", { name: "忘记密码", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "忘记密码", exact: true });
    await expectFooterRight(dialog);
    await saveLayoutScreenshot(page, "login-modal-pills");
    await dialog.getByRole("button", { name: "先不换了", exact: true }).click();
    expect(mocks.get(page)!.calls.some((call) => call.method !== "GET")).toBe(
      false,
    );
  });
});

test("后台和新增弹框的按钮保持胶囊形，取消不会提交", async ({ page }) => {
  await page.setViewportSize({ width: 1390, height: 1000 });
  await page.goto("/admin/accounts");
  await expect(
    page.getByRole("button", { name: "新增账号", exact: true }),
  ).toBeVisible();
  await expectPillButtons(
    page.getByRole("main").locator(".ant-btn:not(.ant-btn-circle):visible"),
  );
  await page.getByRole("button", { name: "新增账号", exact: true }).click();
  const dialog = page.getByRole("dialog", {
    name: "新增平台账号",
    exact: true,
  });
  await expectFooterRight(dialog);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  expect(
    mocks
      .get(page)!
      .calls.filter(
        (call) =>
          call.path === "/api/platform-accounts" && call.method === "POST",
      ),
  ).toHaveLength(0);
});

for (const [width, model, label] of [
  [1390, "qwen-test", "桌面短模型"],
  [1390, longModel, "桌面长模型"],
  [390, longModel, "手机长模型"],
] as const) {
  test(`${label}的订阅摘要卡自适应布局且不溢出`, async ({ page }) => {
    mocks.get(page)!.model = model;
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/admin/subscription");
    const card = page
      .getByRole("main")
      .locator(".goodhr-section-panel")
      .filter({ has: page.getByText("AI 余额", { exact: true }) });
    const membershipCard = page
      .getByRole("main")
      .locator(".goodhr-section-panel")
      .filter({ has: page.getByText("当前会员", { exact: true }) });
    const modelButton = card.getByRole("button", {
      name: `当前模型：${model}`,
      exact: true,
    });
    await expect(modelButton).toBeVisible();
    await expectWithinViewport(page, card);
    await expectWithinViewport(page, modelButton);
    const cardBox = await visibleBox(card),
      membershipBox = await visibleBox(membershipCard),
      modelBox = await visibleBox(modelButton);
    expect(modelBox.x + modelBox.width).toBeLessThanOrEqual(
      cardBox.x + cardBox.width + 1,
    );
    if (width >= 900) {
      expect(Math.abs(cardBox.x - membershipBox.x - membershipBox.width - 16)).toBeLessThanOrEqual(2);
      expect(Math.abs(cardBox.y - membershipBox.y)).toBeLessThanOrEqual(2);
      expect(Math.abs(cardBox.width - membershipBox.width)).toBeLessThanOrEqual(2);
      expect(Math.abs(cardBox.height - membershipBox.height)).toBeLessThanOrEqual(2);
    } else {
      expect(cardBox.y).toBeGreaterThanOrEqual(
        membershipBox.y + membershipBox.height + 15,
      );
      expect(Math.abs(cardBox.width - membershipBox.width)).toBeLessThanOrEqual(2);
    }
    if (model === longModel)
      expect(await modelButton.getAttribute("title")).toContain(longModel);
    await modelButton.click();
    const dialog = page.getByRole("dialog", {
      name: "选择当前模型",
      exact: true,
    });
    await expectWithinViewport(page, dialog);
    await expectFooterRight(dialog);
    await dialog.getByRole("button", { name: "先不改", exact: true }).click();
    await saveLayoutScreenshot(
      page,
      `wallet-${width}-${model === longModel ? "long" : "short"}`,
    );
  });
}

for (const width of [1390, 390]) {
  test(`${width}px 简历查询高度、长文本列表、备注与完整详情可用`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/admin/resumes");
    const search = page.getByLabel("搜索", { exact: true });
    const filters = page.getByTestId("resume-filters");
    await expect(search).toBeVisible();
    await expectWithinViewport(page, filters);
    const inputBox = await visibleBox(
      search.locator(
        "xpath=ancestor::span[contains(@class,'ant-input-affix-wrapper')][1]",
      ),
    );
    const queryButton = page.getByRole("button", { name: "查询", exact: true });
    const queryBox = await visibleBox(queryButton);
    expect(inputBox.height).toBeGreaterThanOrEqual(30);
    expect(inputBox.height).toBeLessThanOrEqual(44);
    expect(Math.abs(queryBox.height - inputBox.height)).toBeLessThanOrEqual(1);
    if (width >= 900)
      expect(Math.abs(queryBox.y - inputBox.y)).toBeLessThanOrEqual(1);
    await search.fill("招聘产品");
    await queryButton.click();
    await expect
      .poll(() =>
        mocks
          .get(page)!
          .calls.some(
            (call) =>
              call.path === "/api/candidates" &&
              new URLSearchParams(call.query).get("q") === "招聘产品",
          ),
      )
      .toBe(true);
    const row = page
      .locator(".ant-table-tbody > tr[data-row-key]")
      .filter({ hasText: candidateName });
    await expect(row).toBeVisible();
    await expect(page.locator(".ant-table-content table")).toHaveCSS(
      "table-layout",
      "fixed",
    );
    expect((await visibleBox(row)).height).toBeLessThan(260);
    const firstAI = row
      .locator("td")
      .nth(2)
      .locator("div.ant-typography:visible")
      .first();
    await expect(firstAI).toContainText("第一次0分");
    await firstAI.hover();
    await expect(
      page
        .getByRole("tooltip")
        .filter({ hasText: `第一次 0分：${longAIReason}` }),
    ).toHaveText(`第一次 0分：${longAIReason}`);
    const notePreview = row
      .getByRole("button", { name: "查看备注", exact: true })
      .locator("div.ant-typography:visible")
      .first();
    await notePreview.hover();
    await expect(
      page.getByRole("tooltip").filter({ hasText: longNote }),
    ).toHaveText(longNote);
    await row.getByRole("button", { name: "查看备注", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "备注记录", exact: true });
    await expect(dialog.getByText(longNote, { exact: true })).toBeVisible();
    await expectWithinViewport(page, dialog);
    await expectFooterRight(dialog);
    await dialog
      .getByLabel("新增备注", { exact: true })
      .fill("本地模拟新增备注");
    await dialog.getByRole("button", { name: "新增备注", exact: true }).click();
    await expect(
      dialog.getByText("本地模拟新增备注", { exact: true }),
    ).toBeVisible();
    await expect(dialog.getByLabel("新增备注", { exact: true })).toHaveValue(
      "",
    );
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await saveLayoutScreenshot(page, `resume-list-${width}`);
    await row.getByRole("link", { name: new RegExp(candidateName) }).click();
    await expect(
      page.getByRole("heading", { name: "简历详情", exact: true }),
    ).toBeVisible();
    const layout = page.getByTestId("resume-detail-layout"),
      main = page.getByTestId("resume-detail-main"),
      aside = page.getByTestId("resume-detail-aside");
    await expectWithinViewport(page, layout);
    await expectWithinViewport(page, main);
    await expectWithinViewport(page, aside);
    const mainBox = await visibleBox(main),
      asideBox = await visibleBox(aside);
    if (width >= 1200)
      expect(asideBox.x).toBeGreaterThanOrEqual(mainBox.x + mainBox.width - 1);
    else
      expect(asideBox.y).toBeGreaterThanOrEqual(mainBox.y + mainBox.height - 1);
    await expect(
      main.getByText(candidate.personal_description, { exact: true }),
    ).toBeVisible();
    await expectWithinViewport(
      page,
      main.getByText(candidate.personal_description, { exact: true }),
    );
    await expectWithinViewport(
      page,
      main.getByText(candidate.work_experiences[0].content, { exact: true }),
    );
    await expectWithinViewport(
      page,
      main.getByText(candidate.project_experiences[0].content, { exact: true }),
    );
    for (const label of [
      "期望地区",
      "期望职位",
      "期望薪资",
      "求职状态",
      "在线状态",
    ])
      await expect(main.getByText(label, { exact: true })).toBeVisible();
    const rawText = main.getByText(longRawText, { exact: true });
    await expect(rawText).toHaveText(longRawText);
    await expect(rawText).toHaveCSS("max-height", "420px");
    await expect(rawText).toHaveCSS("overflow-y", "auto");
    await expectWithinViewport(page, rawText);
    await expect(aside.getByText(longAIReason, { exact: true })).toHaveCount(2);
    const rawPreview = page.getByRole("button", {
      name: "查看完整接口数据",
      exact: false,
    });
    await rawPreview.click();
    await expect(
      page.getByText(`candidate_name: "${candidateName}"`, { exact: true }),
    ).toBeVisible();
    await rawPreview.click();
    await expect(
      page.getByText(`candidate_name: "${candidateName}"`, { exact: true }),
    ).toBeHidden();
    const scrollBox = await visibleBox(page.locator(".goodhr-admin-content"));
    await page.mouse.move(scrollBox.x + 12, scrollBox.y + 40);
    await page.mouse.wheel(0, -10000);
    await expect(
      page.getByRole("heading", { name: "简历详情", exact: true }),
    ).toBeInViewport();
    await saveLayoutScreenshot(page, `resume-detail-${width}`);
  });
}

test("用户金额统计保留负数和充值金额，搜索不改变全局统计，说明可查看", async ({ page }) => {
  const state = mocks.get(page)!;
  state.userStats = {
    today_registered_count: 2,
    agent_binding_count: 797,
    remaining_ai_balance: "-12.3400",
    ai_recharge_amount: "2468.90",
  };
  await page.setViewportSize({ width: 1390, height: 1000 });
  await page.goto("/admin/users");
  const remaining = page.getByTestId("users-remaining-ai-balance");
  const recharge = page.getByTestId("users-ai-recharge-amount");
  await expect(remaining).toHaveText("剩余余额 -12.3400元");
  await expect(recharge).toHaveText("充值金额 2468.90元");
  await expect(remaining).toHaveCSS("white-space", "nowrap");
  const remainingBox = await visibleBox(remaining);
  const rechargeBox = await visibleBox(recharge);
  expect(Math.abs(remainingBox.y - rechargeBox.y)).toBeLessThan(2);
  await remaining.focus();
  await expect(page.getByRole("tooltip")).toHaveText(
    "全体用户 AI 余额合计，减去每人 0.50 元赠送额。",
  );
  await page.getByLabel("搜索用户", { exact: true }).fill("只查一个账号");
  await page.getByRole("main").getByRole("button", { name: /搜索$/ }).click();
  await expect.poll(() => state.calls.some((call) =>
    call.path === "/api/admin/users" && new URLSearchParams(call.query).get("q") === "只查一个账号",
  )).toBe(true);
  await expect(remaining).toHaveText("剩余余额 -12.3400元");
  await expect(recharge).toHaveText("充值金额 2468.90元");
  await recharge.hover();
  await expect(page.getByRole("tooltip")).toHaveText(
    "只合计支付成功的 AI 余额充值订单，不含会员订单、取消或失败的订单。",
  );
  expect(state.calls.some((call) => call.path === "/api/admin/users" && call.method !== "GET")).toBe(false);
  await saveLayoutScreenshot(page, "users-ai-finance-desktop");
  state.userStats = {
    remaining_ai_balance: "-90071992547409.9301",
    ai_recharge_amount: "90071992547409.93",
  };
  await page.getByRole("main").getByRole("button", { name: /刷新$/ }).click();
  await expect(remaining).toHaveText("剩余余额 -90071992547409.9301元");
  await expect(recharge).toHaveText("充值金额 90071992547409.93元");
});

test("用户金额统计在手机上可换行，缺失无效金额显示未知，零金额正常显示", async ({ page }) => {
  const state = mocks.get(page)!;
  await page.setViewportSize({ width: 390, height: 1000 });
  await page.goto("/admin/users");
  const remaining = page.getByTestId("users-remaining-ai-balance");
  const recharge = page.getByTestId("users-ai-recharge-amount");
  await expect(remaining).toHaveText("剩余余额 --元");
  await expect(recharge).toHaveText("充值金额 --元");
  await expectWithinViewport(page, remaining);
  await expectWithinViewport(page, recharge);
  state.userStats = { remaining_ai_balance: "not-a-number", ai_recharge_amount: "0x10" };
  await page.getByRole("main").getByRole("button", { name: /刷新$/ }).click();
  await expect(remaining).toHaveText("剩余余额 --元");
  await expect(recharge).toHaveText("充值金额 --元");
  state.userStats = { remaining_ai_balance: "0.0000", ai_recharge_amount: 0 };
  await page.getByRole("main").getByRole("button", { name: /刷新$/ }).click();
  await expect(remaining).toHaveText("剩余余额 0.0000元");
  await expect(recharge).toHaveText("充值金额 0.00元");
  await expectWithinViewport(page, remaining);
  await expectWithinViewport(page, recharge);
  expect(state.calls.some((call) => call.path === "/api/admin/users" && call.method !== "GET")).toBe(false);
  await saveLayoutScreenshot(page, "users-ai-finance-mobile");
});

test("1390px 用户长邮箱、余额与日期保持横排，流程摘要 Tooltip 保留全文", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1390, height: 1000 });
  await page.goto("/admin/users");
  const row = page
    .locator(".ant-table-tbody > tr[data-row-key]")
    .filter({ hasText: longEmail });
  await expect(row).toBeVisible();
  const cells = row.locator("td");
  const table = page.locator(".ant-table-content table"),
    content = page.locator(".ant-table-content");
  await expect(table).toHaveCSS("table-layout", "fixed");
  const tableBox = await visibleBox(table),
    contentBox = await visibleBox(content);
  expect(tableBox.width).toBeGreaterThanOrEqual(1389);
  expect(tableBox.width).toBeGreaterThan(contentBox.width + 100);
  await expect(content).toHaveCSS("overflow-x", "auto");
  for (const [index, minimum] of [
    [0, 260],
    [2, 115],
    [3, 205],
    [5, 225],
  ] as const)
    expect((await visibleBox(cells.nth(index))).width).toBeGreaterThanOrEqual(
      minimum,
    );
  await expect(cells.nth(2).getByText("￥12345.67", { exact: true })).toHaveCSS(
    "white-space",
    "nowrap",
  );
  await expect(cells.nth(3).getByText(/^注册：/)).toHaveCSS(
    "white-space",
    "nowrap",
  );
  await expect(cells.nth(3).getByText(/^登录：/)).toHaveCSS(
    "white-space",
    "nowrap",
  );
  expect((await visibleBox(row)).height).toBeLessThan(230);
  await expectWithinViewport(page, content);
  await expect(
    row.getByRole("button", { name: "解绑设备", exact: true }),
  ).toBeInViewport();
  await page.mouse.move(contentBox.x + contentBox.width / 2, contentBox.y + 50);
  await page.mouse.wheel(1200, 0);
  await expect(
    row.getByRole("button", { name: "解绑设备", exact: true }),
  ).toBeInViewport();
  const flow = cells.nth(5).locator(".ant-tag");
  const summary = await flow.textContent();
  expect(Array.from(summary || "")).toHaveLength(40);
  expect(summary).toMatch(/\.\.\.$/);
  expect(summary).toContain("😀");
  await flow.hover();
  await expect(page.getByRole("tooltip")).toHaveText(fullFlowLabel);
  await saveLayoutScreenshot(page, "users-desktop-long");
});

test("390px 用户卡片和长流程不溢出，Tooltip 全文仍可查看", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 1000 });
  await page.goto("/admin/users");
  const identity = page.locator(
    `.ant-typography[title="${longEmail}"]:visible`,
  );
  const card = identity.locator("xpath=ancestor::div[.//button][1]");
  await expectWithinViewport(page, card);
  await expectWithinViewport(page, identity);
  await expectWithinViewport(
    page,
    card.getByText("￥12345.67", { exact: true }),
  );
  const flow = card.getByText(/卡在：启动本地程序/);
  await expectWithinViewport(page, flow);
  const summary = await flow.textContent();
  expect(Array.from(summary || "")).toHaveLength(40);
  expect(summary).toMatch(/\.\.\.$/);
  await flow.hover();
  await expect(page.getByRole("tooltip")).toHaveText(fullFlowLabel);
  await expectWithinViewport(page, page.getByRole("tooltip"));
  await saveLayoutScreenshot(page, "users-mobile-long");
});

test("默认确认、编辑及额外动作弹框按钮靠右，下拉框可按中文和会员标签筛选", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1390, height: 1000 });
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "清空简历库", exact: true }).click();
  let dialog = page.getByRole("dialog", { name: "清空简历库", exact: true });
  await expectFooterRight(dialog);
  await saveLayoutScreenshot(page, "clear-resume-right");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toBeHidden();
  expect(
    mocks
      .get(page)!
      .calls.some(
        (call) => call.path === "/api/candidates" && call.method === "DELETE",
      ),
  ).toBe(false);
  const position = page.getByLabel("岗位", { exact: true });
  await position.click();
  await position.fill("成都");
  await expect(
    page.locator(
      ".ant-select-dropdown:visible .ant-select-item-option-content",
    ),
  ).toHaveText(["成都招聘产品经理"]);
  await page
    .locator(".ant-select-dropdown:visible .ant-select-item-option-content")
    .filter({ hasText: "成都招聘产品经理" })
    .click();
  await expect
    .poll(() =>
      mocks
        .get(page)!
        .calls.some(
          (call) =>
            call.path === "/api/candidates" &&
            new URLSearchParams(call.query).get("position_id") ===
              "layout-position",
        ),
    )
    .toBe(true);
  await page.goto("/admin/users");
  await page.getByRole("button", { name: "天数", exact: true }).first().click();
  dialog = page.getByRole("dialog");
  await expectFooterRight(dialog);
  const membership = dialog.getByLabel("会员类型", { exact: true });
  await membership.fill("Plus");
  await expect(
    page.locator(
      ".ant-select-dropdown:visible .ant-select-item-option-content",
    ),
  ).toHaveText(["Plus 基础版"]);
  await page
    .locator(".ant-select-dropdown:visible .ant-select-item-option-content")
    .filter({ hasText: "Plus 基础版" })
    .click();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await page.goto("/admin/team");
  await page.getByRole("button", { name: "编辑", exact: true }).first().click();
  dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("button", { name: "移出团队", exact: true }),
  ).toBeVisible();
  await expectFooterRight(dialog);
  await saveLayoutScreenshot(page, "team-extra-actions-right");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toBeHidden();
  await page.setViewportSize({ width: 390, height: 1000 });
  await page
    .locator(".ant-table-tbody > tr[data-row-key]")
    .filter({ hasText: "pending@example.invalid" })
    .getByRole("button", { name: "编辑", exact: true })
    .click();
  dialog = page.getByRole("dialog", { name: "编辑待确认邀请", exact: true });
  await expect(
    dialog.getByRole("button", { name: "取消邀请", exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "重发邮件", exact: true }),
  ).toBeVisible();
  await expectFooterRight(dialog);
  await saveLayoutScreenshot(page, "team-wrapped-extra-actions-right");
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  expect(
    mocks.get(page)!.calls.filter((call) => call.method === "DELETE"),
  ).toHaveLength(0);
});

for (const entry of [
  { path: "/admin/logs", label: "日志类型", search: "错误", matches: ["错误"] },
  {
    path: "/admin/mail",
    label: "发送对象",
    search: "全部用户",
    matches: ["全部用户"],
  },
  {
    path: "/admin/team-stats",
    label: "时间周期",
    search: "上月",
    matches: ["上月"],
  },
  { path: "/admin/users", label: "每页", search: "50", matches: ["50"] },
  {
    path: "/admin/subscription",
    label: "退款类型",
    search: "余额",
    matches: ["AI 余额退款"],
    open: "退款",
    modal: "申请退款",
    cancel: "先不退",
  },
  {
    path: "/admin/subscription",
    label: "当前模型",
    search: "qwen-test",
    matches: ["本地模拟模型 · qwen-test"],
    open: "当前模型：qwen-test",
    modal: "选择当前模型",
    cancel: "先不改",
    model: true,
  },
]) {
  test(`${entry.path} ${entry.label}支持输入筛选，Escape不提交或误关父弹框`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1390, height: 1000 });
    await page.goto(entry.path);
    if (entry.open)
      await page.getByRole("button", { name: entry.open, exact: true }).click();
    const scope = entry.modal
      ? page.getByRole("dialog", { name: entry.modal, exact: true })
      : page.getByRole("main");
    const select = scope.getByLabel(entry.label, { exact: true });
    const mutationsBefore = businessMutations(page).length;
    await select.fill(entry.search);
    await expect(
      page.locator(
        ".ant-select-dropdown:visible .ant-select-item-option-content",
      ),
    ).toHaveText(entry.matches);
    await select.press("Escape");
    await expect(page.locator(".ant-select-dropdown:visible")).toHaveCount(0);
    await expect(scope).toBeVisible();
    if (entry.model) {
      await select.fill("视觉模型");
      await expect(
        page.locator(
          ".ant-select-dropdown:visible .ant-select-item-option-content",
        ),
      ).toHaveText(["视觉模型 · vision-test"]);
      await select.press("Escape");
      await expect(scope).toBeVisible();
    }
    if (entry.cancel) {
      await expectFooterRight(scope);
      await scope
        .getByRole("button", { name: entry.cancel, exact: true })
        .click();
    }
    expect(businessMutations(page)).toHaveLength(mutationsBefore);
    if (entry.path === "/admin/mail") {
      mocks.get(page)!.phase = "初次编辑邮件正文";
      const editor = page.locator('[contenteditable="true"]').first();
      let mailDraft = "仅供本地模拟的邮件正文，不发送。";
      await editor.fill(mailDraft);
      await expect(editor).toContainText(mailDraft);
      for (const cycle of [1, 2, 3]) {
        mocks.get(page)!.phase = `第${cycle}次切到批量调整`;
        await page
          .locator(".ant-radio-button-wrapper")
          .filter({ hasText: "调整天数、余额" })
          .click();
        await expect(
          page.getByRole("radio", { name: "调整天数、余额", exact: false }),
        ).toBeChecked();
        mocks.get(page)!.phase = `第${cycle}次返回邮件`;
        await page
          .locator(".ant-radio-button-wrapper")
          .filter({ hasText: "发邮件" })
          .click();
        await expect(
          page.getByRole("radio", { name: "发邮件", exact: false }),
        ).toBeChecked();
        const recreatedEditor = page
          .locator('[contenteditable="true"]')
          .first();
        await expect(recreatedEditor).toContainText(mailDraft);
        mailDraft += ` 第${cycle}次往返编辑。`;
        await recreatedEditor.fill(mailDraft);
        await expect(recreatedEditor).toContainText(mailDraft);
      }
      await saveLayoutScreenshot(page, "mail-editor-local-only");
      mocks.get(page)!.phase = "邮件编辑后通过侧栏进入用户管理";
      await page
        .getByRole("menuitem", { name: "用户管理", exact: true })
        .click();
      await expect(
        page.getByRole("heading", { name: "用户管理", exact: true }),
      ).toBeVisible();
      await page
        .getByLabel("搜索用户", { exact: true })
        .fill("SPA 退出后编辑普通输入框");
      mocks.get(page)!.phase = "邮件编辑后通过侧栏进入岗位管理";
      await page
        .getByRole("menuitem", { name: "岗位管理", exact: true })
        .click();
      await expect(
        page.getByRole("heading", { name: "岗位管理", exact: true }),
      ).toBeVisible();
      for (const path of ["/admin/users", "/admin/positions"]) {
        mocks.get(page)!.phase = `邮件编辑后访问${path}`;
        const response = await page.goto(path);
        expect(response?.status()).toBe(200);
        await expect(
          page.getByRole("heading", {
            name: path.endsWith("users") ? "用户管理" : "岗位管理",
            exact: true,
          }),
        ).toBeVisible();
      }
      expect(businessMutations(page)).toHaveLength(mutationsBefore);
    }
  });
}
