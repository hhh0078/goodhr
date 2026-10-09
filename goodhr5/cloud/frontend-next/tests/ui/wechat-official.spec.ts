/** 本文件用全模拟云端及本地接口验证公众号弹框互斥、绑定码生命周期、私有密钥留存和移动端布局。 */
import { test, expect, type Locator, type Page } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const origin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190")
  .origin;
const artifactDir = "artifacts/wechat-follow-compact-qa";
const qrImage = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==",
  "base64",
);
type MockState = {
  enabled: boolean;
  configured: boolean;
  bound: boolean;
  gifted: boolean;
  password: boolean;
  trial: boolean;
  announcement: boolean;
  version: string;
  target: string;
  codeLifetime: number;
  rewardDays: number;
  giftDays: number;
  legacyCode: boolean;
  onboardingConfig: Record<string, unknown>;
  bindingError: boolean;
  statusError: boolean;
  qrError: boolean;
  realQR: boolean;
  bindingGate?: Promise<void>;
  statusGate?: Promise<void>;
  updateGate?: Promise<void>;
  config: Record<string, unknown>;
  calls: { path: string; method: string; body: any }[];
  errors: string[];
  codeCount: number;
  localOnline: boolean;
  runtimeReady: boolean;
  teamInvitation: boolean;
};
const mocks = new WeakMap<Page, MockState>();

test.use({
  viewport: { width: 1390, height: 1000 },
  storageState: {
    cookies: [],
    origins: [
      {
        origin,
        localStorage: [
          { name: "goodhr5_access_token", value: "wechat-local-test-only" },
        ],
      },
    ],
  },
});

/** publicStatus 模拟只包含本人公开绑定状态的接口，不返回公众号密钥。 */
function publicStatus(state: MockState) {
  return {
    enabled: state.enabled,
    configured: state.configured,
    account_name: "GoodHR 测试公众号",
    original_id: "gh_ce9d36869c26",
    qr_image_url: "https://open.weixin.qq.com/qr/code?username=gh_ce9d36869c26",
    reward_days: state.rewardDays,
    gift_days: state.gifted ? state.giftDays || state.rewardDays : 0,
    bound: state.bound,
    followed: state.bound,
    gift_granted: state.gifted,
  };
}

/** mockWechat 拦截全部 API、写请求和外部图片，任何测试都不会真的登录、绑定或启动本地任务。 */
async function mockWechat(page: Page, options: Partial<MockState> = {}) {
  const state: MockState = {
    enabled: true,
    configured: true,
    bound: false,
    gifted: false,
    password: false,
    trial: false,
    announcement: false,
    version: "6.2.10.1",
    target: "6.2.10.1",
    codeLifetime: 600,
    rewardDays: 2,
    giftDays: 0,
    legacyCode: false,
    onboardingConfig: {
      trial_days: 7,
      wechat_follow_reward_days: 2,
      local_agent: [
        { version: "6.2.10.1", url: "https://example.invalid/agent.exe" },
      ],
      runtime_components: { retained: true },
      unknown_future_key: { preserve: [1, 2, 3] },
    },
    bindingError: false,
    statusError: false,
    qrError: false,
    realQR: process.env.UI_REAL_QR === "1",
    config: {
      enabled: false,
      account_name: "GoodHR",
      app_id: "wx-test-only",
      original_id: "gh_ce9d36869c26",
      token: "mock-token-not-secret",
      encoding_aes_key: "",
      encoding_aes_key_configured: true,
      message_mode: "safe",
      reward_days: 5,
      qr_image_url: "",
      callback_url: "https://goodhr.58it.cn/api/wechat-official/callback",
    },
    calls: [],
    errors: [],
    codeCount: 0,
    localOnline: true,
    runtimeReady: true,
    teamInvitation: false,
    ...options,
  };
  mocks.set(page, state);
  page.on("pageerror", (error) =>
    state.errors.push(error.stack || error.message),
  );
  await page.route("**/*", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      method = request.method();
    if (url.origin === origin && !url.pathname.startsWith("/api/"))
      return route.continue();
    if (url.hostname === "open.weixin.qq.com") {
      if (state.qrError) return route.abort();
      // 交付截图可只读取微信公开二维码，所有云端和本地接口仍严格模拟。
      if (state.realQR) return route.continue();
      return route.fulfill({ contentType: "image/png", body: qrImage });
    }
    if (
      ![
        origin,
        "http://127.0.0.1:8084",
        "http://127.0.0.1:43129",
        "https://goodhr.58it.cn",
      ].includes(url.origin)
    )
      return route.abort();
    const headers = {
      "access-control-allow-origin": "*",
      "access-control-allow-headers": "*",
      "access-control-allow-methods": "*",
    };
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    state.calls.push({
      path: url.pathname,
      method,
      body: request.postData() ? request.postDataJSON() : null,
    });
    let status = 200,
      data: any = { ok: true };
    if (url.port === "43129") {
      if (!state.localOnline) return route.abort("connectionrefused");
      if (url.pathname === "/health") data = { version: state.version };
      else if (url.pathname === "/api/v1/runtime/status")
        data = {
          version: state.version,
          node_installed: state.runtimeReady,
          camoufox_installed: state.runtimeReady,
          system_runtime: { ready: true, required: false },
        };
      else if (url.pathname === "/api/v1/app-update/status") {
        if (state.updateGate) await state.updateGate;
        data = {
          current_version: state.version,
          stage: "idle",
          running: false,
        };
      }
      data = { success: true, data };
    } else if (url.pathname === "/api/auth/me")
      data = {
        user: {
          email: "wechat-test@example.invalid",
          role: "super_admin",
          has_openid: state.bound,
          wechat_follow_gift_granted: state.gifted,
        },
        requires_password_setup: state.password,
        show_trial_welcome: state.trial,
      };
    else if (url.pathname === "/api/auth/password/setup") {
      state.password = false;
    } else if (url.pathname === "/api/auth/trial-welcome/ack") {
      state.trial = false;
    } else if (url.pathname === "/api/subscription/status")
      data = {
        subscription: {
          active: true,
          member_type: "max",
          allow_ai: true,
          remaining_days: 30,
          member_name: "Max 全能版",
          expires_at: "2026-11-10T00:00:00Z",
        },
      };
    else if (url.pathname === "/api/ai-wallet")
      data = { wallet: { balance: "18.00" } };
    else if (url.pathname === "/api/runtime/config")
      data = {
        config: {
          local_agent: [
            {
              version: state.target,
              url: "https://example.invalid/update.exe",
              sha256: "a".repeat(64),
            },
          ],
        },
      };
    else if (url.pathname === "/api/system/app-config")
      data = {
        config: {
          announcements_enabled: state.announcement,
          announcements: [
            {
              id: "wechat-priority-test",
              enabled: true,
              once: false,
              title: "先看公告",
              content: "本地模拟公告",
            },
          ],
        },
      };
    else if (url.pathname === "/api/tenants/invitations/pending")
      data = {
        invitations: state.teamInvitation
          ? [
              {
                id: "wechat-priority-team",
                tenant_name: "模拟邀请团队",
                invited_by_email: "team@example.invalid",
              },
            ]
          : [],
      };
    else if (url.pathname === "/api/config/user-ai")
      data = {
        config: { builtin_model: "test-model" },
        available_models: [{ value: "test-model", label: "测试模型" }],
      };
    else if (url.pathname === "/api/user-flow")
      data = { flow: { state: "completed", stage: "completed", steps: {} } };
    else if (url.pathname === "/api/wechat-official/status") {
      if (state.statusGate) await state.statusGate;
      if (state.statusError) {
        status = 500;
        data = { ok: false, error: "公众号状态暂时没读到" };
      } else data = publicStatus(state);
    } else if (url.pathname === "/api/wechat-official/binding") {
      state.codeCount++;
      if (state.bindingGate) await state.bindingGate;
      if (state.bindingError) {
        status = 503;
        data = { ok: false, error: "公众号还没准备好，再试一次就好" };
      } else
        data = {
          ok: true,
          binding_code: `${state.legacyCode ? "ABCD" : ""}${String(1233 + state.codeCount).padStart(4, "0")}`,
          expires_at: new Date(
            Date.now() + state.codeLifetime * 1000,
          ).toISOString(),
          status: publicStatus(state),
        };
    } else if (url.pathname === "/api/admin/system/configs/")
      data = {
        configs: [
          {
            config_key: "system.wechat_official_account",
            config_value: JSON.stringify(state.config),
            description: "公众号绑定与会员奖励",
          },
          {
            config_key: "system.onboarding_config",
            config_value: JSON.stringify(state.onboardingConfig),
            description: "本地程序与组件、关注奖励配置",
          },
          {
            config_key: "system.guide",
            config_value: JSON.stringify({ help: "原配置保持" }),
          },
        ],
      };
    else if (
      url.pathname ===
        "/api/admin/system/configs/system.wechat_official_account" &&
      method === "PUT"
    ) {
      const saved = JSON.parse(request.postDataJSON().config_value);
      state.config = {
        ...saved,
        encoding_aes_key: "",
        encoding_aes_key_configured:
          state.config.encoding_aes_key_configured ||
          Boolean(saved.encoding_aes_key),
        callback_url: "https://goodhr.58it.cn/api/wechat-official/callback",
      };
    } else if (
      url.pathname === "/api/admin/system/configs/system.onboarding_config" &&
      method === "PUT"
    ) {
      state.onboardingConfig = JSON.parse(request.postDataJSON().config_value);
      state.rewardDays =
        Number(state.onboardingConfig.wechat_follow_reward_days) || 2;
    } else if (url.pathname === "/api/admin/users")
      data = {
        total: 2,
        page: 1,
        users: [
          {
            email: "bound@example.invalid",
            role: "user",
            status: "active",
            has_openid: true,
            ai_balance: "3.25",
          },
          {
            email: "empty@example.invalid",
            role: "user",
            status: "active",
            has_openid: false,
            ai_balance: "2.00",
          },
        ],
      };
    else
      data = {
        ok: true,
        positions: [],
        candidates: [],
        total: 0,
        config: {},
        accounts: [],
        platforms: [],
        prompts: {},
      };
    return route.fulfill({
      status,
      headers,
      contentType: "application/json",
      body: JSON.stringify(data),
    });
  });
  return state;
}

/** followDialog 等待弹框入场结束，再使用真实键盘、按钮和遮罩操作。 */
async function followDialog(
  page: Page,
  title = `关注领取 ${mocks.get(page)?.rewardDays ?? 2} 天会员`,
) {
  const dialog = page.getByRole("dialog", { name: title, exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveCSS("animation-name", "none");
  return dialog;
}

/** bindingCalls 统计真实界面发出的模拟绑定请求，检测自动重复创建与取消后继续请求。 */
function bindingCalls(state: MockState) {
  return state.calls.filter(
    (call) => call.path === "/api/wechat-official/binding",
  );
}
/** authCalls 统计会员刷新时读到的本人身份，不把状态轮询误认为会话刷新。 */
function authCalls(state: MockState) {
  return state.calls.filter((call) => call.path === "/api/auth/me");
}

/** expectCompactFollow 验证两个底部按钮同排、右对齐，以及关键说明红色大号粗体。 */
async function expectCompactFollow(dialog: Locator, alreadyGifted = false) {
  await expect(dialog.getByRole("button")).toHaveCount(2);
  await expect(
    dialog.getByRole("button", { name: /复制|生成绑定码|Close/ }),
  ).toHaveCount(0);
  const cancel = dialog.getByRole("button", { name: "暂不关注", exact: true });
  const confirm = dialog.getByRole("button", { name: "重新生成", exact: true });
  await expect(dialog.getByLabel("公众号绑定码")).toBeVisible();
  await expect
    .poll(async () => {
      const cancelBox = await cancel.boundingBox(),
        confirmBox = await confirm.boundingBox();
      return Math.abs(cancelBox!.y - confirmBox!.y);
    })
    .toBeLessThan(2);
  const cancelBox = await cancel.boundingBox(),
    confirmBox = await confirm.boundingBox();
  expect(cancelBox!.x + cancelBox!.width).toBeLessThan(confirmBox!.x);
  await expect(cancel).toHaveCSS("border-radius", "999px");
  await expect(confirm).toHaveCSS("border-radius", "999px");
  const instruction = dialog.getByText(
    "先扫码关注，再把下面的绑定码发给公众号。",
    { exact: true },
  );
  await expect(instruction).toHaveCSS("color", "rgb(207, 19, 34)");
  await expect(instruction).toHaveCSS("font-size", "19px");
  await expect(instruction).toHaveCSS("font-weight", "800");
  const title = dialog.locator(".ant-modal-title");
  await expect(
    title.getByText(alreadyGifted ? "关注公众号" : /关注领取 \d+ 天会员/),
  ).toHaveCSS("color", "rgb(207, 19, 34)");
  await expect(
    title.getByText(alreadyGifted ? "关注公众号" : /关注领取 \d+ 天会员/),
  ).toHaveCSS("font-weight", "800");
  await expect(
    dialog.getByText("GoodHR 测试公众号", { exact: true }),
  ).toHaveCount(0);
  await expect(dialog.getByLabel("公众号绑定码")).toHaveCSS(
    "font-size",
    "42px",
  );
}

/** waitForOfficialQRCode 交付截图只等公开图片读取完成，不执行真实登录或绑定请求。 */
async function waitForOfficialQRCode(dialog: Locator, state: MockState) {
  if (!state.realQR) return;
  const image = dialog.getByRole("img", {
    name: "公众号关注二维码",
    exact: true,
  });
  await expect(image).toHaveJSProperty("complete", true);
  await expect(image).toHaveJSProperty("naturalWidth", 430);
}

test.afterEach(async ({ page }) => {
  expect(mocks.get(page)?.errors || []).toEqual([]);
});

test("空 OpenID 自动提醒，Escape 关闭记住会话，控制台固定入口可重开", async ({
  page,
}) => {
  const state = await mockWechat(page);
  await page.goto("/admin");
  const dialog = await followDialog(page);
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1234");
  expect(bindingCalls(state)).toHaveLength(1);
  await expectCompactFollow(dialog);
  await waitForOfficialQRCode(dialog, state);
  await expect(
    dialog.getByText(/绑定码剩余 (10 分 00 秒|9 分 5\d 秒)/),
  ).toBeVisible();
  await mkdir(artifactDir, { recursive: true });
  await page.screenshot({
    path: `${artifactDir}/follow-desktop.png`,
    animations: "disabled",
  });
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "控制台", exact: true }),
  ).toBeVisible();
  await expect(dialog).toBeHidden();
  expect(bindingCalls(state)).toHaveLength(1);
  await page.getByRole("button", { name: "关注领会员", exact: true }).click();
  await expect(
    (await followDialog(page)).getByLabel("公众号绑定码"),
  ).toHaveValue("1235");
  await page.mouse.click(3, 3);
  await expect(dialog).toBeHidden();
});

test("已领奖励不重复承诺天数，仍可手动重新绑定", async ({ page }) => {
  await mockWechat(page, { gifted: true });
  await page.goto("/admin");
  const dialog = await followDialog(page, "关注公众号");
  await expect(dialog.getByText(/天会员|奖励以前/)).toHaveCount(0);
  await expectCompactFollow(dialog, true);
  await dialog.getByRole("button", { name: "暂不关注" }).click();
  await page.getByRole("button", { name: "关注公众号", exact: true }).click();
  await expect(dialog).toBeVisible();
});

for (const scenario of ["停用", "未配置", "已绑定", "读取失败"] as const) {
  test(`${scenario}时不自动提示、不创建绑定码，入口隐藏`, async ({ page }) => {
    const state = await mockWechat(
      page,
      scenario === "停用"
        ? { enabled: false }
        : scenario === "未配置"
          ? { configured: false }
          : scenario === "已绑定"
            ? { bound: true, gifted: true }
            : { statusError: true },
    );
    await page.goto("/admin");
    await expect(
      page.getByRole("heading", { name: "控制台", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "关注领会员", exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "关注公众号", exact: true }),
    ).toHaveCount(0);
    expect(bindingCalls(state)).toHaveLength(0);
  });
}

test("生成失败可重试，绑定成功只刷新会话一次并停止轮询", async ({ page }) => {
  const state = await mockWechat(page, { bindingError: true });
  await page.goto("/admin");
  const dialog = await followDialog(page);
  await expect(
    dialog.getByText("公众号还没准备好，再试一次就好"),
  ).toBeVisible();
  state.bindingError = false;
  await dialog.getByRole("button", { name: "重新生成", exact: true }).click();
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1235");
  const authCount = authCalls(state).length;
  state.bound = true;
  state.gifted = true;
  await expect(dialog).toBeHidden();
  await expect.poll(() => authCalls(state).length).toBe(authCount + 1);
  await expect(
    page.getByText("公众号绑定好了，2 天会员已到账。继续搬砖吧。"),
  ).toBeVisible();
  const statusCount = state.calls.filter(
    (call) => call.path === "/api/wechat-official/status",
  ).length;
  await page.waitForTimeout(2500);
  expect(
    state.calls.filter((call) => call.path === "/api/wechat-official/status"),
  ).toHaveLength(statusCount);
});

test("绑定码过期停查，仅底部重新生成替换旧码", async ({ page }) => {
  const state = await mockWechat(page, { codeLifetime: 2 });
  await page.goto("/admin");
  const dialog = await followDialog(page);
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1234");
  await expect(
    dialog.getByText("绑定码已过期，重新生成一份就好。"),
  ).toBeVisible();
  await expect(dialog.getByRole("button")).toHaveCount(2);
  await expect(
    dialog.getByRole("button", { name: "重新生成", exact: true }),
  ).toBeEnabled();
  state.codeLifetime = 600;
  await dialog.getByRole("button", { name: "重新生成", exact: true }).click();
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1235");
  await dialog.getByRole("button", { name: "暂不关注" }).click();
  const count = state.calls.filter(
    (call) => call.path === "/api/wechat-official/status",
  ).length;
  await page.waitForTimeout(2500);
  expect(
    state.calls.filter((call) => call.path === "/api/wechat-official/status"),
  ).toHaveLength(count);
});

test("生成中仍能取消，迟到的返回不重开、不继续轮询", async ({ page }) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const state = await mockWechat(page, { bindingGate: gate });
  try {
    await page.goto("/admin");
    const dialog = await followDialog(page);
    await expect.poll(() => bindingCalls(state).length).toBe(1);
    await expect(
      dialog.getByText("正在准备绑定码，暂不关注也能关掉。"),
    ).toBeVisible();
    await dialog.getByRole("button", { name: "暂不关注" }).click();
    release();
    await expect(dialog).toBeHidden();
    const count = state.calls.filter(
      (call) => call.path === "/api/wechat-official/status",
    ).length;
    await page.waitForTimeout(2500);
    expect(
      state.calls.filter((call) => call.path === "/api/wechat-official/status"),
    ).toHaveLength(count);
    expect(bindingCalls(state)).toHaveLength(1);
    state.bindingGate = undefined;
    await page.getByRole("button", { name: "关注领会员", exact: true }).click();
    await expect(
      (await followDialog(page)).getByLabel("公众号绑定码"),
    ).toHaveValue("1235");
  } finally {
    release();
  }
});

test("手动打开后公众号被停用，旧绑定码停止轮询并可取消", async ({ page }) => {
  const state = await mockWechat(page);
  await page.goto("/admin");
  const dialog = await followDialog(page);
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1234");
  await dialog.getByRole("button", { name: "暂不关注" }).click();
  await page.getByRole("button", { name: "关注领会员", exact: true }).click();
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1235");
  state.enabled = false;
  await expect(
    dialog.getByText("公众号暂未开启，先继续招聘就好"),
  ).toBeVisible();
  const count = state.calls.filter(
    (call) => call.path === "/api/wechat-official/status",
  ).length;
  await page.waitForTimeout(2500);
  expect(
    state.calls.filter((call) => call.path === "/api/wechat-official/status"),
  ).toHaveLength(count);
  await dialog.getByRole("button", { name: "暂不关注" }).click();
  await expect(dialog).toBeHidden();
});

test("同账号解绑后重开再次绑定，每个绑定周期都关闭并只刷新一次", async ({
  page,
}) => {
  const state = await mockWechat(page);
  await page.goto("/admin");
  const first = await followDialog(page);
  await expect(first.getByLabel("公众号绑定码")).toHaveValue("1234");
  const firstAuthCount = authCalls(state).length;
  state.bound = true;
  state.gifted = true;
  await expect(first).toBeHidden();
  await expect.poll(() => authCalls(state).length).toBe(firstAuthCount + 1);
  state.bound = false;
  await page.getByRole("button", { name: "刷新状态", exact: true }).click();
  const entry = page.getByRole("button", { name: "关注公众号", exact: true });
  await expect(entry).toBeVisible();
  await entry.click();
  const second = await followDialog(page, "关注公众号");
  await expect(second.getByLabel("公众号绑定码")).toHaveValue("1235");
  const secondAuthCount = authCalls(state).length;
  state.bound = true;
  await expect(second).toBeHidden();
  await expect.poll(() => authCalls(state).length).toBe(secondAuthCount + 1);
  await expect(
    page.getByText("公众号绑定好了，可以继续搬砖了。", { exact: true }),
  ).toBeVisible();
  expect(bindingCalls(state)).toHaveLength(2);
});

for (const priority of [
  "公告",
  "首次奖励",
  "必填密码",
  "更新",
  "团队邀请",
  "安装提示",
  "必要组件",
] as const) {
  test(`${priority}先处理，公众号提醒后显示`, async ({ page }) => {
    const state = await mockWechat(
      page,
      priority === "公告"
        ? { announcement: true }
        : priority === "首次奖励"
          ? { trial: true }
          : priority === "必填密码"
            ? { password: true }
            : priority === "更新"
              ? { target: "6.2.10.2" }
              : priority === "团队邀请"
                ? { teamInvitation: true }
                : priority === "安装提示"
                  ? { localOnline: false }
                  : { runtimeReady: false },
    );
    await page.goto("/admin");
    const title =
      priority === "公告"
        ? "系统公告"
        : priority === "首次奖励"
          ? "Max 全能版已到账"
          : priority === "必填密码"
            ? "给老账号补一个密码"
            : priority === "更新"
              ? "更新本地程序"
              : priority === "团队邀请"
                ? "有人想拉你进团队"
                : priority === "安装提示"
                  ? "请先安装本地程序"
                  : "准备必要组件";
    const first = page.getByRole("dialog", { name: title, exact: true });
    await expect(first).toBeVisible();
    await expect(
      page.getByRole("dialog", { name: "关注领取 2 天会员", exact: true }),
    ).toBeHidden();
    expect(bindingCalls(state)).toHaveLength(0);
    if (priority === "必填密码") {
      await first.getByLabel("新密码", { exact: true }).fill("test-password");
      await first
        .getByLabel("再输入一次", { exact: true })
        .fill("test-password");
      await first.getByRole("button", { name: "设置密码并继续" }).click();
    } else if (priority === "必要组件") state.runtimeReady = true;
    else
      await first
        .getByRole("button", {
          name:
            priority === "更新"
              ? "暂时忽略"
              : priority === "团队邀请"
                ? "拒绝邀请"
                : priority === "安装提示"
                  ? "关闭"
                  : "我知道了",
          exact: true,
        })
        .click();
    await expect(
      (await followDialog(page)).getByLabel("公众号绑定码"),
    ).toHaveValue("1234");
  });
}

test("业务模型弹框先开时，迟到的公众号状态不抢焦点", async ({ page }) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const state = await mockWechat(page, { updateGate: gate });
  // 本地更新状态尚未返回时先打开业务弹框，随后完成检查也不能让关注提醒抢焦点。
  await page.goto("/admin/subscription");
  await expect
    .poll(
      () =>
        state.calls.filter((call) => call.path === "/api/v1/app-update/status")
          .length,
    )
    .toBeGreaterThan(0);
  await page.getByRole("button", { name: /^当前模型：/ }).click();
  const model = page.getByRole("dialog", { name: "选择当前模型" });
  await expect(model).toBeVisible();
  release();
  await expect(
    page.getByRole("dialog", { name: "关注领取 2 天会员", exact: true }),
  ).toBeHidden();
  expect(bindingCalls(state)).toHaveLength(0);
  await model.getByRole("button", { name: "先不改", exact: true }).click();
  await expect(
    (await followDialog(page)).getByLabel("公众号绑定码"),
  ).toHaveValue("1234");
});

test("公众号专用表单保存空 AES 保留密钥，模式可筛选，只读元数据不提交", async ({
  page,
}) => {
  const state = await mockWechat(page, { bound: true });
  await page.goto("/admin/system-config");
  await page.getByRole("tab", { name: "其他配置", exact: true }).click();
  await expect(page.getByLabel("EncodingAESKey", { exact: true })).toHaveValue(
    "",
  );
  await expect(
    page.getByText("已配置密钥，留空会保留；重新填写才会覆盖。"),
  ).toBeVisible();
  await expect(page.getByLabel("服务器地址（只读）")).toHaveValue(
    "https://goodhr.58it.cn/api/wechat-official/callback",
  );
  await page.getByLabel("公众号名称", { exact: true }).fill("GoodHR 改名测试");
  const mode = page.getByLabel("消息模式", { exact: true });
  await mode.click();
  await mode.fill("兼容");
  await page
    .locator(".ant-select-item-option-content")
    .filter({ hasText: "兼容模式" })
    .click();
  await page.getByLabel("启用公众号绑定").click();
  await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
  await expect(
    page.getByText("公众号配置已保存", { exact: true }),
  ).toBeVisible();
  const put = state.calls.find((call) => call.method === "PUT");
  const saved = JSON.parse(put!.body.config_value);
  expect(saved).toMatchObject({
    enabled: true,
    account_name: "GoodHR 改名测试",
    encoding_aes_key: "",
    message_mode: "compat",
  });
  expect(saved).not.toHaveProperty("callback_url");
  expect(saved).not.toHaveProperty("encoding_aes_key_configured");
  expect(saved).not.toHaveProperty("reward_days");
  await expect(page.getByLabel("EncodingAESKey", { exact: true })).toHaveValue(
    "",
  );
  await page.getByRole("tab", { name: "邀请帮助", exact: true }).click();
  await page.getByRole("menuitem", { name: /帮助中心/ }).click();
  await expect(page.locator(".cm-editor")).toBeVisible();
});

test("新密钥必填且格式校验，旧密钥可留空，公众号禁用可保存草稿", async ({
  page,
}) => {
  const state = await mockWechat(page, {
    bound: true,
    config: {
      enabled: false,
      encoding_aes_key: "",
      encoding_aes_key_configured: false,
    },
  });
  await page.goto("/admin/system-config");
  await page.getByLabel("启用公众号绑定").click();
  await expect(
    page.getByText("请填写公众号 AppID", { exact: true }),
  ).toBeVisible();
  await page.getByLabel("AppID", { exact: true }).fill("wx-local-test");
  await page
    .getByLabel("公众号原始 ID", { exact: true })
    .fill("gh_ce9d36869c26");
  await page
    .getByLabel("服务器配置 Token", { exact: true })
    .fill("local-test-token");
  await expect(
    page.getByText("请填写 EncodingAESKey", { exact: true }),
  ).toBeVisible();
  await page.getByLabel("EncodingAESKey", { exact: true }).fill("short");
  await expect(
    page.getByText("EncodingAESKey 需要 43 位 Base64 字符", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "保存当前配置", exact: true }),
  ).toBeDisabled();
  await page.getByLabel("EncodingAESKey", { exact: true }).fill("A".repeat(43));
  await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
  await expect(
    page.getByText("公众号配置已保存", { exact: true }),
  ).toBeVisible();
  expect(
    JSON.parse(
      state.calls.find((call) => call.method === "PUT")!.body.config_value,
    ).encoding_aes_key,
  ).toBe("A".repeat(43));
  await expect(page.getByLabel("EncodingAESKey", { exact: true })).toHaveValue(
    "",
  );
});

test("AES 支持合法 Base64 加号斜杠，二维码站内路径在保存前拦住", async ({
  page,
}) => {
  const state = await mockWechat(page, { bound: true });
  await page.goto("/admin/system-config");
  const testKey = Buffer.alloc(32, 255).toString("base64").replace(/=$/, "");
  await page.getByLabel("EncodingAESKey", { exact: true }).fill(testKey);
  await page
    .getByLabel("关注二维码图片地址", { exact: true })
    .fill("/local-test-qr.png");
  await expect(
    page.getByText("二维码图片请使用 HTTPS 地址", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "保存当前配置", exact: true }),
  ).toBeDisabled();
  await page.getByLabel("关注二维码图片地址", { exact: true }).fill("");
  await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
  await expect(
    page.getByText("公众号配置已保存", { exact: true }),
  ).toBeVisible();
  expect(
    JSON.parse(
      state.calls.find((call) => call.method === "PUT")!.body.config_value,
    ).encoding_aes_key,
  ).toBe(testKey);
});

test("公众号状态慢响应不阻塞控制台，退出登录后迟到响应不能再弹", async ({
  page,
}) => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const state = await mockWechat(page, { statusGate: gate });
  try {
    await page.goto("/admin");
    await expect(
      page.getByRole("heading", { name: "控制台", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "关注领会员", exact: true }),
    ).toHaveCount(0);
    await expect
      .poll(
        () =>
          state.calls.filter(
            (call) => call.path === "/api/wechat-official/status",
          ).length,
      )
      .toBeGreaterThan(0);
    // 开发模式左下角提示层会盖住按钮中心，点击按钮右侧真实区域，仍保持正常可见性和事件检查。
    await page
      .locator(".goodhr-admin-sidebar")
      .getByRole("button", { name: "退出登录", exact: true })
      .click({ position: { x: 160, y: 16 } });
    await expect(page).toHaveURL(/\/login/);
    release();
    await page.waitForTimeout(500);
    await expect(
      page.getByRole("dialog", { name: "关注领取 2 天会员", exact: true }),
    ).toBeHidden();
    expect(bindingCalls(state)).toHaveLength(0);
  } finally {
    release();
  }
});

test("绑定状态暂时读取失败保留有效码，恢复后完成绑定", async ({ page }) => {
  const state = await mockWechat(page);
  await page.goto("/admin");
  const dialog = await followDialog(page);
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1234");
  state.statusError = true;
  await expect(
    dialog.getByText("公众号状态暂时没读到", { exact: true }),
  ).toBeVisible();
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1234");
  state.statusError = false;
  state.bound = true;
  state.gifted = true;
  await expect(dialog).toBeHidden();
  expect(bindingCalls(state)).toHaveLength(1);
});

test("手机关注弹框可取消、按钮靠右，二维码失败有下一步，用户标签不增加表格列", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const state = await mockWechat(page, { qrError: true });
  await page.goto("/admin");
  const dialog = await followDialog(page);
  await expect(
    dialog.getByText(
      "二维码图片暂时没加载，可以在微信搜索“GoodHR 测试公众号”。",
    ),
  ).toBeVisible();
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1234");
  const box = await dialog.boundingBox();
  expect(box!.width).toBeLessThanOrEqual(390);
  expect(box!.x).toBeGreaterThanOrEqual(0);
  await expectCompactFollow(dialog);
  const confirm = await dialog
    .getByRole("button", { name: "重新生成", exact: true })
    .boundingBox();
  expect(confirm!.x + confirm!.width).toBeGreaterThan(box!.x + box!.width - 40);
  await mkdir(artifactDir, { recursive: true });
  await page.screenshot({
    path: `${artifactDir}/follow-mobile.png`,
    animations: "disabled",
  });
  await dialog.getByRole("button", { name: "暂不关注" }).click();
  await page.goto("/admin/users");
  await expect(
    page.getByText("OpenID 有", { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  await expect(
    page.getByText("OpenID 无", { exact: true }).filter({ visible: true }),
  ).toBeVisible();
  await page.setViewportSize({ width: 1390, height: 1000 });
  await expect(page.locator(".ant-table-thead th")).toHaveCount(7);
  expect(bindingCalls(state)).toHaveLength(1);
});

test("动态七天标题与到账提示跟随服务，历史八位绑定码仍可过渡", async ({
  page,
}) => {
  const state = await mockWechat(page, { rewardDays: 7, legacyCode: true });
  await page.goto("/admin");
  const dialog = await followDialog(page);
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("ABCD1234");
  await expect(
    dialog.getByText("关注领取 7 天会员", { exact: true }),
  ).toBeVisible();
  await expect(dialog.getByRole("button")).toHaveCount(2);
  state.bound = true;
  state.gifted = true;
  await expect(dialog).toBeHidden();
  await expect(
    page.getByText("公众号绑定好了，7 天会员已到账。继续搬砖吧。", {
      exact: true,
    }),
  ).toBeVisible();
});

test("到账后配置变动，成功提示仍显示实际获赠天数", async ({ page }) => {
  const state = await mockWechat(page);
  await page.goto("/admin");
  const dialog = await followDialog(page);
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1234");
  state.bound = true;
  state.gifted = true;
  state.giftDays = 2;
  state.rewardDays = 7;
  await expect(dialog).toBeHidden();
  await expect(
    page.getByText("公众号绑定好了，2 天会员已到账。继续搬砖吧。", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    page.getByText("公众号绑定好了，7 天会员已到账。继续搬砖吧。", {
      exact: true,
    }),
  ).toHaveCount(0);
});

test("关注赠送快捷输入保留注册及未知 JSON，保存后刷新动态状态", async ({
  page,
}) => {
  const state = await mockWechat(page);
  await page.goto("/admin/system-config");
  await (await followDialog(page))
    .getByRole("button", { name: "暂不关注", exact: true })
    .click();
  await page.getByRole("tab", { name: "本地组件", exact: true }).click();
  await page.getByRole("menuitem", { name: /本地程序与组件/ }).click();
  const reward = page.getByLabel("关注赠送", { exact: true });
  await expect(reward).toHaveValue("2");
  await expect(page.getByLabel("注册赠送", { exact: true })).toHaveCount(0);
  await expect(page.locator(".cm-editor")).toBeVisible();
  const original = structuredClone(state.onboardingConfig);
  const statusCount = state.calls.filter(
    (call) => call.path === "/api/wechat-official/status",
  ).length;
  await reward.fill("7");
  await page.getByRole("button", { name: "保存当前配置", exact: true }).click();
  await expect(
    page.getByText("本地程序与组件已保存", { exact: true }),
  ).toBeVisible();
  const put = state.calls.find(
    (call) =>
      call.method === "PUT" && call.path.endsWith("system.onboarding_config"),
  );
  expect(JSON.parse(put!.body.config_value)).toEqual({
    ...original,
    wechat_follow_reward_days: 7,
  });
  await expect
    .poll(
      () =>
        state.calls.filter(
          (call) => call.path === "/api/wechat-official/status",
        ).length,
    )
    .toBeGreaterThan(statusCount);
  await page
    .locator(".goodhr-admin-sidebar")
    .getByRole("menuitem", { name: "控制台", exact: true })
    .click();
  await page.getByRole("button", { name: "关注领会员", exact: true }).click();
  await expect(
    (await followDialog(page)).getByLabel("公众号绑定码"),
  ).toHaveValue("1235");
});

test("关注赠送整数范围与 JSON 校验一致，无效草稿不提交", async ({ page }) => {
  const state = await mockWechat(page, { bound: true });
  await page.goto("/admin/system-config");
  await page.getByRole("tab", { name: "本地组件", exact: true }).click();
  await page.getByRole("menuitem", { name: /本地程序与组件/ }).click();
  const editor = page.locator(".cm-content[contenteditable=true]");
  const save = page.getByRole("button", { name: "保存当前配置", exact: true });
  for (const invalid of [0, 366, 1.5, "7", null]) {
    await editor.fill(
      JSON.stringify({
        ...state.onboardingConfig,
        wechat_follow_reward_days: invalid,
      }),
    );
    await expect(
      page.getByText("关注赠送请填写 1 至 365 的整数天数", { exact: true }),
    ).toBeVisible();
    await expect(save).toBeDisabled();
  }
  await editor.fill("[]");
  await expect(
    page.getByText("本地程序与组件配置需要使用 JSON 对象", { exact: true }),
  ).toBeVisible();
  await expect(save).toBeDisabled();
  expect(state.calls.filter((call) => call.method === "PUT")).toHaveLength(0);
  await editor.fill(
    JSON.stringify({
      ...state.onboardingConfig,
      wechat_follow_reward_days: 365,
    }),
  );
  await expect(save).toBeEnabled();
  await save.click();
  await expect(
    page.getByText("本地程序与组件已保存", { exact: true }),
  ).toBeVisible();
  expect(state.rewardDays).toBe(365);
});

test("窄屏二维码与四位码完整可读，底部两个按钮保持同一行", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 568 });
  await mockWechat(page, { rewardDays: 7 });
  await page.goto("/admin");
  const dialog = await followDialog(page);
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1234");
  await expectCompactFollow(dialog);
  const image = dialog.getByRole("img", {
    name: "公众号关注二维码",
    exact: true,
  });
  await expect(image).toBeVisible();
  const dialogBox = await dialog.boundingBox(),
    codeBox = await dialog.getByLabel("公众号绑定码").boundingBox();
  expect(dialogBox!.x).toBeGreaterThanOrEqual(0);
  expect(dialogBox!.x + dialogBox!.width).toBeLessThanOrEqual(320);
  expect(codeBox!.x).toBeGreaterThanOrEqual(dialogBox!.x);
  expect(codeBox!.x + codeBox!.width).toBeLessThanOrEqual(
    dialogBox!.x + dialogBox!.width,
  );
  await mkdir(artifactDir, { recursive: true });
  await page.screenshot({
    path: `${artifactDir}/follow-mobile-narrow.png`,
    animations: "disabled",
  });
  await dialog.getByRole("button", { name: "暂不关注", exact: true }).click();
  await expect(dialog).toBeHidden();
});

test("手机正常关注二维码布局，关键红字与两按钮可完整查看", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const state = await mockWechat(page);
  await page.goto("/admin");
  const dialog = await followDialog(page);
  await expect(dialog.getByLabel("公众号绑定码")).toHaveValue("1234");
  const image = dialog.getByRole("img", {
    name: "公众号关注二维码",
    exact: true,
  });
  await expect(image).toBeVisible();
  await expectCompactFollow(dialog);
  await waitForOfficialQRCode(dialog, state);
  await expect(dialog.getByText(/二维码图片暂时没加载/)).toHaveCount(0);
  await mkdir(artifactDir, { recursive: true });
  await page.screenshot({
    path: `${artifactDir}/follow-mobile-normal.png`,
    animations: "disabled",
  });
  await dialog.getByRole("button", { name: "暂不关注", exact: true }).click();
  await expect(dialog).toBeHidden();
});
