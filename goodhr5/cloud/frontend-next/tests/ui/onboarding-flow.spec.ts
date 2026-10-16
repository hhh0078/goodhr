/** 文件作用：在真实本地前后端与隔离协议模拟 Agent 上逐步验证新手遮罩，招聘平台、安装包和招呼均不实际操作。 */
import { test as base, expect, type Browser, type BrowserContext, type Page, type APIRequestContext, type Locator } from "@playwright/test";
import { CLOUD_URL, PREVIEW_URL, agentControl, openAdmin, protectNetwork, readAgentState, readUserFlow, registerUser, userStorageState, type TestUser } from "../fixtures/onboarding-helpers";
import { createOnboardingState, onboardingStorageKey } from "../../lib/onboarding-state";

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
  await expectPositionEntry(page);
}

/** expectPositionEntry 组件就绪后优先引导创建岗位，兼容当前就在岗位管理的恢复场景。 */
async function expectPositionEntry(page: Page) {
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", /position-menu|position-create/);
  await expect(page.getByTestId("onboarding-tour")).toHaveAttribute("data-step", /position-menu|position-create/);
  await expect(page.locator('[data-onboarding="onboarding-platform-choice"]')).toHaveCount(0);
}

/** headerBrowserButton 只选择顶部当前屏幕可见的真实浏览器按钮，兼容桌面文字按钮和手机图标。 */
function headerBrowserButton(page: Page) {
  return page.locator('header.goodhr-admin-toolbar [data-onboarding="browser-open"]:visible');
}

/** openFirstPositionForm 先进入岗位管理并创建岗位，在真实表单确认平台后才填写名称，不打开浏览器。 */
async function openFirstPositionForm(page: Page, platformName: RegExp = /BOSS\s*直聘/i, acceptFirst = true) {
  if (acceptFirst) await acceptTutorial(page);
  else await expectPositionEntry(page);
  if (await page.getByTestId("onboarding-experience").getAttribute("data-step") === "position-menu") {
    const positionsMenu = page.getByRole("menuitem", { name: "岗位管理", exact: true });
    if (!await positionsMenu.isVisible() && await page.locator('[data-onboarding="mobile-menu"]').isVisible()) await page.locator('[data-onboarding="mobile-menu"]').click();
    await positionsMenu.click();
  }
  await expectStep(page, "position-create");
  await page.locator('[data-onboarding="position-create"]').click();
  await expectStep(page, "position-platform");
  await expectMaskAround(page, '[data-onboarding="position-platform"]');
  await page.locator('[data-onboarding="position-platform"]').getByRole("radio", { name: platformName }).click();
  await expectStep(page, "position-name");
  await expectMaskAround(page, '[data-onboarding="position-name"]');
}

/** fillFirstPosition 逐字段执行输入和失焦；可停在保存前，正常保存时用实际后端核对内容。 */
async function fillFirstPosition(page: Page, request: APIRequestContext, user: TestUser, filterMode: "ai" | "keyword", detailMode: "ai" | "ocr" | "dom", savePosition = true, options: { platformName?: RegExp; loginAfterSave?: boolean; invitationAccepted?: boolean } = {}) {
  await openFirstPositionForm(page, options.platformName, !options.invitationAccepted);
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
  await page.locator('[data-onboarding="position-detail-mode"]').getByRole("radio", { name: detailMode === "ai" ? /^AI 识别/ : detailMode === "dom" ? /^页面文字识别/ : /^本地识别/ }).click();
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
  const beforeSave = await readAgentState(request);
  expect(beforeSave.calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path)), "岗位保存前不能打开或检测招聘平台登录").toHaveLength(0);
  if (!savePosition) return { name };
  await page.locator('[data-onboarding="position-save"]').click();
  await expectStep(page, "platform-open");
  const response = await request.get(`${CLOUD_URL}/api/positions`, { headers: { authorization: `Bearer ${user.token}` } });
  expect(response.ok()).toBeTruthy();
  const positions = (await response.json()).positions;
  const position = positions.find((item: Record<string, unknown>) => item.name === name);
  expect(position, "岗位必须已保存到实际 Go 后端").toBeTruthy();
  expect(position.common_config.mode_default).toBe(filterMode);
  expect(position.common_config.detail_mode).toBe(detailMode);
  const savedStorage = (await page.context().storageState()).origins.find((entry) => entry.origin === new URL(PREVIEW_URL).origin)?.localStorage || [];
  expect(savedStorage.filter((entry) => entry.name.startsWith("goodhr_onboarding_position_draft_v1_"))).toHaveLength(0);
  if (options.loginAfterSave !== false) await loginSavedPosition(page);
  return position;
}

/** loginSavedPosition 岗位保存后才从真实顶部入口打开其平台，连续登录检测通过后进入启动步骤。 */
async function loginSavedPosition(page: Page) {
  await expectStep(page, "platform-open");
  await headerBrowserButton(page).click();
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-start", { timeout: 30_000 });
  await expectStep(page, "position-start");
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

/** openRunConfirmation 通过真实岗位入口打开筛选确认，便于验证启动检查失败后的恢复操作。 */
async function openRunConfirmation(page: Page, positionID: string) {
  await page.locator(`[data-onboarding="position-start"][data-position-id="${positionID}"]`).click();
  await expectStep(page, "platform-filter");
  await page.locator('[data-onboarding="position-open-platform"]').click();
  await expectStep(page, "position-run");
}

/** expectNoCompletion 同时核对界面和真实后端，阻止失败、扫描和缓存被误当作完成。 */
async function expectNoCompletion(page: Page, request: APIRequestContext, user: TestUser) {
  await expect(page.getByRole("button", { name: "开始正式使用", exact: true })).toHaveCount(0);
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
}

/** platformConfig 为关闭平台回归构造公开配置，浏览器操作仍由隔离 Agent 协议接收。 */
function platformConfig(id: string, open: boolean) {
  const entry = id === "boss" ? "https://www.zhipin.com/web/chat/recommend" : "https://tutorial-zhaopin.example.invalid/recruiting";
  return { config_key: `platform.${id}`, enabled: true, config_value: JSON.stringify({ id, open, auth: { pages: [{ url: entry, match: "prefix", entry: true }] }, public: { pages: [{ url: "https://tutorial-login.example.invalid/login", match: "prefix", code: "login" }] } }) };
}

/** cachedTutorialContext 用原生缓存快照复现旧v2恢复，沿用真实用户但不注入页面脚本或内部事件。 */
async function cachedTutorialContext(browser: Browser, user: TestUser, state: ReturnType<typeof createOnboardingState>, width: number, scenario: Scenario) {
  const context = await browser.newContext({ viewport: { width, height: 960 }, storageState: userStorageState(user, [{ name: onboardingStorageKey(user.id), value: JSON.stringify(state) }]) });
  await protectNetwork(context, scenario.forbidden);
  const page = await context.newPage();
  page.on("pageerror", (error) => scenario.errors.push(error.message));
  return { context, page };
}

/** createSavedTutorialPosition 通过真实岗位接口准备恢复样本，不要求或伪造招聘平台登录。 */
async function createSavedTutorialPosition(request: APIRequestContext, user: TestUser, platformID: string) {
  const response = await request.post(`${CLOUD_URL}/api/positions`, { headers: { authorization: `Bearer ${user.token}` }, data: { name: `恢复平台权威测试-${platformID}`, platform_id: platformID, is_open: true, keywords: ["招聘"], common_config: { mode_default: "keyword", detail_mode: platformID === "boss" ? "ocr" : "dom" }, ai_config: {} } });
  expect(response.ok(), await response.text()).toBeTruthy();
  const position = (await response.json()).position;
  expect(position.platform_id).toBe(platformID);
  return position;
}

/** readTutorialProgress 通过浏览器原生存储快照读取本账号进度，不向页面注入脚本。 */
async function readTutorialProgress(page: Page, user: TestUser) {
  const origin = (await page.context().storageState()).origins.find((entry) => entry.origin === new URL(PREVIEW_URL).origin);
  return JSON.parse(origin?.localStorage.find((entry) => entry.name === onboardingStorageKey(user.id))?.value || "{}");
}

/** readTutorialPositions 查询真实本机 Go 岗位列表，核对复核和重走不会重复创建业务数据。 */
async function readTutorialPositions(request: APIRequestContext, user: TestUser) {
  const response = await request.get(`${CLOUD_URL}/api/positions`, { headers: { authorization: `Bearer ${user.token}` } });
  expect(response.ok(), await response.text()).toBeTruthy();
  const result = await response.json();
  return (Array.isArray(result.positions) ? result.positions : []) as { id: string; name: string; platform_id: string; keywords: string[]; status: string }[];
}

/** openTutorialPositionReview 从顶部开窗节点实际回退并点击本岗位编辑，不走另建岗位入口。 */
async function openTutorialPositionReview(page: Page, positionID: string) {
  await expectStep(page, "platform-open");
  await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
  await expectStep(page, "position-edit");
  await expect(page.getByRole("dialog", { name: /这一步怎么做/ })).toHaveCount(0);
  const edit = page.locator(`[data-onboarding="position-edit"][data-position-id="${positionID}"]`);
  await expectMaskAround(page, edit);
  await edit.click();
  await expect(page.getByRole("dialog", { name: "编辑岗位模板", exact: true })).toBeVisible();
}

for (const width of [1440, 390]) {
  test(`${width}px旧v2登录缓存无岗位时回创建，不先打开或检测平台`, async ({ scenario, browser, request }) => {
    const restored = await cachedTutorialContext(browser, scenario.user, { ...createOnboardingState(), status: "active", step: "platform-login", environmentReady: true, platformLoggedIn: true }, width, scenario);
    try {
      await openAdmin(restored.page);
      await expectPositionEntry(restored.page);
      expect((await readAgentState(request)).calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path))).toHaveLength(0);
      const cache = (await restored.context.storageState()).origins.find((origin) => origin.origin === new URL(PREVIEW_URL).origin)?.localStorage || [];
      await expect.poll(async () => {
        const values = (await restored.context.storageState()).origins.find((origin) => origin.origin === new URL(PREVIEW_URL).origin)?.localStorage || [];
        return JSON.parse(values.find((entry) => entry.name === onboardingStorageKey(scenario.user.id))?.value || "{}").platformLoggedIn;
      }).toBe(false);
      expect(cache.some((entry) => entry.name === onboardingStorageKey(scenario.user.id))).toBe(true);
    } finally { await restored.context.close(); }
  });

  test(`${width}px恢复已保存岗位时以实际平台为准，清除其他平台旧登录后再打开`, async ({ scenario, browser, request }) => {
    const position = await createSavedTutorialPosition(request, scenario.user, "zhaopin");
    const restored = await cachedTutorialContext(browser, scenario.user, { ...createOnboardingState(), status: "active", step: "position-start", positionId: position.id, platformId: "boss", environmentReady: true, platformLoggedIn: true }, width, scenario);
    try {
      await restored.page.route("**/api/platforms/config/**", (route) => route.fulfill({ json: { configs: [platformConfig("boss", true), platformConfig("zhaopin", true)] } }));
      await openAdmin(restored.page);
      await expectStep(restored.page, "platform-open");
      const values = (await restored.context.storageState()).origins.find((origin) => origin.origin === new URL(PREVIEW_URL).origin)?.localStorage || [];
      const state = JSON.parse(values.find((entry) => entry.name === onboardingStorageKey(scenario.user.id))?.value || "{}");
      expect(state.positionId).toBe(position.id);
      expect(state.platformId).toBe("zhaopin");
      expect(state.platformLoggedIn).toBe(false);
      expect((await readAgentState(request)).calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path))).toHaveLength(0);
      await headerBrowserButton(restored.page).click();
      await expectStep(restored.page, "platform-login");
      const opened = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open");
      expect(opened).toHaveLength(1);
      expect(opened[0].body.platform_id).toBe("zhaopin");
      await expect(restored.page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", /position-menu|position-start/, { timeout: 25_000 });
      if (await restored.page.getByTestId("onboarding-experience").getAttribute("data-step") === "position-menu") {
        const positions = restored.page.getByRole("menuitem", { name: "岗位管理", exact: true });
        if (!await positions.isVisible()) await restored.page.locator('[data-onboarding="mobile-menu"]').click();
        await positions.click();
      }
      await expectStep(restored.page, "position-start");
    } finally { await restored.context.close(); }
  });

  test(`${width}px表单改平台保留名称并重新确认识别方式，保存前不登录`, async ({ scenario, request }) => {
    const { page, user, context } = scenario;
    await page.setViewportSize({ width, height: 960 });
    await page.route("**/api/platforms/config/**", (route) => route.fulfill({ json: { configs: [platformConfig("boss", true), platformConfig("zhaopin", true)] } }));
    await openFirstPositionForm(page);
    await page.locator('[data-onboarding="position-name"]').fill("切换平台时保留这条岗位名称");
    await page.keyboard.press("Tab");
    await expectStep(page, "position-filter-mode");
    await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
    await expectStep(page, "position-name");
    await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
    await expectStep(page, "position-platform");
    await page.locator('[data-onboarding="position-platform"]').getByRole("radio", { name: /智联招聘/ }).click();
    await expectStep(page, "position-name");
    await expect(page.locator('[data-onboarding="position-name"]')).toHaveValue("切换平台时保留这条岗位名称");
    await expect(page.locator(".ant-tour")).toContainText(/岗位管理/);
    await page.locator('[data-onboarding="position-name"]').focus();
    await page.keyboard.press("Tab");
    await expectStep(page, "position-filter-mode");
    const values = (await context.storageState()).origins.find((origin) => origin.origin === new URL(PREVIEW_URL).origin)?.localStorage || [];
    const state = JSON.parse(values.find((entry) => entry.name === onboardingStorageKey(user.id))?.value || "{}");
    expect(state.platformId).toBe("zhaopin");
    expect(state.platformLoggedIn).toBe(false);
    expect(state.completedFields).not.toContain("detail-mode");
    expect((await readAgentState(request)).calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path))).toHaveLength(0);
  });
}

test("岗位保存前键盘触发顶部浏览器不会绕过遮罩打开Bing或开始登录检测", async ({ scenario, request }) => {
  const { page } = scenario;
  await acceptTutorial(page);
  await expect(page.getByRole("dialog", { name: "新同事，欢迎上岗。", exact: true })).not.toBeVisible();
  const currentStep = await page.getByTestId("onboarding-experience").getAttribute("data-step");
  const before = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/runtime/status").length;
  await headerBrowserButton(page).focus();
  await expect(headerBrowserButton(page), "键盘验证必须真正聚焦顶部按钮，不能在别的输入框按回车而假通过").toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", currentStep!);
  await expect.poll(async () => (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/runtime/status").length).toBeGreaterThan(before);
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path))).toHaveLength(0);
});

/** expectTourClearOfButton 核对说明卡没有压住真实恢复按钮，后续标准点击还会验证按钮可操作。 */
async function expectTourClearOfButton(page: Page, button: Locator) {
  await expect.poll(async () => {
    const [tour, target] = await Promise.all([page.locator(".ant-tour").boundingBox(), button.boundingBox()]);
    return Boolean(tour && target && (tour.x + tour.width <= target.x || target.x + target.width <= tour.x || tour.y + tour.height <= target.y || target.y + target.height <= tour.y));
  }, { message: "教程说明不能压住真实恢复按钮" }).toBe(true);
}

/** expectFeedbackBelowProgress 检查必要弹框完整避让固定进度条，避免手机上标题和错误前半句被盖住。 */
async function expectFeedbackBelowProgress(page: Page, feedback: Locator) {
  await expect.poll(async () => {
    const [modalBox, progressBox] = await Promise.all([feedback.boundingBox(), page.getByRole("region", { name: "新手教程总进度" }).boundingBox()]);
    if (!modalBox || !progressBox) return Number.NEGATIVE_INFINITY;
    return Math.min(modalBox.y - progressBox.y - progressBox.height, (page.viewportSize()?.height || 0) - modalBox.y - modalBox.height);
  }, { message: "必要弹框的标题、原因和按钮应完整位于教程进度条下方的可视范围" }).toBeGreaterThanOrEqual(-1);
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
  await expectPositionEntry(page);
  await tour.getByRole("button", { name: "稍后继续", exact: true }).click();
  await expect(page.getByRole("button", { name: "继续教程", exact: true })).toBeVisible();
  await page.reload();
  await page.getByRole("button", { name: "继续教程", exact: true }).click();
  await expectPositionEntry(page);
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
  await expectStep(page, "position-platform");
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
  await expectPositionEntry(page);
  const agent = await readAgentState(request);
  expect(agent.calls.filter((item: { path: string }) => item.path === "/api/v1/runtime/install")).toHaveLength(1);
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/after-runtime-install.png", fullPage: true });
});

for (const changeDuring of ["welcome", "active"] as const) {
  test(`${changeDuring === "welcome" ? "欢迎阶段" : "教程活动中"}组件从就绪变为缺失，无需刷新就能高亮安装并恢复原进度`, async ({ scenario, request }) => {
    const { page, user } = scenario;
    await openAdmin(page);
    await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
    await expect.poll(async () => (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/runtime/status").length).toBeGreaterThanOrEqual(3);
    if (changeDuring === "active") {
      await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
      await expectPositionEntry(page);
    }
    const currentURL = page.url();
    await agentControl(request, { runtimeReady: false });
    if (changeDuring === "welcome") await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
    await expectStep(page, "runtime-install");
    const installer = page.getByRole("dialog", { name: "准备必要组件", exact: true });
    await expect(installer).toBeVisible();
    await expectMaskAround(page, '[data-onboarding="runtime-install"]');
    await expect(page.getByTestId("onboarding-target-loading")).toHaveCount(0);
    await expectNoCompletion(page, request, user);
    await page.screenshot({ path: `C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/runtime-became-missing-${changeDuring}.png`, fullPage: true });
    await installer.getByRole("button", { name: /安装必要组件$/ }).click();
    await expectPositionEntry(page);
    await expect(installer).not.toBeVisible();
    expect(page.url(), "组件修复和恢复教程不能依赖刷新页面").toBe(currentURL);
    const calls = (await readAgentState(request)).calls as { path: string }[];
    expect(calls.filter((call) => call.path === "/api/v1/runtime/install")).toHaveLength(1);
    expect(calls.filter((call) => call.path === "/api/v1/page/open")).toHaveLength(0);
  });
}

test("微软运行库缺失时先圈下载和重新检查，再引导安装浏览器组件", async ({ scenario, request }) => {
  const { page, context, user } = scenario;
  await agentControl(request, { systemReady: false, runtimeReady: false });
  const attemptedDownloads: string[] = [];
  await context.route("https://example.invalid/test-only-runtime.exe", async (route) => {
    attemptedDownloads.push(route.request().url());
    await route.abort("blockedbyclient");
  });
  await openAdmin(page);
  await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
  await expectStep(page, "runtime-system");
  const installer = page.getByRole("dialog", { name: "准备必要组件", exact: true });
  await expect(installer).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(1);
  const actions = page.locator('[data-onboarding="runtime-system-actions"]');
  await expectMaskAround(page, actions);
  await expect(page.locator(".ant-tour")).toContainText(/运行库/);
  await expect(page.locator(".ant-tour")).toContainText(/检查/);
  const download = actions.getByRole("link", { name: /下载微软运行库/ });
  await expect(download).toHaveAttribute("href", "https://example.invalid/test-only-runtime.exe");
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/manual-full-walkthrough/11-system-runtime-guide.png", fullPage: true });
  const popupPromise = context.waitForEvent("page");
  await download.click();
  const popup = await popupPromise;
  await expect.poll(() => attemptedDownloads.length).toBe(1);
  await popup.close();
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/runtime/install")).toHaveLength(0);
  await expectNoCompletion(page, request, user);
  await actions.getByRole("button", { name: "安装或修复后重新检查", exact: true }).click();
  await expectStep(page, "runtime-install");
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await expectMaskAround(page, '[data-onboarding="runtime-install"]');
  await installer.getByRole("button", { name: /安装必要组件$/ }).click();
  await expectPositionEntry(page);
  const calls = (await readAgentState(request)).calls as { path: string }[];
  expect(calls.filter((call) => call.path === "/api/v1/runtime/system-check")).toHaveLength(1);
  expect(calls.filter((call) => call.path === "/api/v1/runtime/install")).toHaveLength(1);
  expect(calls.findIndex((call) => call.path === "/api/v1/runtime/system-check")).toBeLessThan(calls.findIndex((call) => call.path === "/api/v1/runtime/install"));
});

test("关闭的平台不可选，缓存平台关闭后必须重新选择且不能偷换平台", async ({ scenario, request }) => {
  const { page, user } = scenario;
  let bossOpen = true;
  await page.route("**/api/platforms/config/**", async (route) => {
    await route.fulfill({ json: { ok: true, configs: [platformConfig("boss", bossOpen), platformConfig("zhaopin", true), platformConfig("liepin", false)] } });
  });
  await openFirstPositionForm(page);
  await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
  await expectStep(page, "position-platform");
  const choices = page.locator('[data-onboarding="position-platform"]');
  await expect(choices.getByRole("radio", { name: /猎聘企业端/ })).toBeDisabled();
  await choices.getByRole("radio", { name: /BOSS\s*直聘/i }).click();
  await expectStep(page, "position-name");
  bossOpen = false;
  await page.reload();
  await expectStep(page, "position-platform");
  await expect(choices.getByRole("radio", { name: /BOSS\s*直聘/i })).toBeDisabled();
  await expect(choices.getByRole("radio", { name: /智联招聘/ })).not.toBeChecked();
  await expect(choices).toContainText("暂未开放");
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open")).toHaveLength(0);
  await expectNoCompletion(page, request, user);
  await choices.getByRole("radio", { name: /智联招聘/ }).click();
  await expectStep(page, "position-name");
  await page.locator('[data-onboarding="position-name"]').fill("平台关闭后新建智联岗位");
  await page.keyboard.press("Tab");
  await expectStep(page, "position-filter-mode");
  await page.locator('[data-onboarding="position-filter-mode"]').getByRole("radio", { name: /^关键词筛选/ }).click();
  await expectStep(page, "position-detail-mode");
  await page.locator('[data-onboarding="position-detail-mode"]').getByRole("radio", { name: /^页面文字识别/ }).click();
  await expectStep(page, "position-keywords");
  await page.locator('[data-onboarding="position-keywords"]').fill("招聘");
  await page.keyboard.press("Tab");
  await expectStep(page, "position-save");
  await page.locator('[data-onboarding="position-save"]').click();
  await expectStep(page, "platform-open");
  await headerBrowserButton(page).click();
  await expectStep(page, "platform-login");
  const calls = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open");
  expect(calls).toHaveLength(1);
  expect(calls[0].body).toMatchObject({ platform_id: "zhaopin" });
});

for (const viewport of [{ width: 1280, height: 720 }, { width: 390, height: 844 }]) {
test(`${viewport.width}px启动检查失败展示真实原因与恢复按钮，回退修复后才能重新开跑`, async ({ scenario, request }) => {
  const { page, user } = scenario;
  await page.setViewportSize(viewport);
  const position = await fillFirstPosition(page, request, user, "keyword", "ocr");
  await openRunConfirmation(page, position.id);
  let failNextHealth = true;
  let releaseGuard!: () => void;
  const guardPending = new Promise<void>((resolve) => { releaseGuard = resolve; });
  await page.route("**/health", async (route) => {
    if (!failNextHealth) return route.continue();
    failNextHealth = false;
    await guardPending;
    await route.fulfill({ status: 503, json: { error: { code: "TEST_GUARD_UNAVAILABLE", message: "本机验收：启动检查暂时不可用" } } });
  });
  try {
    await page.locator('[data-onboarding="position-run-confirm"]').click();
    await expectStep(page, "position-start-check");
    await expectMaskAround(page, '.ant-modal-container:has([data-onboarding="position-start-feedback"])');
    await expect(page.locator(".ant-tour")).toContainText(/检查|确认/);
    await expectNoCompletion(page, request, user);
  } finally { releaseGuard(); }
  await expectStep(page, "position-start-error");
  const feedback = page.locator('.ant-modal-container:has([data-onboarding="position-start-feedback"])');
  await expectMaskAround(page, feedback);
  await expectFeedbackBelowProgress(page, feedback);
  await expect(feedback).toContainText("本机验收：启动检查暂时不可用");
  const acknowledge = feedback.getByRole("button", { name: "我知道了", exact: true });
  await expect(acknowledge).toBeEnabled();
  await expectTourClearOfButton(page, acknowledge);
  await expect(page.locator(".ant-tour")).toContainText(/我知道了|处理|修复/);
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => call.path === `/api/v1/local/positions/${position.id}/run`)).toHaveLength(0);
  await expectNoCompletion(page, request, user);
  await page.screenshot({ path: `C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/manual-full-walkthrough/12-start-error-recovery-${viewport.width}.png`, fullPage: true });
  if (viewport.width === 390) await acknowledge.click();
  else await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
  await expectStep(page, "position-start");
  await expect(page.getByRole("dialog", { name: "岗位还没启动成功", exact: true })).toHaveCount(0);
  await openRunConfirmation(page, position.id);
  await page.locator('[data-onboarding="position-run-confirm"]').click();
  await expectStep(page, "first-result");
  await expectNoCompletion(page, request, user);
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => call.path === `/api/v1/local/positions/${position.id}/run`)).toHaveLength(1);
});
}

test("启动发现必要升级时遮罩和立即更新按钮相符，回退不会误启动", async ({ scenario, request }) => {
  const { page, user } = scenario;
  const position = await fillFirstPosition(page, request, user, "keyword", "ocr");
  await openRunConfirmation(page, position.id);
  let reportOldVersion = true;
  await page.route("**/health", async (route) => {
    if (!reportOldVersion) return route.continue();
    reportOldVersion = false;
    const response = await route.fetch();
    const payload = await response.json();
    payload.data = { ...(payload.data || {}), version: "6.2.0.0", agent_version: "6.2.0.0" };
    await route.fulfill({ response, json: payload });
  });
  await page.locator('[data-onboarding="position-run-confirm"]').click();
  await expectStep(page, "position-start-update");
  const feedback = page.locator('.ant-modal-container:has([data-onboarding="position-start-feedback"])');
  await expectMaskAround(page, feedback);
  await expectFeedbackBelowProgress(page, feedback);
  await expect(feedback.getByRole("button", { name: "立即更新", exact: true })).toBeEnabled();
  await expect(page.locator(".ant-tour")).toContainText(/更新/);
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await expectNoCompletion(page, request, user);
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => call.path.endsWith(`/${position.id}/run`))).toHaveLength(0);
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/manual-full-walkthrough/13-start-update-guide.png", fullPage: true });
  await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
  await expectStep(page, "position-start");
  await openRunConfirmation(page, position.id);
  await page.locator('[data-onboarding="position-run-confirm"]').click();
  await expectStep(page, "first-result");
  await expectNoCompletion(page, request, user);
});

for (const endedStatus of ["failed", "stopped"] as const) {
  test(`没有候选结果就${endedStatus}不能算完成，上一步能够重新运行`, async ({ scenario, request }) => {
    const { page, user } = scenario;
    const position = await fillFirstPosition(page, request, user, "keyword", "ocr");
    await startFirstPosition(page, position.id);
    await agentControl(request, { positionID: position.id, status: endedStatus, analysis: null, scannedCount: 12 });
    await expect(page.getByTestId("onboarding-experience")).toContainText(/这一轮已经结束/);
    await expectStep(page, "first-result");
    await expectNoCompletion(page, request, user);
    await page.screenshot({ path: `C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/manual-full-walkthrough/14-task-${endedStatus}-recovery.png`, fullPage: true });
    if (endedStatus === "failed") {
      await page.locator(".ant-tour").getByRole("button", { name: "稍后继续", exact: true }).click();
      await page.getByRole("menuitem", { name: "控制台", exact: true }).click();
      await expect(page).toHaveURL(/\/admin(?:\?|$)/);
      await page.getByRole("button", { name: "继续教程", exact: true }).click();
      await expectStep(page, "position-menu");
      await page.getByRole("menuitem", { name: "岗位管理", exact: true }).click();
      await expectStep(page, "first-result");
      await expect(page.getByTestId("onboarding-experience")).toContainText(/这一轮已经结束/);
      let rejectRecovery = true;
      await page.route(`**/api/v1/local/positions/${position.id}/stop`, async (route) => {
        if (!rejectRecovery) return route.continue();
        rejectRecovery = false;
        await route.fulfill({ status: 503, json: { error: { code: "TEST_RECOVERY_UNAVAILABLE", message: "本机验收：恢复前停止检查暂时不可用" } } });
      });
      await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
      await expect(page.getByTestId("onboarding-experience")).toContainText("本机验收：恢复前停止检查暂时不可用");
      await expect(page.getByTestId("onboarding-experience")).toContainText(/重试|再点|再试/);
      await expectStep(page, "first-result");
      await expect(page.locator(`[data-onboarding="position-start"][data-position-id="${position.id}"]`)).toHaveCount(0);
      expect((await readAgentState(request)).calls.filter((call: { path: string }) => call.path === `/api/v1/local/positions/${position.id}/run`)).toHaveLength(1);
      await expectNoCompletion(page, request, user);
    }
    await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
    await expectStep(page, "position-start");
    await openRunConfirmation(page, position.id);
    await page.locator('[data-onboarding="position-run-confirm"]').click();
    await expectStep(page, "first-result");
    const state = await readAgentState(request);
    expect(state.calls.filter((call: { path: string }) => call.path === `/api/v1/local/positions/${position.id}/run`)).toHaveLength(2);
    expect(state.tasks[position.id].analysis).toBeNull();
    await expectNoCompletion(page, request, user);
    await agentControl(request, { positionID: position.id, analysis: { kind: "keyword", phase: "result", terminal: true, accepted: false, reason: "本机验收：重新运行后关键词未命中，已跳过" } });
    await expect(page.getByRole("button", { name: "开始正式使用", exact: true })).toBeVisible({ timeout: 20_000 });
    await expect.poll(async () => (await readUserFlow(request, user)).tutorial_completed).toBe(true);
  });
}

for (const invalidatedBy of ["删除", "关闭"] as const) {
  test(`暂停期间${invalidatedBy}教程岗位，继续时恢复合法的新建入口`, async ({ scenario, request }) => {
    const { page, user } = scenario;
    const position = await fillFirstPosition(page, request, user, "keyword", "ocr");
    await page.locator(".ant-tour").getByRole("button", { name: "稍后继续", exact: true }).click();
    await expect(page.getByRole("button", { name: "继续教程", exact: true })).toBeVisible();
    const headers = { authorization: `Bearer ${user.token}` };
    const changed = invalidatedBy === "删除"
      ? await request.delete(`${CLOUD_URL}/api/positions/${position.id}`, { headers })
      : await request.post(`${CLOUD_URL}/api/positions`, { headers, data: { id: position.id, platform_id: position.platform_id, name: position.name, is_open: false, keywords: position.keywords, common_config: position.common_config, ai_config: position.ai_config, keyword_config: position.keyword_config } });
    expect(changed.ok(), await changed.text()).toBe(true);
    await page.getByRole("button", { name: "继续教程", exact: true }).click();
    await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", /position-menu|position-create/);
    if (await page.getByTestId("onboarding-experience").getAttribute("data-step") === "position-menu") await page.getByRole("menuitem", { name: "岗位管理", exact: true }).click();
    await expectStep(page, "position-create");
    await expectMaskAround(page, '[data-onboarding="position-create"]');
    await expectNoCompletion(page, request, user);
    await page.locator('[data-onboarding="position-create"]').click();
    await expectStep(page, "position-platform");
    await page.locator('[data-onboarding="position-platform"]').getByRole("radio", { name: /BOSS\s*直聘/i }).click();
    await expectStep(page, "position-name");
    await expect(page.locator('[data-onboarding="position-name"]')).toHaveValue("");
  });
}

test("岗位保存迟到响应遇到实际退出换号，不得把旧账号岗位交给新账号教程", async ({ scenario, request }) => {
  const { page, context, user } = scenario;
  const otherUser = await registerUser(request, "onboarding-save-isolation");
  await fillFirstPosition(page, request, user, "keyword", "ocr", false);
  let releaseSave!: () => void;
  const savePending = new Promise<void>((resolve) => { releaseSave = resolve; });
  let capturedPositionID = "";
  let responseSettled = false;
  await page.route("**/api/positions", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    const response = await route.fetch();
    const payload = await response.json();
    capturedPositionID = String(payload.position?.id || "");
    await savePending;
    try {
      await route.fulfill({ response, json: payload });
    } catch (error) {
      // 真实登录会整页跳转；浏览器取消旧文档请求属于合法结果，不能靠注入事件维持旧页面。
      expect(route.request().failure()?.errorText || String(error)).toMatch(/ABORTED|CANCEL|closed|navigation|request.*finished/i);
    } finally { responseSettled = true; }
  });
  try {
    await page.locator('[data-onboarding="position-save"]').click();
    await expect.poll(() => capturedPositionID).not.toBe("");
    await page.locator(".ant-tour").getByRole("button", { name: "稍后继续", exact: true }).click();
    const form = page.getByRole("dialog", { name: "新建岗位模板", exact: true });
    await page.goBack();
    await expect(page).toHaveURL(/\/admin(?:\?|$)/);
    await expect(form).not.toBeVisible();
    await page.getByRole("button", { name: "退出登录", exact: true }).click();
    await expect(page).toHaveURL(/\/login(?:\?|$)/);
    await page.locator(".public-login-tabs").getByRole("button", { name: "密码登录", exact: true }).click();
    await page.getByLabel("邮箱", { exact: true }).fill(otherUser.email);
    await page.getByLabel("密码", { exact: true }).fill(otherUser.password);
    await page.locator('.public-login-form button[type="submit"]').click();
    await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
    await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
    await expectPositionEntry(page);
    const previousPolls = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/runtime/status").length;
    releaseSave();
    await expect.poll(() => responseSettled).toBe(true);
    await expect.poll(async () => (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/runtime/status").length).toBeGreaterThan(previousPolls);
    await expectPositionEntry(page);
    const cache = (await context.storageState()).origins.find((entry) => entry.origin === new URL(PREVIEW_URL).origin)?.localStorage || [];
    const newUserProgress = cache.find((entry) => entry.name === `goodhr_onboarding_v2_${encodeURIComponent(otherUser.id)}`);
    expect(newUserProgress).toBeTruthy();
    expect(JSON.parse(newUserProgress!.value).positionId).toBe("");
    const originalPositions = await request.get(`${CLOUD_URL}/api/positions`, { headers: { authorization: `Bearer ${user.token}` } });
    const newUserPositions = await request.get(`${CLOUD_URL}/api/positions`, { headers: { authorization: `Bearer ${otherUser.token}` } });
    expect(originalPositions.ok()).toBe(true);
    expect(newUserPositions.ok()).toBe(true);
    expect((await originalPositions.json()).positions.some((position: { id: string }) => position.id === capturedPositionID)).toBe(true);
    expect((await newUserPositions.json()).positions).toHaveLength(0);
    await expectNoCompletion(page, request, otherUser);
    await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/manual-full-walkthrough/15-late-save-account-isolation.png", fullPage: true });
  } finally { releaseSave(); }
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
  await expectPositionEntry(page);
  await expect(page.getByRole("dialog", { name: /系统公告|更新本地程序/ })).toHaveCount(0);
  const state = await readAgentState(request);
  expect(state.calls.filter((item: { path: string }) => item.path === "/api/v1/app-update/start")).toHaveLength(0);
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/optional-update-deferred.png", fullPage: true });
});

test("必要升级优先完成，组件安装与教程不会叠加弹框", async ({ scenario, request }) => {
  const { page } = scenario;
  await page.setViewportSize({ width: 390, height: 844 });
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
  await update.getByText("更新本地程序", { exact: true }).click({ trial: true });
  await page.screenshot({ path: "C:/Users/Administrator/Documents/Codex/2026-10-15/goodhr-onboarding-local/manual-full-walkthrough/16-mobile-required-update.png", fullPage: true });
  await update.getByRole("button", { name: "立即更新", exact: true }).click();
  await expect(update).not.toBeVisible({ timeout: 20_000 });
  const installer = page.getByRole("dialog", { name: "准备必要组件", exact: true });
  await expect(installer).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await expectMaskAround(page, '[data-onboarding="runtime-install"]');
  await installer.getByRole("button", { name: /安装必要组件$/ }).click();
  await expectPositionEntry(page);
  const state = await readAgentState(request);
  expect(state.calls.filter((item: { path: string }) => item.path === "/api/v1/app-update/start")).toHaveLength(1);
  expect(state.calls.filter((item: { path: string }) => item.path === "/api/v1/runtime/install")).toHaveLength(1);
});

test("顶部浏览器按钮打开所选平台，等待登录和刷新恢复都不再指向教程自造入口", async ({ scenario, request }) => {
  const { page, user } = scenario;
  await agentControl(request, { loggedIn: false });
  const platformURL = "https://tutorial-zhaopin.example.invalid/recruiting";
  await page.route("**/api/platforms/config/**", async (route) => {
    await route.fulfill({ json: { ok: true, configs: [{ config_key: "platform.zhaopin", enabled: true, config_value: JSON.stringify({ id: "zhaopin", open: true, auth: { pages: [{ url: platformURL, match: "prefix", entry: true }] }, public: { pages: [{ url: "https://tutorial-zhaopin.example.invalid/login", match: "prefix", code: "login" }] } }) }] } });
  });
  const position = await fillFirstPosition(page, request, user, "keyword", "dom", true, { platformName: /智联招聘/, loginAfterSave: false });
  expect(position.platform_id).toBe("zhaopin");
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
  await expect(tour).toContainText("岗位已经保存");
  await expect(tour).toContainText("请切到刚打开的招聘浏览器");
  await expect(tour).toContainText("在智联招聘里登录招聘账号");
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
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-start", { timeout: 25_000 });
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open")).toHaveLength(2);
});

for (const missingPage of ["", "https://www.bing.com/"]) {
  test(`登录检测遇到${missingPage ? "其他网站" : "空白页面"}时提示顶部重开，实际登录后才继续`, async ({ scenario, request }) => {
    const { page, user } = scenario;
    await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false });
    await headerBrowserButton(page).click();
    await expectStep(page, "platform-login");
    await agentControl(request, { opened: true, loggedIn: true, pageURL: missingPage });
    const tour = page.locator(".ant-tour");
    await expect(tour).toContainText("暂未找到所选招聘平台页面", { timeout: 20_000 });
    await expect(tour).toContainText("请再点顶部“打开浏览器”");
    await expectStep(page, "platform-login");
    expect((await readUserFlow(request, user)).tutorial_completed).not.toBe(true);
    await expectMaskAround(page, headerBrowserButton(page));
    await headerBrowserButton(page).click();
    await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-start", { timeout: 25_000 });
    const opened = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open");
    expect(opened).toHaveLength(2);
    expect(opened[1].body.url).not.toContain("bing.com");
  });
}

test("登录检测报错后停止转圈，顶部真实入口仍能重开恢复", async ({ scenario, request }) => {
  const { page, user } = scenario;
  await agentControl(request, { loggedIn: false });
  await page.route("**/api/v1/page/url", (route) => route.fulfill({ status: 500, json: { error: "招聘浏览器暂时没连上" } }));
  await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false });
  await headerBrowserButton(page).click();
  await expectStep(page, "platform-login");
  const tour = page.locator(".ant-tour");
  await expect(tour.getByRole("alert").filter({ hasText: "招聘浏览器暂时没连上" })).toBeVisible({ timeout: 20_000 });
  await expect(tour.locator(".ant-spin")).toHaveCount(0);
  await expect(headerBrowserButton(page)).toBeEnabled();
  await expectMaskAround(page, headerBrowserButton(page));
  await page.unroute("**/api/v1/page/url");
  await agentControl(request, { loggedIn: true });
  await headerBrowserButton(page).click();
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-start", { timeout: 25_000 });
});

test("模拟预览在欢迎和平台登录明确标注不会打开真实浏览器", async ({ scenario, request }) => {
  test.skip(process.env.UI_TEST_EXPECT_SIMULATION !== "true", "仅在明确配置模拟构建时检查环境标记");
  const { page, user } = scenario;
  await agentControl(request, { loggedIn: false });
  await openAdmin(page);
  const welcome = page.getByRole("dialog", { name: "新同事，欢迎上岗。", exact: true });
  await expect(welcome.getByTestId("onboarding-simulation-notice")).toContainText("不会打开真实招聘浏览器");
  await expect(welcome.getByRole("link", { name: "打开真实浏览器验收" })).toHaveAttribute("href", "http://127.0.0.1:5174/admin");
  await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
  await expectPositionEntry(page);
  await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false, invitationAccepted: true });
  await headerBrowserButton(page).click();
  await expectStep(page, "platform-login");
  await expect(page.locator(".ant-tour").getByTestId("onboarding-simulation-notice")).toContainText("不会打开真实招聘浏览器");
});

test("390px手机浏览器入口的遮罩圈住真实顶部图标，登录等待时仍可点击", async ({ scenario, request }) => {
  const { page, user } = scenario;
  await page.setViewportSize({ width: 390, height: 844 });
  await agentControl(request, { loggedIn: false });
  await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false });
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
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path))).toHaveLength(0);
  await page.locator('[data-onboarding="position-save"]').click();
  await loginSavedPosition(page);
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

test("旧平台配置缺public时按登录前缀识别，泛域名命中登录页也不能提前确认", async ({ scenario, request }) => {
  const { page, user } = scenario;
  const entryURL = "https://legacy-zhaopin.example.invalid/recommend";
  const loginURL = "https://legacy-zhaopin.example.invalid/login";
  await page.route("**/api/platforms/config/**", (route) => route.fulfill({ json: { configs: [{ config_key: "platform.zhaopin", enabled: true, config_value: { id: "zhaopin", open: true, auth: { pages: [{ url: entryURL, match: "contains", entry: true }, { url: "https://legacy-zhaopin.example.invalid", match: "contains" }], entry_url: entryURL, login_url_prefixes: ["https://passport-legacy.example.invalid", loginURL], logged_in_url_contains: ["legacy-zhaopin.example.invalid"] } } }] } }));
  const position = await fillFirstPosition(page, request, user, "keyword", "dom", true, { platformName: /智联招聘/, loginAfterSave: false });
  await headerBrowserButton(page).click();
  await expectStep(page, "platform-login");
  await agentControl(request, { opened: true, loggedIn: true, pageURL: loginURL });
  await expect.poll(async () => (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/url").length, { timeout: 25_000 }).toBeGreaterThanOrEqual(4);
  await expectStep(page, "platform-login");
  await expect(page.locator(".ant-tour")).toContainText(/扫码|验证码/);
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
  await agentControl(request, { pageURL: entryURL });
  await expect(page.getByTestId("onboarding-experience")).toHaveAttribute("data-step", "position-start", { timeout: 20_000 });
  const opened = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/page/open");
  expect(opened).toHaveLength(1);
  expect(opened[0].body).toMatchObject({ platform_id: "zhaopin", url: entryURL });
  await expect(page.locator(`[data-onboarding="position-start"][data-position-id="${position.id}"]`)).toBeVisible();
});

for (const width of [1440, 390]) {
  test(`${width}px开窗前上一步回到同岗位编辑，逐字段回看且保存修改不增岗`, async ({ scenario, request }) => {
    const { page, user } = scenario;
    await page.setViewportSize({ width, height: 960 });
    const position = await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false });
    await openTutorialPositionReview(page, position.id);
    await expectStep(page, "position-save");
    await expect(page.locator('[data-onboarding="position-name"]')).toHaveValue(position.name);
    for (const step of ["position-keywords", "position-detail-mode", "position-filter-mode", "position-name", "position-platform"]) {
      await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
      await expectStep(page, step);
      await expectMaskAround(page, `[data-onboarding="${step}"]`);
    }
    const platform = page.locator('[data-onboarding="position-platform"]');
    const radios = platform.getByRole("radio");
    expect(await radios.count()).toBeGreaterThan(1);
    for (const radio of await radios.all()) await expect(radio, "已保存岗位的真实平台不能在复核表单中更换").toBeDisabled();
    await expect(platform.getByRole("radio", { name: /BOSS\s*直聘/i })).toBeChecked();
    await platform.getByRole("button", { name: "确认使用此平台", exact: true }).click();
    await expectStep(page, "position-save");
    await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
    await expectStep(page, "position-keywords");
    await page.locator('[data-onboarding="position-keywords"]').fill(`Java Spring 修改条件${width}`);
    await page.keyboard.press("Tab");
    await expectStep(page, "position-save");
    const form = page.getByRole("dialog", { name: "编辑岗位模板", exact: true });
    await expect(form.getByRole("button", { name: "保存修改", exact: true })).toBeEnabled();
    await page.screenshot({ path: test.info().outputPath(`review-${width}-same-position.png`), fullPage: true });
    await form.getByRole("button", { name: "保存修改", exact: true }).click();
    await expectStep(page, "platform-open");
    const positions = await readTutorialPositions(request, user);
    expect(positions).toHaveLength(1);
    expect(positions[0]).toMatchObject({ id: position.id, name: position.name, platform_id: "boss", keywords: ["Java", "Spring", `修改条件${width}`] });
    expect(await readTutorialProgress(page, user)).toMatchObject({ positionId: position.id, platformLoggedIn: false });
    expect((await readAgentState(request)).calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path) || /\/(run|stop)$/.test(call.path))).toHaveLength(0);
  });
}

test("已有岗位编辑草稿刷新后保留同ID，取消回编辑入口且不覆盖云端岗位", async ({ scenario, request }) => {
  const { page, user, context } = scenario;
  const position = await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false });
  await openTutorialPositionReview(page, position.id);
  await expectStep(page, "position-save");
  for (const step of ["position-keywords", "position-detail-mode", "position-filter-mode", "position-name"]) {
    await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
    await expectStep(page, step);
  }
  const draftName = "原岗位编辑草稿，刷新后还在";
  await page.locator('[data-onboarding="position-name"]').fill("");
  await page.keyboard.press("Tab");
  await expectStep(page, "position-name");
  await page.locator('[data-onboarding="position-name"]').fill(draftName);
  await expect.poll(async () => (await context.storageState()).origins.some((origin) => origin.localStorage.some((entry) => entry.name.includes("draft") && entry.value.includes(draftName) && entry.value.includes(position.id)))).toBe(true);
  await page.reload();
  await expectStep(page, "position-name");
  const form = page.getByRole("dialog", { name: "编辑岗位模板", exact: true });
  await expect(form).toBeVisible();
  await expect(page.locator('[data-onboarding="position-name"]')).toHaveValue(draftName);
  expect(await readTutorialProgress(page, user)).toMatchObject({ positionId: position.id, reviewPosition: true });
  expect(await readTutorialPositions(request, user)).toMatchObject([{ id: position.id, name: position.name }]);
  const cancel = form.getByRole("button", { name: "取消", exact: true });
  await cancel.focus();
  await expect(cancel).toBeFocused();
  await cancel.press("Enter");
  await expect(form).not.toBeVisible();
  await expectStep(page, "position-edit");
  await expectMaskAround(page, `[data-onboarding="position-edit"][data-position-id="${position.id}"]`);
  await page.locator(`[data-onboarding="position-edit"][data-position-id="${position.id}"]`).click();
  await expect(page.locator('[data-onboarding="position-name"]')).toHaveValue(draftName);
  expect(await readTutorialPositions(request, user)).toMatchObject([{ id: position.id, name: position.name }]);
});

test("登录成功响应迟到也不能把已经回退的岗位编辑跳回开始节点", async ({ scenario, request }) => {
  const { page, user } = scenario;
  const position = await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false });
  let releaseLogin!: () => void;
  const pending = new Promise<void>((resolve) => { releaseLogin = resolve; });
  let reads = 0, held = false, settled = false;
  await page.route("**/api/v1/page/url", async (route) => {
    const response = await route.fetch();
    if (++reads !== 3) return route.fulfill({ response });
    held = true;
    await pending;
    try { await route.fulfill({ response }); }
    catch (error) { expect(route.request().failure()?.errorText || String(error)).toMatch(/ABORT|CANCEL|closed|finished/i); }
    finally { settled = true; }
  });
  try {
    await headerBrowserButton(page).click();
    await expectStep(page, "platform-login");
    await expect.poll(() => held, { message: "必须真正拦住第三次成功登录响应，才能复现返回后的迟到确认", timeout: 25_000 }).toBe(true);
    await page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true }).click();
    await openTutorialPositionReview(page, position.id);
    await expectStep(page, "position-save");
    const before = (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/runtime/status").length;
    releaseLogin();
    await expect.poll(() => settled).toBe(true);
    await expect.poll(async () => (await readAgentState(request)).calls.filter((call: { path: string }) => call.path === "/api/v1/runtime/status").length).toBeGreaterThan(before);
    await expectStep(page, "position-save");
    expect(await readTutorialProgress(page, user)).toMatchObject({ positionId: position.id, platformLoggedIn: false, reviewPosition: true });
    await expect(page.getByRole("dialog", { name: "编辑岗位模板", exact: true })).toBeVisible();
    expect((await readAgentState(request)).calls.filter((call: { path: string }) => /\/(run|stop)$/.test(call.path))).toHaveLength(0);
  } finally { releaseLogin(); }
});

test("帮助里可以返回原岗位，并从头重走邀请和平台确认而不另建岗位", async ({ scenario, request }) => {
  const { page, user } = scenario;
  const position = await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false });
  await page.locator(".ant-tour").getByRole("button", { name: "遇到问题", exact: true }).click();
  const help = page.getByRole("dialog", { name: /这一步怎么做/ });
  await help.getByRole("button", { name: "上一步", exact: true }).click();
  await expect(help).not.toBeVisible();
  await expectStep(page, "position-edit");
  await page.locator(".ant-tour").getByRole("button", { name: "遇到问题", exact: true }).click();
  await help.getByRole("button", { name: "从头开始", exact: true }).click();
  const confirm = page.getByRole("dialog", { name: "从第一步再走一遍？", exact: true });
  await expect(confirm).toContainText(/岗位.*保留|保留.*岗位/);
  await confirm.getByRole("button", { name: "从头带我一遍", exact: true }).click();
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  await expect(help).not.toBeVisible();
  expect(await readTutorialProgress(page, user)).toMatchObject({ status: "invited", positionId: position.id, completedFields: [], replaying: true, reviewPosition: true });
  await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
  await expectStep(page, "position-edit");
  await expectMaskAround(page, `[data-onboarding="position-edit"][data-position-id="${position.id}"]`);
  await page.locator(`[data-onboarding="position-edit"][data-position-id="${position.id}"]`).click();
  await expectStep(page, "position-platform");
  await expect(page.getByRole("dialog", { name: "编辑岗位模板", exact: true })).toBeVisible();
  await page.locator('[data-onboarding="position-platform"]').getByRole("button", { name: "确认使用此平台", exact: true }).click();
  await expectStep(page, "position-name");
  await expect(page.locator('[data-onboarding="position-name"]')).toHaveValue(position.name);
  await page.locator('[data-onboarding="position-name"]').focus();
  await page.keyboard.press("Tab");
  await expectStep(page, "position-filter-mode");
  await page.locator('[data-onboarding="position-filter-mode"]').getByRole("radio", { name: /^关键词筛选/ }).click();
  await expectStep(page, "position-detail-mode");
  await page.locator('[data-onboarding="position-detail-mode"]').getByRole("radio", { name: /^本地识别/ }).click();
  await expectStep(page, "position-keywords");
  expect((await page.locator('[data-onboarding="position-keywords"]').inputValue()).split(/[，,\s]+/)).toEqual(["Java", "Spring", "后端开发"]);
  await page.locator('[data-onboarding="position-keywords"]').focus();
  await page.keyboard.press("Tab");
  await expectStep(page, "position-save");
  await page.getByRole("dialog", { name: "编辑岗位模板", exact: true }).getByRole("button", { name: "保存修改", exact: true }).click();
  await expectStep(page, "platform-open");
  expect(await readTutorialPositions(request, user)).toMatchObject([{ id: position.id, name: position.name, platform_id: position.platform_id }]);
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path) || /\/(run|stop)$/.test(call.path))).toHaveLength(0);
});

for (const entry of ["顶部", "暂停"] as const) {
  test(`${entry}从头开始需要确认，取消保留进度，确认后保留真实岗位且无开窗运行副作用`, async ({ scenario, request }) => {
    const { page, user } = scenario;
    const position = await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false });
    if (entry === "暂停") {
      await page.locator(".ant-tour").getByRole("button", { name: "稍后继续", exact: true }).click();
      await expect(page.getByRole("button", { name: "继续教程", exact: true })).toBeVisible();
    }
    const restart = entry === "顶部" ? page.locator('[aria-label="新手教程总进度"]').getByRole("button", { name: "从头开始", exact: true }) : page.getByRole("button", { name: "从头开始", exact: true });
    await restart.click();
    const confirm = page.getByRole("dialog", { name: "从第一步再走一遍？", exact: true });
    await confirm.getByRole("button", { name: "先不重来", exact: true }).click();
    await expect(confirm).not.toBeVisible();
    expect(await readTutorialProgress(page, user)).toMatchObject({ status: entry === "顶部" ? "active" : "paused", positionId: position.id, step: "platform-open" });
    await restart.click();
    await confirm.getByRole("button", { name: "从头带我一遍", exact: true }).click();
    await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
    await page.reload();
    await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
    expect(await readTutorialProgress(page, user)).toMatchObject({ status: "invited", positionId: position.id, replaying: true, completedFields: [] });
    await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
    await expectStep(page, "position-edit");
    expect(await readTutorialPositions(request, user)).toMatchObject([{ id: position.id, name: position.name }]);
    expect((await readAgentState(request)).calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path) || /\/(run|stop)$/.test(call.path))).toHaveLength(0);
  });
}

test("运行中不能从头重置教程，明确去停止只暂停遮罩而不自动停止任务", async ({ scenario, request }) => {
  const { page, user } = scenario;
  const position = await fillFirstPosition(page, request, user, "keyword", "ocr");
  await startFirstPosition(page, position.id);
  await page.locator('[aria-label="新手教程总进度"]').getByRole("button", { name: "从头开始", exact: true }).click();
  const confirm = page.getByRole("dialog", { name: "从第一步再走一遍？", exact: true });
  await expect(confirm.getByRole("button", { name: "从头带我一遍", exact: true })).toBeDisabled();
  await expect(confirm).toContainText(/运行|停止/);
  expect(await readTutorialProgress(page, user)).toMatchObject({ status: "active", step: "first-result", positionId: position.id, positionStarted: true });
  await confirm.getByRole("button", { name: "稍后继续，去停止岗位", exact: true }).click();
  await expect(confirm).not.toBeVisible();
  await expect(page.getByRole("button", { name: "继续教程", exact: true })).toBeVisible();
  await expect(page.getByTestId("onboarding-tour")).toHaveCount(0);
  expect(await readTutorialProgress(page, user)).toMatchObject({ status: "paused", step: "first-result", positionId: position.id, positionStarted: true });
  const agent = await readAgentState(request);
  expect(agent.tasks[position.id].status).toBe("running");
  expect(agent.calls.filter((call: { path: string }) => call.path === `/api/v1/local/positions/${position.id}/stop`)).toHaveLength(0);
  expect((await readTutorialPositions(request, user))[0]).toMatchObject({ id: position.id, status: "running" });
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(false);
});

test("已有岗位保存修改进行中禁止回退和重走，迟到保存仍落在同一个岗位", async ({ scenario, request }) => {
  const { page, user } = scenario;
  const position = await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false });
  await openTutorialPositionReview(page, position.id);
  await expectStep(page, "position-save");
  let releaseSave!: () => void;
  const pending = new Promise<void>((resolve) => { releaseSave = resolve; });
  let held = false, settled = false;
  await page.route("**/api/positions", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    expect(route.request().postDataJSON().id).toBe(position.id);
    const response = await route.fetch();
    held = true;
    await pending;
    await route.fulfill({ response });
    settled = true;
  });
  try {
    await page.getByRole("dialog", { name: "编辑岗位模板", exact: true }).getByRole("button", { name: "保存修改", exact: true }).click();
    await expect.poll(() => held).toBe(true);
    await expect(page.locator(".ant-tour").getByRole("button", { name: "上一步", exact: true })).toBeDisabled();
    await page.locator('[aria-label="新手教程总进度"]').getByRole("button", { name: "从头开始", exact: true }).click();
    const confirm = page.getByRole("dialog", { name: "从第一步再走一遍？", exact: true });
    await expect(confirm.getByRole("button", { name: "从头带我一遍", exact: true })).toBeDisabled();
    await expect(confirm).toContainText(/处理中|保存/);
    await confirm.getByRole("button", { name: "先不重来", exact: true }).click();
    await expectStep(page, "position-save");
    releaseSave();
    await expect.poll(() => settled).toBe(true);
    await expectStep(page, "platform-open");
    expect(await readTutorialPositions(request, user)).toMatchObject([{ id: position.id, name: position.name }]);
    expect(await readTutorialProgress(page, user)).toMatchObject({ positionId: position.id, reviewPosition: false, platformLoggedIn: false });
    expect((await readAgentState(request)).calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path) || /\/(run|stop)$/.test(call.path))).toHaveLength(0);
  } finally { releaseSave(); }
});

test("云端已完成账号从新手引导确认重走，刷新继续复核且不撤销完成事实", async ({ scenario, request }) => {
  const { page, user } = scenario;
  const position = await fillFirstPosition(page, request, user, "keyword", "ocr", true, { loginAfterSave: false });
  const completed = await request.post(`${CLOUD_URL}/api/user-flow`, { headers: { authorization: `Bearer ${user.token}` }, data: { step: "first_screening_result", source: "local_agent_test_fixture", position_id: position.id, metadata: { kind: "keyword", phase: "result", terminal: true, accepted: false } } });
  expect(completed.ok(), await completed.text()).toBeTruthy();
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(true);
  await page.reload();
  await expect(page.getByTestId("onboarding-experience")).toHaveCount(0);
  const trialNotice = page.getByRole("dialog", { name: "Max 全能版已到账", exact: true });
  await expect(trialNotice).toBeVisible();
  await trialNotice.getByRole("button", { name: "我知道了", exact: true }).click();
  await expect(trialNotice).not.toBeVisible();
  const announcements = page.getByRole("dialog", { name: "系统公告", exact: true });
  await expect(announcements).toBeVisible();
  await announcements.getByRole("button", { name: "我知道了", exact: true }).click();
  await expect(announcements).not.toBeVisible();
  await page.getByRole("button", { name: "新手引导", exact: true }).click();
  const confirm = page.getByRole("dialog", { name: "从第一步再走一遍？", exact: true });
  await expect(confirm).toBeVisible();
  await expect(page.getByRole("dialog"), "重走确认打开后普通公告和推广提示不能再次叠上来").toHaveCount(1);
  await expect(confirm.getByRole("button", { name: "从头带我一遍", exact: true })).toBeEnabled();
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(true);
  await confirm.getByRole("button", { name: "从头带我一遍", exact: true }).click();
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  await page.reload();
  await expect(page.getByTestId("onboarding-welcome")).toBeVisible();
  expect(await readTutorialProgress(page, user)).toMatchObject({ status: "invited", positionId: position.id, replaying: true, completedFields: [] });
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(true);
  await page.getByRole("button", { name: "好，带我跑一遍", exact: true }).click();
  await expectStep(page, "position-edit");
  await page.locator(`[data-onboarding="position-edit"][data-position-id="${position.id}"]`).click();
  await expectStep(page, "position-platform");
  await page.reload();
  await expectStep(page, "position-platform");
  await expect(page.getByRole("dialog", { name: "编辑岗位模板", exact: true })).toBeVisible();
  await page.locator('[data-onboarding="position-platform"]').getByRole("button", { name: "确认使用此平台", exact: true }).click();
  await expectStep(page, "position-name");
  await expect(page.locator('[data-onboarding="position-name"]')).toHaveValue(position.name);
  expect((await readUserFlow(request, user)).tutorial_completed).toBe(true);
  expect(await readTutorialPositions(request, user)).toMatchObject([{ id: position.id, name: position.name }]);
  expect((await readAgentState(request)).calls.filter((call: { path: string }) => ["/api/v1/page/open", "/api/v1/page/url"].includes(call.path) || /\/(run|stop)$/.test(call.path))).toHaveLength(0);
});
