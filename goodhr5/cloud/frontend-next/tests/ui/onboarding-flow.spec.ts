/** 文件作用：在真实本地前后端与隔离协议模拟 Agent 上逐步验证新手遮罩，招聘平台、安装包和招呼均不实际操作。 */
import { test as base, expect, type BrowserContext, type Page, type APIRequestContext } from "@playwright/test";
import { CLOUD_URL, PREVIEW_URL, agentControl, openAdmin, protectNetwork, readAgentState, readUserFlow, registerUser, userStorageState, type TestUser } from "../fixtures/onboarding-helpers";

type Scenario = { context: BrowserContext; page: Page; user: TestUser; forbidden: string[]; errors: string[] };
const test = base.extend<{ scenario: Scenario }>({
  scenario: async ({ browser, request }, use, testInfo) => {
    await agentControl(request, { reset: true, autoResult: false });
    const user = await registerUser(request);
    const context = await browser.newContext({ viewport: { width: 1440, height: 1080 }, storageState: userStorageState(user) });
    context.setDefaultTimeout(15_000);
    const forbidden: string[] = [], errors: string[] = [];
    await protectNetwork(context, forbidden);
    const page = await context.newPage();
    page.on("pageerror", (error) => errors.push(error.message));
    await use({ context, page, user, forbidden, errors });
    if (testInfo.status !== testInfo.expectedStatus) await page.screenshot({ path: testInfo.outputPath("onboarding-failure.png"), fullPage: true });
    await context.close();
    expect(forbidden, "任何真实 43129 Agent 请求都必须为零").toEqual([]);
    expect(errors, "教程操作不应出现浏览器脚本错误").toEqual([]);
  },
});
test.setTimeout(120_000);

/** expectStep 等待控制器和可见遮罩同时进入指定真实操作节点。 */
async function expectStep(page: Page, step: string) {
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", step, { timeout: step === "first-result" ? 25_000 : 10_000 });
  await expect(page.getByTestId("onboarding-tour")).toHaveAttribute("data-step", step);
  await expect(page.getByRole("button", { name: "下一步", exact: true })).toHaveCount(0);
}

/** expectMaskAround 使用标准界面读取确认透明孔位围住当前目标，避免标题已更新但遮罩还停在上一项。 */
async function expectMaskAround(page: Page, selector: string) {
  await expect.poll(async () => {
    const box = await page.locator(selector).boundingBox();
    const hole = page.locator('.ant-tour-mask defs rect[fill="black"]').last();
    if (!box || !await hole.count()) return Number.POSITIVE_INFINITY;
    const [x, y, width, height] = await Promise.all([hole.getAttribute("x"), hole.getAttribute("y"), hole.getAttribute("width"), hole.getAttribute("height")]);
    return Math.max(Math.abs(Number(x) - box.x + 7), Math.abs(Number(y) - box.y + 7), Math.abs(Number(width) - box.width - 14), Math.abs(Number(height) - box.height - 14));
  }, { message: "当前遮罩透明区域必须准确圈住当前按钮或输入框", timeout: 10_000 }).toBeLessThan(2);
}

/** acceptTutorial 从欢迎弹框进入教程，已有组件应自动跳过准备阶段。 */
async function acceptTutorial(page: Page) {
  await openAdmin(page);
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
  await expectStep(page, "platform-select");
}

/** openFirstPositionForm 真实点击平台、岗位菜单与新建入口，不调用内部教程事件。 */
async function openFirstPositionForm(page: Page) {
  await acceptTutorial(page);
  await page.locator('[data-onboarding="onboarding-platform-choice"]').getByRole("radio", { name: /BOSS 直聘/ }).click();
  await expectStep(page, "platform-open");
  await page.locator('[data-onboarding="onboarding-platform-open"]').click();
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", /position-menu|position-create/, { timeout: 30_000 });
  if (await page.getByTestId("onboarding-experience").getAttribute("data-step") === "position-menu") await page.getByRole("menuitem", { name: "岗位管理", exact: true }).click();
  await expectStep(page, "position-create");
  await page.locator('[data-onboarding="position-create"]').click();
  await expectStep(page, "position-name");
  await expectMaskAround(page, '[data-onboarding="position-name"]');
}

/** fillFirstPosition 逐字段执行输入和失焦，再通过实际后端确认保存内容。 */
async function fillFirstPosition(page: Page, request: APIRequestContext, user: TestUser, filterMode: "ai" | "keyword", detailMode: "ai" | "ocr") {
  await openFirstPositionForm(page);
  const nameInput = page.locator('[data-onboarding="position-name"]');
  await nameInput.focus();
  await page.keyboard.press("Tab");
  await expectStep(page, "position-name");
  const name = `教程测试${filterMode}-${detailMode}`;
  await nameInput.fill(name);
  await page.keyboard.press("Tab");
  await expectStep(page, "position-filter-mode");
  await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
  await expectStep(page, "position-name");
  await expect(nameInput).toHaveValue(name);
  await nameInput.focus();
  await page.keyboard.press("Tab");
  await expectStep(page, "position-filter-mode");
  await page.locator('[data-onboarding="position-filter-mode"]').getByRole("radio", { name: filterMode === "ai" ? /^AI 筛选/ : /^关键词筛选/ }).click();
  await expectStep(page, "position-detail-mode");
  await page.locator('[data-onboarding="position-detail-mode"]').getByRole("radio", { name: detailMode === "ai" ? /^AI 识别/ : /^本地识别/ }).click();
  if (filterMode === "ai" || detailMode === "ai") {
    await expectStep(page, "position-requirement");
    const requirement = page.locator('[data-onboarding="position-requirement"]');
    await requirement.focus();
    await page.keyboard.press("Tab");
    await expectStep(page, "position-requirement");
    await requirement.fill("3 年以上后端开发经验，熟悉 Java 和 Spring，有电商项目经验。");
    await page.keyboard.press("Tab");
  }
  if (filterMode === "keyword") {
    await expectStep(page, "position-keywords");
    await page.locator('[data-onboarding="position-keywords"]').fill("Java，Spring，后端开发");
    await page.keyboard.press("Tab");
  }
  await expectStep(page, "position-save");
  await expectMaskAround(page, '[data-onboarding="position-save"]');
  await page.screenshot({ path: `C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/${filterMode}-${detailMode}-position.png`, fullPage: true });
  await page.locator('[data-onboarding="position-save"]').click();
  await expectStep(page, "position-start");
  const response = await request.get(`${CLOUD_URL}/api/positions`, { headers: { authorization: `Bearer ${user.token}` } });
  expect(response.ok()).toBeTruthy();
  const positions = (await response.json()).positions;
  const position = positions.find((item: Record<string, unknown>) => item.name === name);
  expect(position, "岗位必须已保存到实际 Go 后端").toBeTruthy();
  expect(position.common_config.mode_default).toBe(filterMode);
  expect(position.common_config.detail_mode).toBe(detailMode);
  const savedStorage = (await page.context().storageState()).origins.find((entry) => entry.origin === new URL(PREVIEW_URL).origin)?.localStorage || [];
  expect(savedStorage.filter((entry) => entry.name.startsWith("goodhr_onboarding_position_draft_v1_"))).toHaveLength(0);
  return position;
}

/** startFirstPosition 复用真实启动确认流程，所有浏览器与运行协议进入隔离模拟程序。 */
async function startFirstPosition(page: Page, positionID: string) {
  await page.locator(`[data-onboarding="position-start"][data-position-id="${positionID}"]`).click();
  await expectStep(page, "platform-filter");
  await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
  await expectStep(page, "position-start");
  await page.locator(`[data-onboarding="position-start"][data-position-id="${positionID}"]`).click();
  await expectStep(page, "platform-filter");
  await page.locator('[data-onboarding="position-open-platform"]').click();
  await expectStep(page, "position-run");
  await page.locator('[data-onboarding="position-run-confirm"]').click();
  await expectStep(page, "first-result");
}

for (const [filterMode, detailMode] of [["ai", "ai"], ["keyword", "ocr"], ["keyword", "ai"]] as const) {
  test(`完整教程 ${filterMode}+${detailMode}：扫描不算完成，不合适结果可以跑通`, async ({ scenario, request }) => {
    const { page, user } = scenario;
    const position = await fillFirstPosition(page, request, user, filterMode, detailMode);
    await startFirstPosition(page, position.id);
    if (filterMode === "ai") {
      await agentControl(request, { available: false });
      await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "agent-menu", { timeout: 15_000 });
      await expect(page.getByRole("region", { name: "新手教程总进度" })).toContainText("上手进度 4/5");
      const disconnectedStorage = (await scenario.context.storageState()).origins.find((entry) => entry.origin === new URL(PREVIEW_URL).origin)?.localStorage || [];
      const progress = disconnectedStorage.filter((entry) => entry.name.startsWith("goodhr_onboarding_v2_")).map((entry) => JSON.parse(entry.value)).find((entry) => entry.positionId === position.id);
      expect(progress.positionStarted).toBe(true);
      expect(progress.positionId).toBe(position.id);
      await expect(page.getByRole("dialog", { name: /安装本地程序/ })).toHaveCount(0);
      await agentControl(request, { available: true });
      await expectStep(page, "first-result");
    }
    await agentControl(request, { positionID: position.id, scannedCount: 37, analysis: null });
    await expectStep(page, "first-result");
    expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
    await agentControl(request, { positionID: position.id, analysis: { kind: filterMode, phase: "analyzing", terminal: false, accepted: false, candidate_name: "测试候选人", reason: "正在分析" } });
    await expect(page.getByRole("button", { name: "开始正式使用", exact: true })).toHaveCount(0);
    expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
    await agentControl(request, { positionID: position.id, analysis: { kind: filterMode, phase: "result", terminal: true, accepted: false, candidate_name: "测试候选人", score: 40, threshold: 70, reason: "经验未达到要求，已跳过", keywords: ["Java"], matched_keywords: [] } });
    await expect(page.getByRole("button", { name: "开始正式使用", exact: true })).toBeVisible({ timeout: 20_000 });
    await expect.poll(async () => (await readUserFlow(request, user)).tutorial_completed).toBe(true);
    await expect(page.locator("canvas").first(), "正常动态偏好下应播放礼炮").toBeVisible();
    await expect(page.locator('[class*="celebrationToast"]')).toHaveCount(0);
    await expect(page.getByRole("dialog", { name: /首轮跑通/ })).toHaveCSS("animation-name", "none");
    await page.screenshot({ path: `C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/${filterMode}-${detailMode}-complete.png`, fullPage: true });
    const agent = await readAgentState(request);
    expect(agent.calls.some((item: { path: string }) => item.path.endsWith(`/${position.id}/run`))).toBe(true);
    expect(agent.calls.filter((item: { path: string }) => item.path === "/api/v1/app-update/start")).toHaveLength(0);
    expect(agent.tasks[position.id].greeted_count).toBe(0);
    await page.getByRole("button", { name: "开始正式使用", exact: true }).click();
    await page.reload();
    await expect(page.getByTestId("onboarding-welcome")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "开始正式使用", exact: true })).toHaveCount(0);
  });
}

test("跳过需要精确输入，刷新不会再邀请；跳过不写成云端已完成", async ({ scenario, request }) => {
  const { page, user } = scenario;
  await openAdmin(page);
  await page.getByRole("button", { name: "我非新用户，无需教程", exact: true }).click();
  const input = page.getByRole("textbox", { name: "输入跳过教程确认文字", exact: true });
  const confirm = page.getByRole("button", { name: "确认跳过", exact: true });
  await expect(confirm).toBeDisabled();
  await input.fill("让开，我不需要教程 ");
  await expect(confirm).toBeDisabled();
  await page.getByRole("button", { name: "还是跟着教程走", exact: true }).click();
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
  await page.getByRole("button", { name: "我非新用户，无需教程", exact: true }).click();
  await input.fill("让开，我不需要教程");
  await expect(confirm).toBeEnabled();
  await confirm.click();
  await expect(page.getByRole("button", { name: "开启引导", exact: true })).toBeVisible();
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
  await page.reload();
  await expect(page.getByTestId("onboarding-welcome")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "开启引导", exact: true })).toBeVisible();
});

test("暂停、帮助、刷新与账号隔离不会丢失或串用进度", async ({ scenario, request, browser }) => {
  const { page, context } = scenario;
  await acceptTutorial(page);
  const tour = page.locator(".ant-tour");
  await tour.getByRole("button", { name: "遇到问题", exact: true }).click();
  await expect(page.getByRole("dialog", { name: /这一步怎么做/ })).toBeVisible();
  await page.getByRole("button", { name: "我再试试", exact: true }).click();
  await expectStep(page, "platform-select");
  await tour.getByRole("button", { name: "稍后继续", exact: true }).click();
  await expect(page.getByRole("button", { name: "继续教程", exact: true })).toBeVisible();
  await page.reload();
  await page.getByRole("button", { name: "继续教程", exact: true }).click();
  await expectStep(page, "platform-select");
  const cache = (await context.storageState()).origins.find((item) => item.origin === new URL(PREVIEW_URL).origin)?.localStorage || [];
  expect(cache.some((entry) => entry.name.startsWith("goodhr_onboarding_v2_") && entry.value.includes('"status":"active"'))).toBe(true);
  const otherUser = await registerUser(request, "onboarding-isolation");
  const other = await browser.newContext({ storageState: userStorageState(otherUser, cache), viewport: { width: 1440, height: 1080 } });
  const forbidden: string[] = [];
  await protectNetwork(other, forbidden);
  const otherPage = await other.newPage();
  await openAdmin(otherPage);
  await expect(otherPage.getByTestId("onboarding-welcome")).toBeVisible();
  await other.close();
  expect(forbidden).toEqual([]);
});

test("半填岗位刷新恢复草稿，暂停不会弹表单，字段上一步能回到新建入口", async ({ scenario }) => {
  const { page, context } = scenario;
  await openFirstPositionForm(page);
  await page.locator('[data-onboarding="position-name"]').fill("还没填完的测试岗位");
  await expect.poll(async () => (await context.storageState()).origins.some((origin) => origin.localStorage.some((entry) => entry.name.startsWith("goodhr_onboarding_position_draft_v1_") && entry.value.includes("还没填完的测试岗位")))).toBe(true);
  await page.reload();
  await expectStep(page, "position-name");
  await expect(page.locator('[data-onboarding="position-name"]')).toHaveValue("还没填完的测试岗位");
  await page.locator(".ant-tour").getByRole("button", { name: "稍后继续", exact: true }).click();
  await page.reload();
  await expect(page.getByRole("dialog", { name: "新建岗位模板", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "继续教程", exact: true }).click();
  await expect(page.locator('[data-onboarding="position-name"]')).toHaveValue("还没填完的测试岗位");
  if (await page.getByTestId("onboarding-experience").getAttribute("data-step") !== "position-name") await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
  await expectStep(page, "position-name");
  await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
  await expectStep(page, "position-create");
  await expect(page.getByRole("dialog", { name: "新建岗位模板", exact: true })).toHaveCount(0);
  await expect(page.locator('[data-onboarding="position-create"]')).toBeVisible();
});

test("本地连接和组件安装只占一个必要弹框，公告更新不盖住教程", async ({ scenario, request }) => {
  const { page } = scenario;
  await agentControl(request, { available: false, runtimeReady: false });
  await openAdmin(page);
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
  await expectStep(page, "agent-menu");
  await page.getByRole("menuitem", { name: "组件信息", exact: true }).click();
  await expectStep(page, "agent-download");
  await page.locator('[data-onboarding="onboarding-agent-download"]').click();
  await expectStep(page, "agent-connect");
  await agentControl(request, { available: true });
  await page.locator('[data-onboarding="onboarding-agent-connect"]').click();
  const installer = page.getByRole("dialog", { name: "准备必要组件", exact: true });
  await expect(installer).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await expect(page.getByRole("dialog", { name: /系统公告|更新本地程序|安装本地程序/ })).toHaveCount(0);
  await expectMaskAround(page, '[data-onboarding="runtime-install"]');
  await installer.getByRole("button", { name: /安装必要组件$/ }).click();
  await expect(installer).not.toBeVisible({ timeout: 15_000 });
  await expectStep(page, "platform-select");
  const agent = await readAgentState(request);
  expect(agent.calls.filter((item: { path: string }) => item.path === "/api/v1/runtime/install")).toHaveLength(1);
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/after-runtime-install.png", fullPage: true });
});

test("有公告和可选更新时，欢迎弹框与教程遮罩仍然独占引导", async ({ scenario, request }) => {
  const { page } = scenario;
  await page.route("**/api/runtime/config", async (route) => {
    const response = await route.fetch();
    const payload = await response.json();
    payload.config.local_agent = [{ version: "6.2.10.9", url: "https://example.invalid/test-only-update.exe", sha256: "a".repeat(64), note: "仅教程测试的可选更新" }];
    await route.fulfill({ response, json: payload });
  });
  await openAdmin(page);
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await expect(page.getByRole("dialog", { name: /系统公告|更新本地程序/ })).toHaveCount(0);
  await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
  await expectStep(page, "platform-select");
  await expect(page.getByRole("dialog", { name: /系统公告|更新本地程序/ })).toHaveCount(0);
  const state = await readAgentState(request);
  expect(state.calls.filter((item: { path: string }) => item.path === "/api/v1/app-update/start")).toHaveLength(0);
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/optional-update-deferred.png", fullPage: true });
});

test("必要升级优先完成，组件安装与教程不会叠加弹框", async ({ scenario, request }) => {
  const { page } = scenario;
  await agentControl(request, { runtimeReady: false, targetVersion: "6.2.11.0" });
  await page.route("**/api/runtime/config", async (route) => {
    const response = await route.fetch();
    const payload = await response.json();
    payload.config.local_agent = [{ version: "6.2.11.0", url: "https://example.invalid/test-only-update.exe", sha256: "a".repeat(64), note: "仅教程测试的必要升级" }];
    await route.fulfill({ response, json: payload });
  });
  await openAdmin(page);
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
  const update = page.getByRole("dialog", { name: "更新本地程序", exact: true });
  await expect(update).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await expect(page.getByTestId("onboarding-tour")).toHaveCount(0);
  await update.getByRole("button", { name: "立即更新", exact: true }).click();
  await expect(update).not.toBeVisible({ timeout: 20_000 });
  const installer = page.getByRole("dialog", { name: "准备必要组件", exact: true });
  await expect(installer).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await expectMaskAround(page, '[data-onboarding="runtime-install"]');
  await installer.getByRole("button", { name: /安装必要组件$/ }).click();
  await expectStep(page, "platform-select");
  const state = await readAgentState(request);
  expect(state.calls.filter((item: { path: string }) => item.path === "/api/v1/app-update/start")).toHaveLength(1);
  expect(state.calls.filter((item: { path: string }) => item.path === "/api/v1/runtime/install")).toHaveLength(1);
});

test("390px手机能打开菜单并真实进入岗位管理", async ({ scenario }) => {
  const { page } = scenario;
  await page.setViewportSize({ width: 390, height: 844 });
  await acceptTutorial(page);
  await page.locator('[data-onboarding="onboarding-platform-choice"]').getByRole("radio", { name: /BOSS 直聘/ }).click();
  await expectStep(page, "platform-open");
  await page.locator('[data-onboarding="onboarding-platform-open"]').click();
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-menu", { timeout: 30_000 });
  await page.locator('[data-onboarding="mobile-menu"]').click();
  await page.getByRole("menuitem", { name: "岗位管理", exact: true }).click();
  await expectStep(page, "position-create");
  await expect(page.locator('[data-onboarding="position-create"]')).toBeVisible();
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/mobile-390-position-menu.png", fullPage: true });
});

test("减少动态效果时保留鼓励文字并禁用礼炮", async ({ scenario, request }) => {
  const { page, user } = scenario;
  await page.emulateMedia({ reducedMotion: "reduce" });
  const position = await fillFirstPosition(page, request, user, "keyword", "ocr");
  await startFirstPosition(page, position.id);
  await agentControl(request, { positionID: position.id, analysis: { kind: "keyword", phase: "result", terminal: true, accepted: false, candidate_name: "测试候选人", reason: "关键词未命中，已跳过" } });
  await expect(page.getByRole("button", { name: "开始正式使用", exact: true })).toBeVisible({ timeout: 20_000 });
  await expect(page.locator("canvas")).toHaveCount(0);
  await expect(page.getByRole("dialog", { name: /首轮跑通/ })).toContainText("第一条真实筛选结果已经出来了");
});
