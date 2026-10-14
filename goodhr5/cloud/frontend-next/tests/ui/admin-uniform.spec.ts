/** 文件作用：在全模拟接口下验证后台控件统一尺寸、工具栏留白、右端新增和独立/内置分页，保存三个宽度的截图及边界报告。 */
import { test, expect, type Locator, type Page } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { expectUniformNetwork, mockUniformAdmin, uniformLongPosition, uniformOrigin, type UniformState } from "./admin-uniform-fixture";

const output = resolve("../../../output/admin-uniform-20261015");
const keyPages = ["positions", "recommendations", "users", "logs", "system-config", "recruitment", "resumes"];
const menus = ["", "positions", "resumes", "recommendations", "recruitment", "accounts", "team", "team-stats", "subscription", "personal-config", "invitations", "local-data", "agent-download", "help", "users", "activation-codes", "payment-records", "ai-records", "logs", "mail", "system-config", "recruitment/settings"];
const minimumPadding = /^(?:1[2-9]|[2-9]\d|\d{3,})(?:\.\d+)?px$/;
type Measurement = { route: string; viewport: number; state: string; controls: Array<{ tag: string; width: number; height: number }>; pagination?: { width: number; height: number } };

test.use({ actionTimeout: 12_000, storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "uniform-synthetic-token" }] }] } });
test.setTimeout(480_000);

/** visibleBox 确认元素可见并读取标准Locator边界，不注入页面脚本。 */
async function visibleBox(locator: Locator) {
  await expect(locator).toBeVisible();
  const box = await locator.boundingBox();
  expect(box).not.toBeNull();
  return box!;
}

/** regularControls 选择单行常规控件，排除多行文本、内部输入及紧凑表格动作。 */
function regularControls(root: Locator) {
  return root.locator([
    ".ant-input-affix-wrapper:not(.ant-input-affix-wrapper-sm):not(.ant-input-affix-wrapper-lg)",
    "input.ant-input:not(.ant-input-sm):not(.ant-input-lg):not(.ant-input-affix-wrapper input):not(.ant-picker input)",
    ".ant-select:not(.ant-select-sm):not(.ant-select-lg):not(.ant-select-multiple)",
    ".ant-input-number:not(.ant-input-number-sm):not(.ant-input-number-lg)",
    ".ant-picker:not(.ant-picker-small):not(.ant-picker-large)",
    ".ant-btn:not(.ant-btn-sm):not(.ant-btn-lg):not(.ant-btn-text):not(.ant-btn-link):not(.ant-table .ant-btn)",
    ".ant-radio-button-wrapper:not(.ant-radio-group-small .ant-radio-button-wrapper):not(.ant-radio-group-large .ant-radio-button-wrapper)",
  ].join(","));
}

/** checkControls 校验默认单行控件32px和13px字体，并保存实际边界。 */
async function checkControls(root: Locator, report: Measurement) {
  const controls = regularControls(root);
  for (let index = 0; index < await controls.count(); index++) {
    const control = controls.nth(index);
    if (!await control.isVisible()) continue;
    const box = await visibleBox(control);
    expect(box.height, `常规控件${index}应为32px`).toBeGreaterThanOrEqual(31);
    expect(box.height, `常规控件${index}应为32px`).toBeLessThanOrEqual(33);
    await expect(control).toHaveCSS("font-size", "13px");
    report.controls.push({ tag: await control.getAttribute("class") || "", width: box.width, height: box.height });
  }
}

/** checkToolbar 检查工具栏与内容间距、输入组间距以及新增入口在最右端。 */
async function checkToolbar(page: Page, route: string) {
  const toolbar = route === "resumes" ? page.getByTestId("resume-filters") : page.getByTestId(`${route}-toolbar`);
  if (!await toolbar.count()) return;
  const box = await visibleBox(toolbar);
  await expect(toolbar).toHaveCSS("column-gap", "8px");
  if (route === "recruitment") {
    const tab = await visibleBox(toolbar.locator(".ant-tabs-tab").filter({ hasText: "岗位招聘进度" }));
    expect(tab.height).toBeGreaterThanOrEqual(31);
    expect(tab.height).toBeLessThanOrEqual(33);
  }
  const content = route === "positions" ? page.locator(".goodhr-admin-content .ant-card").first() : route === "resumes" ? page.getByTestId("resume-folder-bar") : page.locator(".goodhr-admin-content .ant-table-wrapper").first();
  if (await content.count() && await content.isVisible()) {
    const contentBox = await visibleBox(content);
    expect(contentBox.y - box.y - box.height, "工具栏下方应有至少10px留白").toBeGreaterThanOrEqual(9.5);
  }
  if (route === "positions") {
    const create = toolbar.getByRole("button", { name: "新建岗位", exact: true });
    const createBox = await visibleBox(create);
    expect(Math.abs(box.x + box.width - createBox.x - createBox.width), "新建岗位在工具栏右端").toBeLessThanOrEqual(2);
  }
  if ((page.viewportSize()?.width || 0) < 600) {
    const createName = ({ positions: "新建岗位", recommendations: "新增推荐", recruitment: "开启招聘", resumes: "添加" } as Record<string, string>)[route];
    if (createName) {
      const create = toolbar.getByRole("button", { name: createName, exact: true });
      const createBox = await visibleBox(create);
      expect(createBox.x, "手机新增入口应直接处于可见范围").toBeGreaterThanOrEqual(0);
      expect(createBox.x + createBox.width, "手机新增入口无需横向滚动").toBeLessThanOrEqual(page.viewportSize()!.width);
      await create.click({ trial: true });
    }
  }
}

/** checkPagination 同时覆盖独立分页与Table内置分页，验证留白和页码/page-size统一32px。 */
async function checkPagination(page: Page, report: Measurement) {
  const pagination = page.locator(".goodhr-admin-content .ant-pagination").first();
  if (!await pagination.count() || !await pagination.isVisible()) return;
  await pagination.scrollIntoViewIfNeeded();
  const wrapper = page.locator(".goodhr-table-pagination").filter({ has: page.locator(".ant-pagination") });
  const independent = await wrapper.count() > 0;
  const padded = independent ? wrapper.first() : pagination;
  await expect(padded).toHaveCSS(independent ? "padding-top" : "margin-top", minimumPadding);
  await expect(padded).toHaveCSS(independent ? "padding-bottom" : "margin-bottom", minimumPadding);
  await expect(padded).toHaveCSS("padding-left", minimumPadding);
  await expect(padded).toHaveCSS("padding-right", minimumPadding);
  const item = pagination.locator(".ant-pagination-item").first();
  if (await item.count()) {
    const itemBox = await visibleBox(item);
    expect(itemBox.height).toBeGreaterThanOrEqual(31);
    expect(itemBox.height).toBeLessThanOrEqual(33);
  }
  const size = pagination.locator(".ant-select").first();
  // Ant Design在窄屏自动隐藏每页数量选择，仍检查可见页码的32px及完整分页留白。
  if (await size.count() && await size.isVisible()) {
    const sizeBox = await visibleBox(size);
    expect(sizeBox.height).toBeGreaterThanOrEqual(31);
    expect(sizeBox.height).toBeLessThanOrEqual(33);
  }
  const paginationBox = await visibleBox(padded);
  report.pagination = { width: paginationBox.width, height: paginationBox.height };
}

/** captureUniform 将只含合成资料的稳定截图和尺寸记录写入本轮独立输出目录。 */
async function captureUniform(page: Page, name: string, reports: Measurement[]) {
  mkdirSync(output, { recursive: true });
  await page.screenshot({ path: resolve(output, `${name}.png`), fullPage: true, animations: "disabled" });
  writeFileSync(resolve(output, `${name}.json`), JSON.stringify(reports, null, 2));
}

/** openUniform 等待后台和本页数据加载完成，检查所有普通独立刷新入口已移除。 */
async function openUniform(page: Page, route: string, state: UniformState) {
  await page.goto(`/admin/${route}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator(".goodhr-admin-content")).toBeVisible();
  await expect(page.locator(".goodhr-admin-content .ant-spin-spinning")).toHaveCount(0);
  const header = page.locator(".goodhr-admin-content .admin-page-header").first();
  if (await header.count()) await expect(header).toHaveCSS("margin-bottom", "12px");
  await expect(page.getByRole("main").getByRole("button", { name: /^(?:reload\s*)?刷新(?:中|状态|进度|预览)?$/ })).toHaveCount(0);
  expectUniformNetwork(state);
}

for (const width of [1366, 1920, 390]) {
  for (const empty of [false, true]) {
    test(`后台统一控件 ${width}px ${empty ? "空数据" : "有数据"}与分页边界`, async ({ page }) => {
      const state = await mockUniformAdmin(page, empty);
      await page.setViewportSize({ width, height: 960 });
      const reports: Measurement[] = [];
      for (const route of keyPages) {
        await openUniform(page, route, state);
        const report: Measurement = { route, viewport: width, state: empty ? "empty" : "populated", controls: [] };
        await checkControls(page.locator(".goodhr-admin-content"), report);
        await checkToolbar(page, route);
        if (route === "resumes") {
          const chip = page.getByTestId("resume-folder-bar").getByRole("button", { name: "全部", exact: true });
          expect((await visibleBox(chip)).height, "收藏夹小按钮保留25px").toBe(25);
          const action = page.locator(".ant-table-tbody").getByRole("button", { name: "备注", exact: true }).first();
          if (await action.count()) {
            const actionBox = await visibleBox(action);
            expect(actionBox.height).toBeGreaterThanOrEqual(22);
            expect(actionBox.height).toBeLessThanOrEqual(28);
          }
        }
        reports.push(report);
        await captureUniform(page, `${route}-${width}-${report.state}-top`, reports);
        await checkPagination(page, report);
        if (!empty && ["recommendations", "users", "logs", "recruitment", "resumes"].includes(route)) {
          const second = page.locator(".goodhr-admin-content .ant-pagination-item-2").first();
          await second.click();
          await expect(second).toHaveClass(/ant-pagination-item-active/);
          if (route !== "recruitment") {
            const endpoint = route === "recommendations" ? "/api/candidate-recommendations" : route === "resumes" ? "/api/candidates" : `/api/admin/${route}`;
            await expect.poll(() => state.calls.some((call) => call.path === endpoint && new URLSearchParams(call.query).get("page") === "2")).toBe(true);
          }
          await checkPagination(page, report);
          await captureUniform(page, `${route}-${width}-page-2`, reports);
        }
        expectUniformNetwork(state);
      }
      writeFileSync(resolve(output, `matrix-${width}-${empty ? "empty" : "populated"}.json`), JSON.stringify({ reports, calls: state.calls, unknown: state.unknown, writes: state.writes, errors: state.errors }, null, 2));
    });
  }
}

for (const width of [1366, 390]) {
  test(`消息中心 ${width}px 浮层完整可见、第二页32px、正文13辅助12`, async ({ page }) => {
    const state = await mockUniformAdmin(page, false, true);
    await page.setViewportSize({ width, height: 960 });
    await openUniform(page, "positions", state);
    await page.getByRole("button", { name: "消息中心", exact: true }).click();
    const panel = page.getByTestId("message-center-panel");
    const popover = page.locator(".ant-popover").filter({ has: page.getByTestId("message-center-panel") });
    await expect(panel.getByText("合成消息1", { exact: true })).toBeVisible();
    await expect(popover).toHaveCSS("opacity", "1");
    await expect(popover).toHaveCSS("transform", "none");
    for (const element of [panel, popover]) {
      const box = await visibleBox(element);
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(width);
      expect(box.y).toBeGreaterThanOrEqual(0);
      expect(box.y + box.height).toBeLessThanOrEqual(960);
    }
    await expect(panel).toHaveCSS("font-size", "13px");
    await expect(panel.getByRole("button", { name: /刷新/ })).toHaveCount(0);
    const pagination = panel.locator(".goodhr-table-pagination");
    for (const side of ["top", "bottom", "left", "right"]) await expect(pagination).toHaveCSS(`padding-${side}`, "12px");
    const second = panel.locator(".ant-pagination-item-2");
    expect((await visibleBox(second)).height).toBe(32);
    await second.click();
    await expect(second).toHaveClass(/ant-pagination-item-active/);
    await expect(panel.getByText("合成消息21", { exact: true })).toBeVisible();
    await expect.poll(() => state.calls.some((call) => call.path === "/api/messages" && new URLSearchParams(call.query).get("page") === "2")).toBe(true);
    await panel.getByRole("button", { name: "合成消息21", exact: true }).click();
    const body = panel.getByText("只含合成内容的消息正文21", { exact: true });
    await expect(body).toBeVisible();
    await expect(body).toHaveCSS("font-size", "13px");
    await expect(panel.getByText(/已读消息 3 天后清理/)).toHaveCSS("font-size", "12px");
    await expect(panel.getByText("消息中心 · 60 条未读", { exact: true })).toBeVisible();
    expect(state.readMessages).toEqual(["uniform-message-21"]);
    await captureUniform(page, `message-center-${width}-page-2`, []);
    expectUniformNetwork(state);
  });
}

test("后台所有菜单的普通刷新均移除，常规控件和输入按钮间距统一", async ({ page }) => {
  const state = await mockUniformAdmin(page);
  await page.setViewportSize({ width: 1366, height: 960 });
  const reports: Measurement[] = [];
  for (const route of menus) {
    await openUniform(page, route, state);
    if (route === "personal-config") {
      await expect(page.getByLabel("API 地址", { exact: true })).toHaveValue(`${uniformOrigin}/api/builtin-ai/v1/chat/completions`);
      await expect(page.locator(".ant-select:has(#personal-config-field-2)")).toHaveText(/合成测试模型.*synthetic-model/);
      await expect(page.getByLabel("模型名称", { exact: true })).toBeEnabled();
      await expect(page.getByRole("main").getByText(/个人配置暂时没读完整|内置模型列表暂时没读到/)).toHaveCount(0);
    }
    const report: Measurement = { route: route || "dashboard", viewport: 1366, state: "all-menus", controls: [] };
    await checkControls(page.locator(".goodhr-admin-content"), report);
    reports.push(report);
    await captureUniform(page, `menu-${route.replaceAll("/", "-") || "dashboard"}`, reports);
    if (route === "personal-config") {
      await page.setViewportSize({ width: 390, height: 960 });
      const mobile: Measurement = { route, viewport: 390, state: "normal-ai-config", controls: [] };
      await checkControls(page.locator(".goodhr-admin-content"), mobile);
      await captureUniform(page, "menu-personal-config-390-normal", [mobile]);
      await page.setViewportSize({ width: 1366, height: 960 });
    }
    expectUniformNetwork(state);
  }
  writeFileSync(resolve(output, "all-menus.json"), JSON.stringify({ reports, calls: state.calls, unknown: state.unknown, writes: state.writes, errors: state.errors }, null, 2));
});

test("长岗位选项保持搜索选择，第二页与新增弹框常规32px一致", async ({ page }) => {
  const state = await mockUniformAdmin(page);
  await page.setViewportSize({ width: 1366, height: 960 });
  await openUniform(page, "recruitment", state);
  await page.getByLabel("筛选招聘岗位", { exact: true }).click();
  const option = page.locator(".ant-select-item-option").filter({ hasText: uniformLongPosition });
  await expect(option).toBeVisible();
  await option.click();
  await expect.poll(() => state.calls.some((call) => call.path === "/api/recruitment/applications" && new URLSearchParams(call.query).get("position_id") === "uniform-position-1")).toBe(true);
  await captureUniform(page, "long-position-selected-1366", []);
  await openUniform(page, "positions", state);
  await page.getByRole("button", { name: "新建岗位", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "新建岗位模板", exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog).toHaveCSS("transform", "none");
  const report: Measurement = { route: "positions-create-dialog", viewport: 1366, state: "long-options", controls: [] };
  await checkControls(dialog, report);
  await captureUniform(page, "positions-create-dialog-1366", [report]);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  expectUniformNetwork(state);
});

test.describe("公开页面与后台尺寸隔离", () => {
  test.use({ storageState: { cookies: [], origins: [] } });
  test("标准登录进入32px后台，退出回登录时恢复38px和14px", async ({ page }) => {
    const state = await mockUniformAdmin(page);
    await page.setViewportSize({ width: 1366, height: 960 });
    await page.goto("/login?next=/admin/positions");
    const email = page.locator(".public-login-form .ant-input-affix-wrapper").first();
    const mode = page.locator(".public-login-tabs").getByRole("button", { name: "验证码登录", exact: true });
    expect((await visibleBox(email)).height).toBe(38);
    expect((await visibleBox(mode)).height).toBe(38);
    await expect(mode).toHaveCSS("font-size", "14px");
    await page.locator(".public-login-tabs").getByRole("button", { name: "密码登录", exact: true }).click();
    await page.getByLabel("邮箱", { exact: true }).fill("uniform@example.invalid");
    await page.getByLabel("密码", { exact: true }).fill("synthetic-password-only");
    await page.locator(".public-login-form button[type='submit']").click();
    await expect(page).toHaveURL(/\/admin\/positions$/);
    const create = page.getByRole("button", { name: "新建岗位", exact: true });
    expect((await visibleBox(create)).height).toBe(32);
    await expect(create).toHaveCSS("font-size", "13px");
    await captureUniform(page, "route-login-to-admin-1366", []);
    await page.getByRole("button", { name: "退出登录", exact: true }).click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator(".public-login-tabs").getByRole("button", { name: "验证码登录", exact: true })).toHaveCSS("font-size", "14px");
    expect((await visibleBox(page.locator(".public-login-form .ant-input-affix-wrapper").first())).height).toBe(38);
    await captureUniform(page, "route-admin-to-login-1366", []);
    expectUniformNetwork(state);
  });
});

for (const width of [1366, 1920, 390]) {
  test(`团队统计 ${width}px 自定义日期32px、查询底线对齐且不溢出`, async ({ page }) => {
    const state = await mockUniformAdmin(page);
    await page.setViewportSize({ width, height: 960 });
    await openUniform(page, "team-stats", state);
    await page.getByLabel("时间周期", { exact: true }).click();
    await page.locator(".ant-select-item-option").filter({ hasText: "自定义" }).click();
    const select = page.locator(".ant-select:has(#goodhr-team-stats-field-1)");
    const start = page.locator(".ant-picker:has(#goodhr-team-stats-field-2)");
    const end = page.locator(".ant-picker:has(#goodhr-team-stats-field-3)");
    const query = page.getByRole("main").getByRole("button", { name: "查询", exact: true });
    const boxes = await Promise.all([select, start, end, query].map(visibleBox));
    for (const box of boxes) {
      // 13px字体的日期框会产生0.5px行高，额外验证32px主题变量，不能接受其他常规尺寸。
      expect(box.height).toBeGreaterThanOrEqual(31.5);
      expect(box.height).toBeLessThanOrEqual(32.5);
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(width);
    }
    for (const control of [select, start, end, query]) {
      await expect(control).toHaveCSS("--ant-control-height", "32px");
      await expect(control).toHaveCSS("font-size", "13px");
    }
    await expect(page.locator("[class*='layout2']").filter({ has: page.locator("#goodhr-team-stats-field-1") })).toHaveCSS("column-gap", "8px");
    if (width >= 900) {
      for (const box of boxes.slice(0, 3)) expect(Math.abs(box.y + box.height - boxes[3].y - boxes[3].height)).toBeLessThan(1);
    } else {
      for (let index = 1; index < boxes.length; index++) expect(boxes[index].y).toBeGreaterThan(boxes[index - 1].y + boxes[index - 1].height);
    }
    await query.click();
    await expect.poll(() => state.calls.some((call) => call.path === "/api/team/stats" && new URLSearchParams(call.query).get("period") === "custom")).toBe(true);
    await captureUniform(page, `team-stats-${width}-custom`, []);
    expectUniformNetwork(state);
  });

  test(`最后间距修复 ${width}px 简历用户日志输入组8px与分页32px`, async ({ page }) => {
    const state = await mockUniformAdmin(page);
    await page.setViewportSize({ width, height: 960 });
    const reports: Measurement[] = [];
    for (const route of ["resumes", "users", "logs"]) {
      await openUniform(page, route, state);
      const report: Measurement = { route, viewport: width, state: "final-spacing", controls: [] };
      await checkControls(page.locator(".goodhr-admin-content"), report);
      await checkToolbar(page, route);
      const group = route === "resumes" ? page.getByTestId("resume-filters").locator("[class*='searchControls']") : route === "users" ? page.locator("[class*='searchBar']") : page.locator("[class*='layout19']");
      await expect(group).toHaveCSS("column-gap", "8px");
      if (width < 600) {
        const main = await visibleBox(page.locator(".goodhr-admin-content"));
        expect(main.x + main.width).toBeLessThanOrEqual(width);
      }
      reports.push(report);
      await captureUniform(page, `${route}-${width}-final-spacing-top`, reports);
      await checkPagination(page, report);
      expectUniformNetwork(state);
    }
  });
}
