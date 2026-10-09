/** 本文件通过完整模拟接口回归本地程序更新的锁定、防重、失败重试、串行轮询和安装后自动重连。 */
import { test, expect, type Locator, type Page } from "@playwright/test";

const previewOrigin = new URL(
  process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190",
).origin;
const updateURL = "https://example.invalid/test-only-update.exe";
const updateSHA256 = "a".repeat(64);
const releaseNote = "仅本地模拟的更新说明";

type Deferred = { promise: Promise<void>; release: () => void };
type UpdateProgress = {
  stage: string;
  running: boolean;
  percent?: number;
  received?: number;
  total?: number;
  message?: string;
};
type UpdateMock = {
  version: string;
  target: string;
  offline: boolean;
  startError: boolean;
  progress: UpdateProgress;
  startGate?: Deferred;
  statusGate?: Deferred;
  completionBindGate?: Deferred;
  completionBindGateAttempt: number;
  completionBindFailures: number;
  completionBindAttempts: number;
  completionHealthFailures: number;
  completionHealthEmptyVersions: number;
  completionRuntimeEmptyVersions: number;
  disconnectAfterIdleSnapshot: boolean;
  lastHealthReadAt: number;
  statusCalls: number;
  pendingStatus: number;
  maximumPendingStatus: number;
  errors: string[];
  calls: { path: string; method: string; body: unknown }[];
};
const mocks = new WeakMap<Page, UpdateMock>();

test.use({
  viewport: { width: 1390, height: 1000 },
  storageState: {
    cookies: [],
    origins: [
      {
        origin: previewOrigin,
        localStorage: [
          { name: "goodhr5_access_token", value: "update-test-only-token" },
        ],
      },
    ],
  },
});

/** deferred 暂停一条模拟接口响应，以真实网络等待验证按钮防重和慢请求轮询。 */
function deferred(): Deferred {
  let release!: () => void;
  const promise = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { promise, release };
}

/** mockUpdate 拦截全部业务接口与外部资源，使用本地 Go 相同的响应封装和错误结构。 */
async function mockUpdate(page: Page, options: Partial<UpdateMock> = {}) {
  const state: UpdateMock = {
    version: "6.2.10.1",
    target: "6.2.11.0",
    offline: false,
    startError: false,
    progress: { stage: "idle", running: false, percent: 0 },
    statusCalls: 0,
    pendingStatus: 0,
    maximumPendingStatus: 0,
    completionBindFailures: 0,
    completionBindAttempts: 0,
    completionBindGateAttempt: 0,
    completionHealthFailures: 0,
    completionHealthEmptyVersions: 0,
    completionRuntimeEmptyVersions: 0,
    disconnectAfterIdleSnapshot: false,
    lastHealthReadAt: 0,
    errors: [],
    calls: [],
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
    if (url.origin === previewOrigin && !url.pathname.startsWith("/api/"))
      return route.continue();
    if (
      ![
        previewOrigin,
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
      data: Record<string, unknown> = { ok: true };
    if (url.port === "43129") {
      if (state.offline) return route.abort("connectionrefused");
      const completing =
        state.version === state.target &&
        state.progress.stage === "idle" &&
        state.completionBindAttempts > 0;
      if (url.pathname === "/health") {
        state.lastHealthReadAt = Date.now();
        if (completing && state.completionHealthFailures > 0) {
          state.completionHealthFailures--;
          status = 500;
          data = {
            error: {
              code: "HEALTH_TEMPORARILY_UNAVAILABLE",
              message: "测试更新完成健康状态暂时没读到",
            },
          };
        } else if (completing && state.completionHealthEmptyVersions > 0) {
          state.completionHealthEmptyVersions--;
          data = {};
        } else data = { version: state.version };
      } else if (url.pathname === "/api/v1/runtime/status") {
        const emptyVersion =
          completing && state.completionRuntimeEmptyVersions > 0;
        if (emptyVersion) state.completionRuntimeEmptyVersions--;
        data = {
          version: emptyVersion ? "" : state.version,
          node_installed: true,
          camoufox_installed: true,
          system_runtime: { ready: true, required: false },
        };
      } else if (
        url.pathname === "/api/v1/session/bind" &&
        state.version === state.target &&
        state.progress.stage === "idle"
      ) {
        state.completionBindAttempts++;
        if (state.completionBindFailures > 0) {
          state.completionBindFailures--;
          status = 409;
          data = {
            error: {
              code: "AGENT_BIND_FAILED",
              message: "测试更新完成绑定暂时失败",
            },
          };
        } else if (
          state.completionBindGate &&
          (!state.completionBindGateAttempt ||
            state.completionBindAttempts === state.completionBindGateAttempt)
        ) {
          await state.completionBindGate.promise;
        }
      } else if (url.pathname === "/api/v1/app-update/status") {
        state.statusCalls++;
        state.pendingStatus++;
        state.maximumPendingStatus = Math.max(
          state.maximumPendingStatus,
          state.pendingStatus,
        );
        const gate = state.statusGate;
        if (gate) await gate.promise;
        data = {
          current_version: state.version,
          target_version: state.target,
          ...state.progress,
        };
        state.pendingStatus--;
        if (
          state.disconnectAfterIdleSnapshot &&
          state.version === state.target &&
          state.progress.stage === "idle"
        ) {
          state.disconnectAfterIdleSnapshot = false;
          state.offline = true;
        }
      } else if (url.pathname === "/api/v1/app-update/start") {
        const gate = state.startGate;
        if (gate) await gate.promise;
        if (state.startError) {
          status = 409;
          data = {
            error: {
              code: "APP_UPDATE_FAILED",
              message: "测试更新启动失败，请再试一次",
            },
          };
        } else {
          status = 202;
          state.progress = {
            stage: "queued",
            running: true,
            percent: 1,
            message: "测试准备下载",
          };
          data = {
            current_version: state.version,
            target_version: state.target,
            ...state.progress,
          };
        }
      }
      if (status < 400) data = { success: true, data };
    } else {
      if (url.pathname === "/api/auth/me")
        data = {
          user: {
            id: "update-test",
            email: "update@example.invalid",
            role: "super_admin",
          },
        };
      else if (url.pathname === "/api/subscription/status")
        data = {
          subscription: {
            active: true,
            member_type: "max",
            allow_ai: true,
            remaining_days: 30,
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
                url: updateURL,
                sha256: updateSHA256,
                note: releaseNote,
              },
            ],
          },
        };
      else if (url.pathname === "/api/system/app-config") data = { config: {} };
      else if (url.pathname === "/api/tenants/invitations/pending")
        data = { invitations: [] };
      else if (url.pathname === "/api/config/notification-profile")
        data = { profile: { completed: true } };
      else if (url.pathname === "/api/user-flow")
        data = { flow: { state: "completed", stage: "completed", steps: {} } };
      else
        data = {
          ok: true,
          positions: [],
          accounts: [],
          platforms: [],
          prompts: {},
        };
    }
    return route.fulfill({
      status,
      headers,
      contentType: "application/json",
      body: JSON.stringify(data),
    });
  });
  return state;
}

/** updateDialog 等待真实更新弹框完成入场，确保键盘和遮罩操作针对稳定界面。 */
async function updateDialog(page: Page) {
  const dialog = page.getByRole("dialog", {
    name: "更新本地程序",
    exact: true,
  });
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveCSS("animation-name", "none");
  await expect(dialog).toHaveCSS("transform", "none");
  return dialog;
}

/** expectLocked 验证必要升级和更新进行中不能通过关闭、忽略、Esc 或遮罩绕过。 */
async function expectLocked(page: Page, dialog: Locator) {
  await page.keyboard.press("Escape");
  await page.mouse.click(3, 3);
  await expect(dialog).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "关闭", exact: true }),
  ).toHaveCount(0);
  await expect(
    dialog.getByRole("button", { name: "暂时忽略", exact: true }),
  ).toHaveCount(0);
}

/** startCalls 只统计主动更新 POST，不把自动绑定和流程心跳误判成用户提交。 */
function startCalls(state: UpdateMock) {
  return state.calls.filter(
    (call) =>
      call.path === "/api/v1/app-update/start" && call.method === "POST",
  );
}

test.afterEach(async ({ page }) => {
  const state = mocks.get(page);
  state?.startGate?.release();
  state?.statusGate?.release();
  state?.completionBindGate?.release();
  expect(state?.errors || []).toEqual([]);
});

test("必要升级保持锁定", async ({ page }) => {
  await mockUpdate(page);
  await page.goto("/admin/positions");
  await expectLocked(page, await updateDialog(page));
});

test("小补丁忽略后刷新记忆，新补丁重新提醒", async ({ page }) => {
  const state = await mockUpdate(page, { target: "6.2.10.2" });
  await page.goto("/admin/positions");
  const dialog = await updateDialog(page);
  await dialog.getByRole("button", { name: "暂时忽略", exact: true }).click();
  await expect(dialog).toBeHidden();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "岗位管理", exact: true }),
  ).toBeVisible();
  await expect(dialog).toBeHidden();
  state.target = "6.2.10.3";
  await page.reload();
  await expect(
    (await updateDialog(page)).getByRole("button", {
      name: "暂时忽略",
      exact: true,
    }),
  ).toBeVisible();
  expect(startCalls(state)).toHaveLength(0);
});

test("更新请求等待时锁定且不重复提交，四个参数与本地接口一致", async ({
  page,
}) => {
  const gate = deferred();
  const state = await mockUpdate(page, { target: "6.2.10.2", startGate: gate });
  await page.goto("/admin/positions");
  const dialog = await updateDialog(page);
  await dialog.getByRole("button", { name: "立即更新", exact: true }).click();
  await expect(dialog.getByRole("button", { name: /正在更新/ })).toBeDisabled();
  await page.keyboard.press("Enter");
  await expectLocked(page, dialog);
  expect(startCalls(state)).toHaveLength(1);
  expect(startCalls(state)[0].body).toEqual({
    url: updateURL,
    target_version: state.target,
    release_note: releaseNote,
    sha256: updateSHA256,
  });
  state.startGate = undefined;
  gate.release();
  await expect(dialog.getByText("测试准备下载", { exact: true })).toBeVisible();
});

test("queued/download/failed 轮询正确，409 后重试，快速重启同步顶栏版本", async ({
  page,
}) => {
  const state = await mockUpdate(page, { target: "6.2.10.2" });
  await page.goto("/admin/positions");
  const dialog = await updateDialog(page);
  await dialog.getByRole("button", { name: "立即更新", exact: true }).click();
  await expect(dialog.getByText("测试准备下载", { exact: true })).toBeVisible();
  state.progress = {
    stage: "download",
    running: true,
    percent: 20,
    received: 1024,
    total: 4096,
    message: "测试正在下载",
  };
  await expect(dialog.getByText("测试正在下载", { exact: true })).toBeVisible();
  await expect(
    dialog.getByText("1.0 KB / 4.0 KB", { exact: true }),
  ).toBeVisible();
  await expectLocked(page, dialog);
  state.progress = {
    stage: "failed",
    running: false,
    percent: 20,
    message: "模拟下载中途失败",
  };
  await expect(
    dialog.getByRole("button", { name: "立即更新", exact: true }),
  ).toBeEnabled();
  await expect(
    dialog.getByText("模拟下载中途失败", { exact: true }).first(),
  ).toBeVisible();
  state.startError = true;
  await dialog.getByRole("button", { name: "立即更新", exact: true }).click();
  await expect(
    dialog.getByText("测试更新启动失败，请再试一次", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "立即更新", exact: true }),
  ).toBeEnabled();
  state.startError = false;
  await dialog.getByRole("button", { name: "立即更新", exact: true }).click();
  await expect(dialog.getByText("测试准备下载", { exact: true })).toBeVisible();
  expect(startCalls(state)).toHaveLength(3);
  state.version = state.target;
  state.progress = { stage: "idle", running: false };
  await expect(dialog).toBeHidden();
  await expect(
    page.getByRole("button", { name: /6\.2\.10\.2 · 端口 43129/ }),
  ).toBeVisible();
});

test("接管 install 后断线仍继续重连，新版恢复后解除更新锁定", async ({
  page,
}) => {
  const state = await mockUpdate(page, {
    progress: {
      stage: "install",
      running: true,
      percent: 100,
      message: "测试安装器启动",
    },
  });
  await page.goto("/admin/positions");
  const dialog = await updateDialog(page);
  await expectLocked(page, dialog);
  await expect(dialog.getByRole("button", { name: /正在更新/ })).toBeDisabled();
  expect(startCalls(state)).toHaveLength(0);
  state.offline = true;
  await expect(
    page.getByRole("button", { name: "本地程序未连接", exact: true }),
  ).toBeVisible({ timeout: 12000 });
  await expect(dialog).toBeVisible();
  state.offline = false;
  state.version = state.target;
  state.progress = { stage: "idle", running: false };
  await expect(dialog).toBeHidden({ timeout: 15000 });
  await expect(
    page.getByRole("button", { name: /6\.2\.11\.0 · 端口 43129/ }),
  ).toBeVisible();
  expect(startCalls(state)).toHaveLength(0);
});

test("新版 idle 的首次完成绑定失败会继续重试，绑定完成前保持更新锁", async ({
  page,
}) => {
  const state = await mockUpdate(page, {
    progress: { stage: "download", running: true, percent: 90 },
  });
  await page.goto("/admin/positions");
  const dialog = await updateDialog(page);
  const gate = deferred();
  state.completionBindFailures = 1;
  state.completionBindGate = gate;
  state.version = state.target;
  state.progress = { stage: "idle", running: false };
  await expect.poll(() => state.completionBindAttempts).toBe(2);
  await expectLocked(page, dialog);
  await expect(dialog.getByRole("button", { name: /正在更新/ })).toBeDisabled();
  expect(startCalls(state)).toHaveLength(0);
  state.completionBindGate = undefined;
  gate.release();
  await expect(dialog).toBeHidden();
  await expect(
    page.getByRole("button", { name: /6\.2\.11\.0 · 端口 43129/ }),
  ).toBeVisible();
  expect(state.completionBindAttempts).toBe(2);
});

test("完成时 health 暂时失败使用 runtime 新版，两个版本来源为空时继续重试", async ({
  page,
}) => {
  const state = await mockUpdate(page, {
    progress: { stage: "download", running: true, percent: 90 },
  });
  await page.goto("/admin/positions");
  const dialog = await updateDialog(page);
  state.completionHealthFailures = 1;
  state.version = state.target;
  state.progress = { stage: "idle", running: false };
  await expect(dialog).toBeHidden();
  await expect(
    page.getByRole("button", { name: /6\.2\.11\.0 · 端口 43129/ }),
  ).toBeVisible();
  expect(state.completionHealthFailures).toBe(0);
  expect(state.completionBindAttempts).toBe(1);

  state.target = "6.2.12.0";
  state.completionBindAttempts = 0;
  state.progress = { stage: "download", running: true, percent: 90 };
  await page.reload();
  await updateDialog(page);
  const gate = deferred();
  state.completionBindGate = gate;
  state.completionBindGateAttempt = 2;
  state.completionHealthEmptyVersions = 1;
  state.completionRuntimeEmptyVersions = 1;
  state.version = state.target;
  state.progress = { stage: "idle", running: false };
  await expect.poll(() => state.completionBindAttempts).toBe(2);
  await expectLocked(page, dialog);
  await expect(dialog.getByRole("button", { name: /正在更新/ })).toBeDisabled();
  expect(state.completionHealthEmptyVersions).toBe(0);
  expect(state.completionRuntimeEmptyVersions).toBe(0);
  state.completionBindGate = undefined;
  gate.release();
  await expect(dialog).toBeHidden();
  await expect(
    page.getByRole("button", { name: /6\.2\.12\.0 · 端口 43129/ }),
  ).toBeVisible();
  expect(state.completionBindAttempts).toBe(2);
  expect(startCalls(state)).toHaveLength(0);
});

test("新版 idle 快照返回后同步期间再次断线，仍锁定并自动重连", async ({
  page,
}) => {
  const state = await mockUpdate(page, {
    progress: { stage: "download", running: true, percent: 90 },
  });
  await page.goto("/admin/positions");
  const dialog = await updateDialog(page);
  const gate = deferred();
  state.statusGate = gate;
  await expect.poll(() => state.pendingStatus).toBe(1);
  // 等真实端口探测缓存过期，让完成同步确实遇到断线，而非复用缓存的旧连接。
  await expect
    .poll(() => Date.now() - state.lastHealthReadAt, {
      timeout: 4000,
      intervals: [500],
    })
    .toBeGreaterThan(2100);
  state.version = state.target;
  state.progress = { stage: "idle", running: false };
  state.disconnectAfterIdleSnapshot = true;
  state.statusGate = undefined;
  gate.release();
  await expect(
    page.getByRole("button", { name: "本地程序未连接", exact: true }),
  ).toBeVisible();
  await expectLocked(page, dialog);
  await expect(dialog.getByRole("button", { name: /正在更新/ })).toBeDisabled();
  expect(state.disconnectAfterIdleSnapshot).toBe(false);
  state.offline = false;
  await expect(dialog).toBeHidden({ timeout: 15000 });
  await expect(
    page.getByRole("button", { name: /6\.2\.11\.0 · 端口 43129/ }),
  ).toBeVisible();
  expect(startCalls(state)).toHaveLength(0);
});

test("慢状态响应只能保留一轮请求，不按固定间隔堆积", async ({ page }) => {
  const state = await mockUpdate(page);
  await page.goto("/admin/positions");
  const dialog = await updateDialog(page);
  const gate = deferred();
  state.statusGate = gate;
  await dialog.getByRole("button", { name: "立即更新", exact: true }).click();
  await expect(dialog.getByText("测试准备下载", { exact: true })).toBeVisible();
  await expect.poll(() => state.pendingStatus).toBe(1);
  const count = state.statusCalls;
  let observations = 0;
  await expect
    .poll(
      () => {
        expect(state.statusCalls).toBe(count);
        expect(state.maximumPendingStatus).toBe(1);
        return ++observations;
      },
      { timeout: 4500, intervals: [1000] },
    )
    .toBe(4);
  state.statusGate = undefined;
  state.version = state.target;
  state.progress = { stage: "idle", running: false };
  gate.release();
  await expect(dialog).toBeHidden();
});

test("状态响应未完成时 SPA 退出后台，不恢复旧弹框或继续轮询", async ({
  page,
}) => {
  const gate = deferred();
  const state = await mockUpdate(page, { statusGate: gate });
  await page.goto("/admin/positions");
  await expect(
    page.getByRole("heading", { name: "岗位管理", exact: true }),
  ).toBeVisible();
  await expect.poll(() => state.pendingStatus).toBe(1);
  await page
    .locator(".goodhr-admin-sidebar")
    .getByRole("button", { name: "退出登录", exact: true })
    .click();
  await expect(page).toHaveURL(/\/login/);
  state.statusGate = undefined;
  state.progress = { stage: "download", running: true, percent: 50 };
  const count = state.statusCalls;
  gate.release();
  await expect.poll(() => state.pendingStatus).toBe(0);
  let observations = 0;
  await expect
    .poll(
      () => {
        expect(state.statusCalls).toBe(count);
        return ++observations;
      },
      { timeout: 4500, intervals: [1000] },
    )
    .toBe(4);
  await expect(
    page.getByRole("dialog", { name: "更新本地程序", exact: true }),
  ).toBeHidden();
});
