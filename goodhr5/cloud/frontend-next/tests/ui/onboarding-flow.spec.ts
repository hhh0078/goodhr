/** 文件作用：在真实本地前后端与隔离协议模拟 Agent 上逐步验证新手遮罩，招聘平台、安装包和招呼均不实际操作。 */
import { test as base, expect, type BrowserContext, type Page, type APIRequestContext, type Locator } from "@playwright/test";
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
async function expectMaskAround(page: Page, selector: string | Locator) {
  const target = typeof selector === "string" ? page.locator(selector) : selector;
  let measured: unknown = null;
  try {
    await expect.poll(async () => {
      const box = await target.boundingBox();
      const hole = page.locator('.ant-tour-mask defs rect[fill="black"]').last();
      if (!box || !await hole.count()) return Number.POSITIVE_INFINITY;
      const [x, y, width, height] = await Promise.all([hole.getAttribute("x"), hole.getAttribute("y"), hole.getAttribute("width"), hole.getAttribute("height")]);
      measured = { step: await page.getByTestId("onboarding-experience").getAttribute("data-step"), target: box, hole: { x: Number(x), y: Number(y), width: Number(width), height: Number(height) } };
      return Math.max(Math.abs(Number(x) - box.x + 7), Math.abs(Number(y) - box.y + 7), Math.abs(Number(width) - box.width - 14), Math.abs(Number(height) - box.height - 14));
    }, { message: "当前遮罩透明区域必须准确圈住当前按钮或输入框", timeout: 10_000 }).toBeLessThan(2);
  } catch (error) {
    await test.info().attach("mask-target-geometry.json", { body: JSON.stringify(measured), contentType: "application/json" });
    console.log(`遮罩目标几何：${JSON.stringify(measured)}`);
    throw error;
  }
}

/** expectModalFieldVisible 确认真实字段没有被滚动弹框裁切，且透明孔随内部滚动保持同步。 */
async function expectModalFieldVisible(page: Page, selector: string) {
  const target = page.locator(selector);
  const body = page.getByRole("dialog", { name: "新建岗位模板", exact: true }).locator(".ant-modal-body");
  await expect.poll(async () => {
    const [fieldBox, bodyBox] = await Promise.all([target.boundingBox(), body.boundingBox()]);
    if (!fieldBox || !bodyBox) return Number.NEGATIVE_INFINITY;
    return Math.min(fieldBox.x - bodyBox.x, fieldBox.y - bodyBox.y, bodyBox.x + bodyBox.width - fieldBox.x - fieldBox.width, bodyBox.y + bodyBox.height - fieldBox.y - fieldBox.height);
  }, { message: "当前字段必须完整处于 Modal body 可视范围，不能只在视口中却被内部裁切", timeout: 10_000 }).toBeGreaterThanOrEqual(-1);
  try {
    await expectMaskAround(page, target);
  } catch (error) {
    const geometry = { target: selector, field: await target.boundingBox(), body: await body.boundingBox(), holes: await page.locator('.ant-tour-mask defs rect[fill="black"]').count(), tour: await page.getByTestId("onboarding-tour").count(), step: await page.getByTestId("onboarding-experience").getAttribute("data-step") };
    await test.info().attach("modal-field-geometry.json", { body: JSON.stringify(geometry), contentType: "application/json" });
    console.log(`遮罩实测几何：${JSON.stringify(geometry)}`);
    throw error;
  }
}

/** acceptTutorial 从欢迎弹框进入教程，已有组件应自动跳过准备阶段。 */
async function acceptTutorial(page: Page) {
  await openAdmin(page);
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
  await expectStep(page, "platform-select");
}

/** headerBrowserButton 只选择顶部当前屏幕可见的真实浏览器按钮，兼容桌面文字按钮和手机图标。 */
function headerBrowserButton(page: Page) {
  return page.locator('header.goodhr-admin-toolbar [data-onboarding="browser-open"]:visible');
}

/** openFirstPositionForm 真实点击平台、岗位菜单与新建入口，不调用内部教程事件。 */
async function openFirstPositionForm(page: Page) {
  await acceptTutorial(page);
  await page.locator('[data-onboarding="onboarding-platform-choice"]').getByRole("radio", { name: /BOSS 直聘/ }).click();
  await expectStep(page, "platform-open");
  await headerBrowserButton(page).click();
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
  test(`完整教程 ${filterMode}+${detailMode}：扫描不算完成，明确判断可以跑通`, async ({ scenario, request }) => {
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
    if (filterMode === "keyword" && detailMode === "ai") {
      const before = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === `/api/v1/local/positions/${position.id}/status`).length;
      await agentControl(request, { positionID: position.id, analysis: { kind: "keyword", phase: "result", terminal: true, accepted: true, candidate_name: "测试候选人", reason: "基础关键词命中，仍需等待详情 AI", keywords: ["Java"], matched_keywords: ["Java"] } });
      await expect.poll(async () => (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === `/api/v1/local/positions/${position.id}/status`).length).toBeGreaterThan(before + 1);
      await expectStep(page, "first-result");
      expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
    }
    const resultKind = filterMode === "keyword" && detailMode === "ai" ? "ai" : filterMode;
    const accepted = filterMode === "keyword" && detailMode === "ocr";
    await agentControl(request, { positionID: position.id, analysis: { kind: resultKind, phase: "result", terminal: true, accepted, candidate_name: "测试候选人", score: accepted ? 80 : 40, threshold: 70, reason: accepted ? "关键词命中，已经得到合适的筛选结果" : "经验未达到要求，已跳过", keywords: ["Java"], matched_keywords: accepted ? ["Java"] : [] } });
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

test("要求和关键词半填刷新保留六项草稿，换号不会带入岗位", async ({ scenario, request, browser }) => {
  const { page, context, user } = scenario;
  await openFirstPositionForm(page);
  await page.locator('[data-onboarding="position-name"]').fill("六项草稿测试岗位");
  await page.keyboard.press("Tab");
  await expectStep(page, "position-filter-mode");
  await page.locator('[data-onboarding="position-filter-mode"]').getByRole("radio", { name: /^关键词筛选/ }).click();
  await expectStep(page, "position-detail-mode");
  await page.locator('[data-onboarding="position-detail-mode"]').getByRole("radio", { name: /^AI 识别/ }).click();
  await expectStep(page, "position-requirement");
  await page.locator('[data-onboarding="position-requirement"]').fill("半填岗位要求：后端开发，熟悉 Java");
  await expect.poll(async () => (await context.storageState()).origins.some((origin) => origin.localStorage.some((entry) => entry.name.startsWith("goodhr_onboarding_position_draft_v1_") && entry.value.includes("半填岗位要求")))).toBe(true);
  await page.reload();
  await expectStep(page, "position-requirement");
  await expect(page.locator('[data-onboarding="position-name"]')).toHaveValue("六项草稿测试岗位");
  await expect(page.locator('[data-onboarding="position-filter-mode"]').getByRole("radio", { name: /^关键词筛选/ })).toBeChecked();
  await expect(page.locator('[data-onboarding="position-detail-mode"]').getByRole("radio", { name: /^AI 识别/ })).toBeChecked();
  await expect(page.locator('[data-onboarding="position-requirement"]')).toHaveValue("半填岗位要求：后端开发，熟悉 Java");
  await page.locator('[data-onboarding="position-requirement"]').focus();
  await page.keyboard.press("Tab");
  await expectStep(page, "position-keywords");
  await page.locator('[data-onboarding="position-keywords"]').fill("Java，Spring");
  await expect.poll(async () => (await context.storageState()).origins.some((origin) => origin.localStorage.some((entry) => entry.name.startsWith("goodhr_onboarding_position_draft_v1_") && entry.value.includes("Java，Spring")))).toBe(true);
  await page.reload();
  await expectStep(page, "position-keywords");
  await expect(page.locator('[data-onboarding="position-keywords"]')).toHaveValue("Java，Spring");
  const cache = (await context.storageState()).origins.find((entry) => entry.origin === new URL(PREVIEW_URL).origin)?.localStorage || [];
  const draft = cache.find((entry) => entry.name === `goodhr_onboarding_position_draft_v1_${encodeURIComponent(user.id)}`);
  expect(draft, "当前账号应保存独立草稿").toBeTruthy();
  expect(JSON.parse(draft!.value).draft).toEqual({ name: "六项草稿测试岗位", platform_id: "boss", mode_default: "keyword", detail_mode: "ai", position_requirement: "半填岗位要求：后端开发，熟悉 Java", keywords: "Java，Spring" });
  const otherUser = await registerUser(request, "onboarding-draft-isolation");
  const other = await browser.newContext({ storageState: userStorageState(otherUser, cache), viewport: { width: 1440, height: 1080 } });
  const forbidden: string[] = [];
  await protectNetwork(other, forbidden);
  try {
    const otherPage = await other.newPage();
    await openFirstPositionForm(otherPage);
    await expect(otherPage.locator('[data-onboarding="position-name"]')).toHaveValue("");
    await expect(otherPage.locator('[data-onboarding="position-requirement"]')).toHaveValue("");
  } finally { await other.close(); }
  expect(forbidden).toEqual([]);
});

test("打开平台请求未返回时上一步不提前高亮背后的开始按钮", async ({ scenario, request }) => {
  const { page, user } = scenario;
  const position = await fillFirstPosition(page, request, user, "keyword", "ocr");
  await page.locator(`[data-onboarding="position-start"][data-position-id="${position.id}"]`).click();
  await expectStep(page, "platform-filter");
  let release!: () => void;
  const pending = new Promise<void>((resolve) => { release = resolve; });
  let intercepted = false;
  await page.route("**/api/v1/page/open", async (route) => { intercepted = true; await pending; await route.continue(); });
  try {
    await page.locator('[data-onboarding="position-open-platform"]').click();
    await expect.poll(() => intercepted).toBe(true);
    await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
    const baseline = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/runtime/status").length;
    await expect.poll(async () => (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/runtime/status").length).toBeGreaterThan(baseline);
    await expect(page.getByTestId("onboarding-experience")).not.toHaveAttribute("data-step", "position-start");
    await expect(page.getByRole("dialog", { name: "开始招聘岗位", exact: true })).toBeVisible();
  } finally { release(); }
  await expectStep(page, "position-start");
  await page.locator(`[data-onboarding="position-start"][data-position-id="${position.id}"]`).click();
  await expectStep(page, "platform-filter");
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

test("顶部浏览器按钮打开所选平台，等待登录和刷新恢复都不再指向教程自造入口", async ({ scenario, request }) => {
  const { page } = scenario;
  await agentControl(request, { loggedIn: false });
  const platformURL = "https://tutorial-zhaopin.example.invalid/recruiting";
  await page.route("**/api/platforms/config/**", async (route) => {
    await route.fulfill({ json: { ok: true, configs: [{ config_key: "platform.zhaopin", enabled: true, config_value: JSON.stringify({ id: "zhaopin", open: true, auth: { pages: [{ url: platformURL, match: "prefix", entry: true }] }, public: { pages: [{ url: "https://tutorial-zhaopin.example.invalid/login", match: "prefix", code: "login" }] } }) }] } });
  });
  await acceptTutorial(page);
  await page.locator('[data-onboarding="onboarding-platform-choice"]').getByRole("radio", { name: /智联招聘/ }).click();
  await expectStep(page, "platform-open");
  const browserButton = headerBrowserButton(page);
  await expect(browserButton).toHaveCount(1);
  await expect(browserButton).toBeVisible();
  await expectMaskAround(page, browserButton);
  await expect(page.locator('[data-onboarding="onboarding-platform-open"], [data-onboarding="onboarding-platform-wait"]')).toHaveCount(0);
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open")).toHaveLength(0);
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/desktop-browser-open-mask.png", fullPage: true });
  await browserButton.click();
  await expectStep(page, "platform-login");
  await expectMaskAround(page, browserButton);
  const tour = page.locator(".ant-tour");
  await expect(tour).toContainText(/刚.*打开.{0,12}浏览器/);
  await expect(tour).toContainText("狐狸");
  await expect(tour).toContainText("任务栏");
  await expect(tour).toContainText("程序坞");
  await expect(page.getByRole("dialog", { name: "浏览器已经打开啦", exact: true })).toHaveCount(0);
  await expect(page.locator('[aria-label="当前教程操作"]')).toHaveCount(0);
  const firstOpen = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open");
  expect(firstOpen).toHaveLength(1);
  expect(firstOpen[0].body).toMatchObject({ platform_id: "zhaopin", user_data_dir: "default", url: platformURL });
  expect(firstOpen[0].body.url).not.toContain("bing.com");
  await expect.poll(async () => (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/url").length, { message: "未登录时应持续检测，并停在外部浏览器登录说明", timeout: 20_000 }).toBeGreaterThanOrEqual(2);
  await expectStep(page, "platform-login");
  await expect(browserButton, "登录等待期间顶部按钮仍能重新打开招聘浏览器").toBeEnabled();
  await browserButton.click();
  await expect.poll(async () => (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open").length).toBe(2);
  await expectStep(page, "platform-login");
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/desktop-platform-login-top.png", fullPage: true });
  const beforeRefresh = await readAgentState(request);
  const checksBeforeRefresh = beforeRefresh.calls.filter((call: { path: string }) => call.path === "/api/v1/page/url").length;
  await page.reload();
  await expectStep(page, "platform-login");
  await expectMaskAround(page, headerBrowserButton(page));
  await expect.poll(async () => (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/url").length, { message: "刷新必须自动恢复登录检测，不需要再次点击打开", timeout: 20_000 }).toBeGreaterThan(checksBeforeRefresh + 1);
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open")).toHaveLength(2);
  await expectStep(page, "platform-login");
  await agentControl(request, { loggedIn: true });
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-menu", { timeout: 25_000 });
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open")).toHaveLength(2);
});

test("390px手机浏览器入口的遮罩圈住真实顶部图标，登录等待时仍可点击", async ({ scenario, request }) => {
  const { page } = scenario;
  await page.setViewportSize({ width: 390, height: 844 });
  await agentControl(request, { loggedIn: false });
  await acceptTutorial(page);
  await page.locator('[data-onboarding="onboarding-platform-choice"]').getByRole("radio", { name: /BOSS 直聘/ }).click();
  await expectStep(page, "platform-open");
  const browserButton = headerBrowserButton(page);
  await expect(browserButton).toHaveCount(1);
  await expect(browserButton).toHaveAttribute("aria-label", "打开浏览器");
  const buttonBox = await browserButton.boundingBox();
  const headerBox = await page.locator("header.goodhr-admin-toolbar").boundingBox();
  expect(buttonBox).toBeTruthy();
  expect(headerBox).toBeTruthy();
  expect(buttonBox!.x).toBeGreaterThanOrEqual(0);
  expect(buttonBox!.x + buttonBox!.width).toBeLessThanOrEqual(390);
  expect(buttonBox!.y).toBeGreaterThanOrEqual(headerBox!.y);
  expect(buttonBox!.y + buttonBox!.height).toBeLessThanOrEqual(headerBox!.y + headerBox!.height);
  await expectMaskAround(page, browserButton);
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/mobile-390-browser-open-mask.png", fullPage: true });
  await browserButton.click();
  await expectStep(page, "platform-login");
  await expectMaskAround(page, browserButton);
  await expect(browserButton).toBeEnabled();
  await expect(page.locator('[data-onboarding="onboarding-platform-open"], [data-onboarding="onboarding-platform-wait"]')).toHaveCount(0);
  await expect(page.locator('[aria-label="当前教程操作"]')).toHaveCount(0);
  const calls = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open");
  expect(calls).toHaveLength(1);
  expect(calls[0].body).toMatchObject({ platform_id: "boss", user_data_dir: "default" });
  expect(calls[0].body.url).toContain("zhipin.com");
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/mobile-390-platform-login-top.png", fullPage: true });
});

test("暂停教程后顶部按钮保留普通打开Bing和首次浏览器图标提醒", async ({ scenario, request }) => {
  const { page, user } = scenario;
  await acceptTutorial(page);
  await page.locator(".ant-tour").getByRole("button", { name: "稍后继续", exact: true }).click();
  await expect(page.getByRole("button", { name: "继续教程", exact: true })).toBeVisible();
  await expect(page.getByTestId("onboarding-tour")).toHaveCount(0);
  await headerBrowserButton(page).click();
  const notice = page.getByRole("dialog", { name: "浏览器已经打开啦", exact: true });
  await expect(notice).toBeVisible();
  await expect(notice).toContainText("任务栏或程序坞");
  const calls = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open");
  expect(calls).toHaveLength(1);
  expect(calls[0].body).toMatchObject({ user_data_dir: "default", url: "https://www.bing.com" });
  expect(calls[0].body.platform_id).toBeUndefined();
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
  await notice.getByRole("button", { name: "知道了", exact: true }).click();
  await expect(notice).not.toBeVisible();
  await expect(page.getByRole("button", { name: "继续教程", exact: true })).toBeVisible();
});

test("390px手机能打开菜单并真实进入岗位管理", async ({ scenario }) => {
  const { page } = scenario;
  await page.setViewportSize({ width: 390, height: 844 });
  await acceptTutorial(page);
  await page.locator('[data-onboarding="onboarding-platform-choice"]').getByRole("radio", { name: /BOSS 直聘/ }).click();
  await expectStep(page, "platform-open");
  await headerBrowserButton(page).click();
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-menu", { timeout: 30_000 });
  await page.locator('[data-onboarding="mobile-menu"]').click();
  await expectMaskAround(page, page.getByRole("menuitem", { name: "岗位管理", exact: true }));
  await page.getByRole("menuitem", { name: "岗位管理", exact: true }).click();
  await expectStep(page, "position-create");
  await expect(page.locator('[data-onboarding="position-create"]')).toBeVisible();
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/mobile-390-position-menu.png", fullPage: true });
});

test("1280x720弹框真实滚轮和Tab后字段不被裁切，遮罩同步且完整跑到结果", async ({ scenario, request }) => {
  const { page, user } = scenario;
  await page.setViewportSize({ width: 1280, height: 720 });
  await openFirstPositionForm(page);
  const name = page.locator('[data-onboarding="position-name"]');
  await expectModalFieldVisible(page, '[data-onboarding="position-name"]');
  const beforeWheel = await name.boundingBox();
  expect(beforeWheel).toBeTruthy();
  await page.mouse.move(beforeWheel!.x + beforeWheel!.width / 2, beforeWheel!.y + beforeWheel!.height / 2);
  await page.mouse.wheel(0, 480);
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/desktop-1280x720-name-wheel.png", fullPage: true });
  await expectModalFieldVisible(page, '[data-onboarding="position-name"]');
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/desktop-1280x720-name-wheel.png", fullPage: true });
  await name.click();
  await name.fill("低窗口滚动联调岗位");
  await page.keyboard.press("Tab");
  await expectStep(page, "position-filter-mode");
  await expectModalFieldVisible(page, '[data-onboarding="position-filter-mode"]');
  await page.locator('[data-onboarding="position-filter-mode"]').getByRole("radio", { name: /^关键词筛选/ }).click();
  await expectStep(page, "position-detail-mode");
  await expectModalFieldVisible(page, '[data-onboarding="position-detail-mode"]');
  await page.locator('[data-onboarding="position-detail-mode"]').getByRole("radio", { name: /^AI 识别/ }).click();
  await expectStep(page, "position-requirement");
  await expectModalFieldVisible(page, '[data-onboarding="position-requirement"]');
  const requirement = page.locator('[data-onboarding="position-requirement"]');
  const requirementBox = await requirement.boundingBox();
  await page.mouse.move(requirementBox!.x + requirementBox!.width / 2, requirementBox!.y + requirementBox!.height / 2);
  await page.mouse.wheel(0, -280);
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/desktop-1280x720-requirement-wheel.png", fullPage: true });
  await expectModalFieldVisible(page, '[data-onboarding="position-requirement"]');
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/desktop-1280x720-requirement-wheel.png", fullPage: true });
  await requirement.click();
  await requirement.fill("后端开发 3 年以上。\n熟悉 Java 与 Spring。\n有电商项目经验。\n熟悉关系型数据库。\n能独立完成接口设计。\n有团队协作经验。\n具备问题排查能力。");
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-requirement");
  await expect(requirement, "文本框增高导致遮罩重测时仍应保留输入焦点").toBeFocused();
  await expectModalFieldVisible(page, '[data-onboarding="position-requirement"]');
  await page.keyboard.press("Tab");
  await expectStep(page, "position-keywords");
  await expectModalFieldVisible(page, '[data-onboarding="position-keywords"]');
  await page.locator('[data-onboarding="position-keywords"]').click();
  await page.locator('[data-onboarding="position-keywords"]').fill("Java，Spring");
  await page.keyboard.press("Tab");
  await expectStep(page, "position-save");
  await expectMaskAround(page, '[data-onboarding="position-save"]');
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/desktop-1280x720-save.png", fullPage: true });
  await page.locator('[data-onboarding="position-save"]').click();
  await expectStep(page, "position-start");
  const response = await request.get(`${CLOUD_URL}/api/positions`, { headers: { authorization: `Bearer ${user.token}` } });
  expect(response.ok()).toBeTruthy();
  const position = (await response.json()).positions.find((item: { name: string }) => item.name === "低窗口滚动联调岗位");
  expect(position).toBeTruthy();
  expect(position.common_config.mode_default).toBe("keyword");
  expect(position.common_config.detail_mode).toBe("ai");
  await startFirstPosition(page, position.id);
  await agentControl(request, { positionID: position.id, analysis: { kind: "ai", phase: "result", terminal: true, accepted: false, candidate_name: "测试候选人", reason: "本机联调：经验不符合要求，已跳过" } });
  await expect(page.getByRole("button", { name: "开始正式使用", exact: true })).toBeVisible({ timeout: 20_000 });
  await expect.poll(async () => (await readUserFlow(request, user)).tutorial_completed).toBe(true);
  expect((await readAgentState(request)).tasks[position.id].greeted_count).toBe(0);
  await expect(page.getByRole("dialog", { name: /首轮跑通/ })).toHaveCSS("animation-name", "none");
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/desktop-1280x720-complete.png", fullPage: true });
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
