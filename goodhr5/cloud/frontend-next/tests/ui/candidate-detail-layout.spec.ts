/** 文件作用说明：使用独立的本机合成 API 验证候选人、面试和推荐详情的新布局与数据隔离；只用标准定位、键盘、鼠标和截图，不注入页面脚本。 */
import { test as base, expect, type Locator, type Page } from "@playwright/test";
import type { Server } from "node:http";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";

const previewOrigin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
const candidateName = "林晓（演示）";
const candidateID = "candidate-linxiao";
const applicationID = "application-linxiao";
const primaryPosition = "高级产品经理";
const secondaryPosition = "企业招聘解决方案顾问（长岗位名称演示）";

type RequestCall = { path: string; method: string; body?: Record<string, any> };
type PreviewState = { user: Record<string, any>; candidates: any[]; positions: any[]; notes: any[]; recommendations: any[]; applications: any[]; [key: string]: any };
type MockSession = { state: PreviewState; calls: RequestCall[]; errors: string[] };
type PreviewModule = { createPreviewState: () => PreviewState; createPreviewServer: (state: PreviewState) => Server };

const test = base.extend<{ mock: MockSession }>({
  mock: async ({ page }, use) => {
    // 用变量路径动态加载同一 HTTP fixture，每条测试拥有独立内存状态和随机本机端口。
    const fixtureURL = pathToFileURL(resolve(process.cwd(), "tests/fixtures/candidate-detail-preview.mjs")).href;
    const fixture = await import(fixtureURL) as PreviewModule;
    const state = fixture.createPreviewState();
    const server = fixture.createPreviewServer(state);
    await new Promise<void>((done, reject) => { server.once("error", reject); server.listen(0, "127.0.0.1", done); });
    const address = server.address();
    if (!address || typeof address === "string") throw new Error("合成预览端口没有创建成功。");
    const apiOrigin = `http://127.0.0.1:${address.port}`;
    const calls: RequestCall[] = [];
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.context().route("**/*", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (url.origin === previewOrigin && !url.pathname.startsWith("/api/")) return route.continue();
      if (!url.pathname.startsWith("/api/") && url.pathname !== "/health") return route.abort();
      let body: Record<string, any> | undefined;
      try { body = request.postDataJSON() as Record<string, any>; } catch { /* GET 和预检请求没有 JSON 正文。 */ }
      calls.push({ path: url.pathname, method: request.method(), body });
      const response = await route.fetch({ url: `${apiOrigin}${url.pathname}${url.search}` });
      return route.fulfill({ response });
    });
    try { await use({ state, calls, errors }); }
    finally {
      await page.context().unrouteAll({ behavior: "wait" });
      server.closeAllConnections();
      await new Promise<void>((done) => server.close(() => done()));
    }
  },
});

test.use({ storageState: { cookies: [], origins: [{ origin: previewOrigin, localStorage: [{ name: "goodhr5_access_token", value: "candidate-detail-synthetic-only" }] }] } });

/** waitForModalReady 等待实际入场动画结束，再读取双栏坐标，避免动画缩放导致几何误差。 */
async function waitForModalReady(page: Page) {
  const modal = page.locator(".ant-modal:visible").last();
  await expect(modal).toBeVisible();
  await expect(modal).not.toHaveClass(/ant-zoom-(appear|enter)/);
}

/** openCandidate 从真实列表入口打开详情，确认资料加载后再进行布局断言。 */
async function openCandidate(page: Page) {
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: candidateName, exact: true }).click();
  const dialog = page.getByRole("dialog", { name: candidateName, exact: true });
  await expect(dialog.getByTestId("resume-detail-main")).toBeVisible();
  await expect(dialog.getByRole("heading", { name: candidateName, exact: true })).toBeVisible();
  await waitForModalReady(page);
  return dialog;
}

/** openApplication 从企业招聘列表打开独立面试详情，保持应用中的真实点击路径。 */
async function openApplication(page: Page) {
  await page.goto("/admin/recruitment");
  await page.getByRole("button", { name: "查看进度", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: `${candidateName} · ${primaryPosition}`, exact: true });
  await expect(dialog).toBeVisible();
  await waitForModalReady(page);
  return dialog;
}

/** measuredBox 读取标准定位器的实际尺寸，避免用页面脚本读取或改变布局。 */
async function measuredBox(locator: Locator) {
  const box = await locator.boundingBox();
  if (!box) throw new Error("待验证区域暂时没有可读取的尺寸。");
  return box;
}

/** expectWithinViewport 用实际包围框检查窄屏左右边界，不读取 DOM 脚本属性。 */
async function expectWithinViewport(locator: Locator, width: number) {
  const box = await measuredBox(locator);
  expect(box.x).toBeGreaterThanOrEqual(-1);
  expect(box.x + box.width).toBeLessThanOrEqual(width + 1);
}

/** scrollWithWheel 在区域内发送真实鼠标滚轮，并通过内容坐标变化证明滚动发生。 */
async function scrollWithWheel(page: Page, region: Locator, movingContent: Locator, distance = 700) {
  await region.scrollIntoViewIfNeeded();
  const area = await measuredBox(region);
  const before = await measuredBox(movingContent);
  const viewport = page.viewportSize();
  if (!viewport) throw new Error("预览视口没有设置。");
  const pointerY = Math.min(viewport.height - 30, Math.max(30, area.y + Math.min(area.height / 2, 120)));
  await page.mouse.move(area.x + area.width / 2, pointerY);
  await page.mouse.wheel(0, distance);
  await expect.poll(async () => (await measuredBox(movingContent)).y).toBeLessThan(before.y - 8);
}

test("桌面详情采用宽弹层和双栏，标签切换保留独立记录", async ({ page, mock }, testInfo) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  const dialog = await openCandidate(page);
  const dialogBox = await measuredBox(dialog);
  expect(dialogBox.width).toBeGreaterThan(1200);
  const main = dialog.getByTestId("resume-detail-main");
  const aside = dialog.getByTestId("resume-detail-aside");
  const [mainBox, asideBox] = await Promise.all([measuredBox(main), measuredBox(aside)]);
  expect(asideBox.x).toBeGreaterThanOrEqual(mainBox.x + mainBox.width);
  expect(Math.abs(asideBox.y - mainBox.y)).toBeLessThan(2);
  await expect(dialog.getByRole("tab", { name: "简历", exact: true })).toHaveAttribute("aria-selected", "true");
  await dialog.getByRole("tab", { name: /^推荐 \d/ }).click();
  await expect(dialog.getByTestId("resume-recommendation-history")).toContainText("初试通过，已安排复试。");
  await dialog.getByRole("tab", { name: "任务记录", exact: true }).click();
  await expect(dialog.getByTestId("resume-auto-tasks-section")).toContainText("合成自动任务：已读取候选人详情");
  await dialog.getByRole("tab", { name: "简历", exact: true }).click();
  await expect(dialog.getByText("合成星野科技有限公司", { exact: true })).toBeVisible();
  await expect(dialog.getByTestId("resume-notes-section")).toBeVisible();
  await expect(dialog.getByRole("button", { name: /刷新/ })).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("candidate-detail-desktop.png"), fullPage: true, animations: "disabled" });
  expect(mock.errors).toEqual([]);
});

test("推荐保存按岗位隔离，切换标签与通用备注不会复制推荐内容", async ({ page, mock }) => {
  await page.setViewportSize({ width: 1440, height: 1000 });
  const dialog = await openCandidate(page);
  await dialog.getByRole("tab", { name: /^推荐 \d/ }).click();
  const history = dialog.getByTestId("resume-recommendation-history");
  const form = dialog.getByTestId("resume-recommendations-section");
  await dialog.getByRole("button", { name: `选择推荐记录 ${secondaryPosition}`, exact: true }).click();
  await expect(history).toContainText("另一个岗位的独立跟进历史");
  await expect(history).not.toContainText("初试通过，已安排复试。");
  await form.getByRole("radio", { name: "面试中", exact: true }).check();
  await form.getByRole("textbox", { name: "新增岗位跟进备注", exact: true }).fill("合成岗位专属备注：顾问岗位安排面试。");
  await dialog.getByRole("tab", { name: "简历", exact: true }).click();
  await dialog.getByRole("tab", { name: /^推荐 \d/ }).click();
  await expect(form.getByRole("textbox", { name: "新增岗位跟进备注", exact: true })).toHaveValue("合成岗位专属备注：顾问岗位安排面试。");
  await form.getByRole("button", { name: "记录跟进", exact: true }).click();
  await expect(history.getByText("合成岗位专属备注：顾问岗位安排面试。", { exact: true })).toBeVisible();
  const primaryRecord = mock.state.recommendations.find((item) => item.position_id === "position-product");
  const secondaryRecord = mock.state.recommendations.find((item) => item.position_id === "position-consultant");
  expect(primaryRecord.events).toHaveLength(3);
  expect(secondaryRecord.stage).toBe("interview");
  expect(secondaryRecord.events).toHaveLength(2);
  await dialog.getByRole("button", { name: `选择推荐记录 ${primaryPosition}`, exact: true }).click();
  await expect(history).toContainText("初试通过，已安排复试。");
  await expect(history).not.toContainText("合成岗位专属备注：顾问岗位安排面试。");
  const savedRecommendations = JSON.stringify(mock.state.recommendations);
  const notes = dialog.getByTestId("resume-notes-section");
  await notes.getByRole("textbox", { name: "新增候选人备注", exact: true }).fill("合成通用备注：周五下午方便沟通。");
  await notes.getByRole("button", { name: "加备注", exact: true }).click();
  await expect(notes.getByText("合成通用备注：周五下午方便沟通。", { exact: true })).toBeVisible();
  expect(JSON.stringify(mock.state.recommendations)).toBe(savedRecommendations);
  await expect(history).not.toContainText("合成通用备注：周五下午方便沟通。");
  expect(mock.calls.filter((call) => call.method === "POST" && call.path === `/api/candidates/${candidateID}/recommendations`)).toHaveLength(1);
  expect(mock.calls.filter((call) => call.method === "POST" && call.path === `/api/candidates/${candidateID}/notes`)).toHaveLength(1);
  expect(mock.errors).toEqual([]);
});

test("已关闭岗位保留历史，推荐阶段和保存入口只读", async ({ page, mock }) => {
  mock.state.recommendations.push({ id: "rec-closed", candidate_id: candidateID, position_id: "position-closed", position_name: "招聘运营 · 历史岗位", stage: "paused", events: [{ id: "closed-event", from_stage: "contacting", to_stage: "paused", content: "合成关闭岗位的历史仍然保留。", author_name: "周禾", occurred_at: "2026-10-15T10:00:00+08:00" }] });
  await page.goto("/admin/recommendations");
  const row = page.getByRole("row").filter({ hasText: "招聘运营 · 历史岗位" });
  await expect(row.getByText("已关闭 · 历史保留", { exact: true })).toBeVisible();
  await row.getByRole("button", { name: "查看", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: candidateName, exact: true });
  await expect(dialog.getByTestId("resume-recommendation-history")).toContainText("合成关闭岗位的历史仍然保留。");
  const form = dialog.getByTestId("resume-recommendations-section");
  await expect(form.getByText("这个岗位当前不可推荐，历史记录还在。", { exact: true })).toBeVisible();
  await expect(form.getByRole("radio", { name: "暂不推进", exact: true })).toBeDisabled();
  await expect(form.getByRole("textbox", { name: "新增岗位跟进备注", exact: true })).toBeDisabled();
  await expect(form.getByRole("button", { name: "记录跟进", exact: true })).toBeDisabled();
  expect(mock.calls.filter((call) => call.method === "POST" && call.path.endsWith("/recommendations"))).toHaveLength(0);
  expect(mock.state.recommendations.find((item) => item.id === "rec-closed").events).toHaveLength(1);
  expect(mock.errors).toEqual([]);
});

test("面试详情分别呈现初复试、多位评价及待评价，原有操作仍可到达", async ({ page, mock }, testInfo) => {
  await page.setViewportSize({ width: 1446, height: 1220 });
  const dialog = await openApplication(page);
  await expect(dialog.getByRole("tab", { name: /^面试/ })).toHaveAttribute("aria-selected", "true");
  const first = dialog.getByTestId("recruitment-interview-interview-first");
  const second = dialog.getByTestId("recruitment-interview-interview-second");
  await expect(first).toHaveAttribute("aria-label", "初试面试安排与评价");
  await expect(second).toHaveAttribute("aria-label", "复试面试安排与评价");
  await expect(first).toContainText("苏宁");
  await expect(first).toContainText("未评价");
  await expect(first).toContainText("manager@example.invalid");
  await expect(first).toContainText("团队管理经验还需进一步了解");
  await expect(second).toContainText("未评价");
  await expect(second).toContainText("杭州 · 合成星野科技");
  await expect(dialog.getByRole("button", { name: "安排面试", exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "预览并发送邮件", exact: true })).toBeVisible();
  await dialog.getByRole("tab", { name: "简历", exact: true }).click();
  await expect(dialog.getByText("合成星野科技有限公司 · 高级产品经理", { exact: true })).toBeVisible();
  await dialog.getByRole("tab", { name: /^面试/ }).click();
  await page.screenshot({ path: testInfo.outputPath("interview-detail-desktop.png"), fullPage: true, animations: "disabled" });
  expect(mock.calls.filter((call) => call.method === "POST" && call.path === `/api/recruitment/applications/${applicationID}/actions`)).toHaveLength(0);
  expect(mock.errors).toEqual([]);
});

test("抄送成员可阅读面试和评价，没有排期推进发信等操作", async ({ page, mock }) => {
  mock.state.user.email = "cc@example.invalid";
  mock.state.user.role = "user";
  mock.state.applications[0].permissions = { can_manage: false, can_assign: false, can_view_resume: false, can_advance: false, can_schedule: false, can_feedback: false, can_approve: false };
  const dialog = await openApplication(page);
  await expect(dialog.getByTestId("recruitment-interview-interview-first")).toContainText("未评价");
  for (const name of ["安排面试", "推进下一步", "提交审核", "调整节点人员", "结束招聘", "预览并发送邮件", "写面试评价", "发送通知", "转交面试官"]) await expect(dialog.getByRole("button", { name, exact: true })).toHaveCount(0);
  await dialog.getByRole("tab", { name: "简历", exact: true }).click();
  await expect(dialog.getByText("合成星野科技有限公司 · 高级产品经理", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("link", { name: "查看完整简历", exact: true })).toHaveCount(0);
  expect(mock.calls.filter((call) => call.method === "POST" && call.path.endsWith("/actions"))).toHaveLength(0);
  expect(mock.calls.filter((call) => call.path.endsWith("/mail-history"))).toHaveLength(0);
  expect(mock.errors).toEqual([]);
});

test("手机详情单栏无左右溢出，真实滚轮能阅读资料与备注历史", async ({ page, mock }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  mock.state.notes = Array.from({ length: 30 }, (_, index) => ({ id: `long-note-${index}`, content: `合成长备注 ${index + 1}：${"已沟通岗位意向，等待下次跟进。".repeat(12)}`, author_name: "合成 HR", occurred_at: "2026-10-16T09:00:00+08:00" }));
  const dialog = await openCandidate(page);
  const main = dialog.getByTestId("resume-detail-main");
  const aside = dialog.getByTestId("resume-detail-aside");
  const [mainBox, asideBox] = await Promise.all([measuredBox(main), measuredBox(aside)]);
  expect(asideBox.y).toBeGreaterThanOrEqual(mainBox.y + mainBox.height);
  expect(Math.abs(asideBox.x - mainBox.x)).toBeLessThanOrEqual(2);
  for (const region of [dialog, main, aside]) await expectWithinViewport(region, 390);
  const candidateHeading = dialog.getByRole("heading", { name: candidateName, exact: true });
  await scrollWithWheel(page, main, candidateHeading, 600);
  const history = dialog.getByRole("region", { name: "历史备注", exact: true });
  await history.scrollIntoViewIfNeeded();
  await expect(history).toBeInViewport();
  const historyBox = await measuredBox(history);
  expect(historyBox.height).toBeLessThanOrEqual(321);
  const firstNote = history.getByText(/^合成长备注 1：/);
  await scrollWithWheel(page, history, firstNote);
  await dialog.getByRole("textbox", { name: "新增候选人备注", exact: true }).fill("合成手机备注：滚动后仍能保存。");
  await dialog.getByRole("button", { name: "加备注", exact: true }).click();
  await expect(dialog.getByTestId("resume-notes-section")).toContainText("合成手机备注：滚动后仍能保存。");
  expect(mock.state.notes).toHaveLength(31);
  expect(mock.state.recommendations[0].events).toHaveLength(3);
  await page.screenshot({ path: testInfo.outputPath("candidate-detail-mobile.png"), fullPage: true, animations: "disabled" });
  expect(mock.errors).toEqual([]);
});

test("手机面试详情保持单栏，真实滚轮能到达初试评价", async ({ page, mock }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const dialog = await openApplication(page);
  const main = dialog.getByTestId("recruitment-detail-main");
  const aside = dialog.getByTestId("recruitment-detail-aside");
  const [mainBox, asideBox] = await Promise.all([measuredBox(main), measuredBox(aside)]);
  expect(asideBox.y).toBeGreaterThanOrEqual(mainBox.y + mainBox.height);
  for (const region of [dialog, main, aside]) await expectWithinViewport(region, 390);
  const second = dialog.getByTestId("recruitment-interview-interview-second");
  await scrollWithWheel(page, main, second, 600);
  const first = dialog.getByTestId("recruitment-interview-interview-first");
  await first.scrollIntoViewIfNeeded();
  await expect(first).toBeInViewport();
  await expect(first).toContainText("团队管理经验还需进一步了解");
  await page.screenshot({ path: testInfo.outputPath("interview-detail-mobile.png"), fullPage: true, animations: "disabled" });
  expect(mock.errors).toEqual([]);
});
