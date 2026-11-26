/** 文件作用：用合成日志验证桌面与手机错误去重、不同原因保留，以及完整原始诊断仍可展开查看。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const reason = "候选人列表读取超时，请重新开始任务";
const message = `读取候选人列表失败：${reason}`;
const token = "c".repeat(64);
const createdAt = "2026-11-26T08:00:00Z";
const event = {
  timestamp: createdAt, category: "错误", is_error: true, error_message: reason, message,
  task_id: "synthetic-task", action: "platform", step: "find_candidates", status: "failed", duration_ms: 43051,
  input: { flow: "platform", step: "find_candidates", original_parameter: "synthetic-input-preserved" },
  output: { error_code: "WORKER_ACTION_TIMEOUT", request_id: "SYNTHETIC_REQ_001", original_result: "synthetic-output-preserved" },
  original_stack: "synthetic-original-stack-preserved",
};
const otherEvent = { ...event, message: "平台任务已停止，请确认页面", error_message: "云端状态保存失败", task_id: "synthetic-other-task", output: { error_code: "SAVE_STATE_FAILED" } };
const logs = [event, otherEvent].map((item, index) => ({ id: `synthetic-log-${index}`, level: "error", category: "错误", account: "uniform@example.invalid", position_name: "合成岗位", position_id: "synthetic-position", created_at: createdAt, message: JSON.stringify(item), event: item }));
const legacyBody = "旧事件的完整正文保留在外层日志字段";
const whitespaceBody = "空白事件正文继续回退外层完整说明";
const sharedLogs = [...logs,
  { id: "legacy-structured-log", level: "info", message: legacyBody, created_at: createdAt, event: { action: "platform", step: "find_candidates", input: { legacy: true }, output: { legacy_context: "kept" } } },
  { id: "legacy-whitespace-log", level: "info", message: whitespaceBody, created_at: createdAt, event: { message: " \n ", input: {}, output: {} } },
];
const output = resolve(process.env.UI_DIAGNOSTIC_OUTPUT || "../../../output/playwright/error-messages-20261126");

test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "uniform-synthetic-token" }] }] } });

/** mockLogs 拦截日志接口并复用现有全模拟登录环境，不访问真实账号、招聘页或本地程序。 */
async function mockLogs(page: Page) {
  const state = await mockUniformAdmin(page);
  await page.route("**/api/admin/logs?**", route => route.fulfill({ json: { logs, total: logs.length } }));
  await page.route("**/api/public/position-log-shares/*", route => route.fulfill({ json: { ok: true, logs: sharedLogs, expires_at: "2026-12-03T08:00:00Z" } }));
  return state;
}

for (const width of [1366, 390]) {
  test(`管理员日志${width}px只展示一份原因，原始数据可查`, async ({ page }) => {
    const state = await mockLogs(page);
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/admin/logs");
    const row = page.locator("tr[data-row-key='synthetic-log-0']");
    await expect(row.getByText(message, { exact: true })).toBeVisible();
    await row.getByRole("button", { name: /展开/ }).click();
    await expect(row.getByText("详情已展开", { exact: true })).toBeVisible();
    await expect(row.getByText(message, { exact: true })).toHaveCount(0);
    const details = row.locator("xpath=following-sibling::tr[1]");
    await expect(details.getByText(`日志内容：${message}`, { exact: true })).toBeVisible();
    await expect(details.locator(".ant-alert")).toHaveCount(0);
    await expect(details.getByText("synthetic-input-preserved", { exact: false })).toBeVisible();
    await expect(details.getByText("WORKER_ACTION_TIMEOUT", { exact: false })).toBeVisible();
    await details.getByText("查看原始日志", { exact: true }).click();
    await expect(details.locator("pre").last()).toContainText("synthetic-original-stack-preserved");
    await expect(details.locator("pre").last()).toContainText(reason);
    await expect(details.locator("pre").last()).toContainText("SYNTHETIC_REQ_001");
    await details.getByText("查看原始日志", { exact: true }).click();
    const other = page.locator("tr[data-row-key='synthetic-log-1']");
    await other.getByRole("button", { name: /展开/ }).click();
    const otherDetails = other.locator("xpath=following-sibling::tr[1]");
    await expect(otherDetails.getByText("日志内容：云端状态保存失败", { exact: true })).toBeVisible();
    await expect(otherDetails.getByText("平台任务已停止，请确认页面", { exact: true })).toBeVisible();
    mkdirSync(output, { recursive: true });
    await page.screenshot({ path: resolve(output, `admin-${width}.png`), fullPage: true });
    expect(state.errors).toEqual([]);
  });

  test(`诊断分享${width}px不重复错误，独立原因与原始记录保留`, async ({ page }) => {
    const state = await mockLogs(page);
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`/diagnostics/${token}`);
    await expect(page.getByText(message, { exact: true })).toBeVisible();
    await expect(page.getByText("最近一条错误：", { exact: false })).toHaveCount(0);
    await expect(page.getByText(reason, { exact: true })).toHaveCount(0);
    await expect(page.getByText("平台任务已停止，请确认页面", { exact: true })).toBeVisible();
    await expect(page.getByText("云端状态保存失败", { exact: true })).toBeVisible();
    await expect(page.getByText(legacyBody, { exact: true })).toBeVisible();
    await expect(page.getByText(whitespaceBody, { exact: true })).toBeVisible();
    const card = page.locator(".public-diagnostic-panel").filter({ has: page.getByText(message, { exact: true }) });
    await card.getByText("查看入参、出参和任务编号", { exact: true }).click();
    await expect(card.getByText("synthetic-input-preserved", { exact: false }).first()).toBeVisible();
    await expect(card.getByText("WORKER_ACTION_TIMEOUT", { exact: false }).first()).toBeVisible();
    await expect(card.getByText("synthetic-original-stack-preserved", { exact: false })).toBeVisible();
    await card.getByText("查看入参、出参和任务编号", { exact: true }).click();
    await page.getByRole("textbox", { name: "搜索错误、步骤或请求编号" }).fill("SYNTHETIC_REQ_001");
    await expect(page.getByText(message, { exact: true })).toBeVisible();
    await expect(page.getByText("云端状态保存失败", { exact: true })).toHaveCount(0);
    mkdirSync(output, { recursive: true });
    await page.screenshot({ path: resolve(output, `share-${width}.png`), fullPage: true });
    expect(state.errors).toEqual([]);
  });
}
