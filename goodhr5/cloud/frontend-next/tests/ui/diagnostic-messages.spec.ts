/** 文件作用：用合成日志验证桌面与手机错误去重、不同原因保留，以及完整原始诊断仍可展开查看。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const reason = "候选人列表读取超时，请重新开始任务";
const message = `task_id=synthetic-task flow=platform step=find_candidates status=failed duration_ms=43051 error="${reason}"`;
const escapedReason = '候选人列表读取超时："resume"\n请重新开始任务';
const escapedMessage = `task_id=synthetic-escaped-task flow=platform step=find_candidates status=failed duration_ms=43051 error=${JSON.stringify(escapedReason)}`;
const token = "c".repeat(64);
const createdAt = "2026-11-26T08:00:00Z";
const event = {
  timestamp: createdAt, category: "错误", is_error: true, error_message: reason, message,
  task_id: "synthetic-task", action: "platform", step: "find_candidates", status: "failed", duration_ms: 43051,
  input: { flow: "platform", step: "find_candidates", original_parameter: "synthetic-input-preserved" },
  output: { error_code: "WORKER_ACTION_TIMEOUT", request_id: "SYNTHETIC_REQ_001", original_result: "synthetic-output-preserved" },
  original_stack: "synthetic-original-stack-preserved",
};
const otherEvent = { ...event, category: "运行", message: "平台任务已停止，请确认页面", error_message: "云端状态保存失败", task_id: "synthetic-other-task", output: { error_code: "SAVE_STATE_FAILED" } };
const escapedEvent = { ...event, message: escapedMessage, error_message: escapedReason, task_id: "synthetic-escaped-task", output: { ...event.output, request_id: "SYNTHETIC_REQ_002" } };
const logs = [event, escapedEvent, otherEvent].map((item, index) => ({ id: `synthetic-log-${index}`, level: "error", category: item.category, account: "uniform@example.invalid", position_name: "合成岗位", position_id: "synthetic-position", created_at: createdAt, message: JSON.stringify(item), event: item }));
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
    await expect(row.getByText(reason, { exact: true })).toBeVisible();
    const escapedRow = page.locator("tr[data-row-key='synthetic-log-1']");
    await expect(escapedRow.getByText(escapedReason, { exact: true })).toBeVisible();
    await expect(page.getByText(message, { exact: false })).toHaveCount(0);
    await expect(page.getByText(escapedMessage, { exact: false })).toHaveCount(0);
    mkdirSync(output, { recursive: true });
    await row.getByText(reason, { exact: true }).scrollIntoViewIfNeeded();
    await page.screenshot({ path: resolve(output, `admin-collapsed-${width}.png`), fullPage: true });
    await row.getByRole("button", { name: /展开/ }).click();
    await expect(row.getByText("详情已展开", { exact: true })).toBeVisible();
    await expect(row.getByText(reason, { exact: true })).toHaveCount(0);
    const details = row.locator("xpath=following-sibling::tr[1]");
    await expect(details.getByText(`日志内容：${reason}`, { exact: true })).toBeVisible();
    await expect(details.locator(".ant-alert")).toHaveCount(0);
    await expect(details.getByText(message, { exact: false })).toHaveCount(0);
    await expect(details.getByText("synthetic-input-preserved", { exact: false })).toBeVisible();
    await expect(details.getByText("WORKER_ACTION_TIMEOUT", { exact: false })).toBeVisible();
    await details.getByText("查看原始日志", { exact: true }).click();
    await expect(details.locator("pre").last()).toContainText("synthetic-original-stack-preserved");
    await expect(details.locator("pre").last()).toContainText(reason);
    await expect(details.locator("pre").last()).toContainText("SYNTHETIC_REQ_001");
    expect(JSON.parse((await details.locator("pre").last().textContent()) || "{}").message).toBe(message);
    await details.getByText("查看原始日志", { exact: true }).click();
    await escapedRow.getByRole("button", { name: /展开/ }).click();
    const escapedDetails = escapedRow.locator("xpath=following-sibling::tr[1]");
    await expect(escapedDetails.getByText(`日志内容：${escapedReason}`, { exact: true })).toBeVisible();
    await expect(escapedDetails.locator(".ant-alert")).toHaveCount(0);
    await expect(page.getByText(escapedMessage, { exact: false })).toHaveCount(0);
    await escapedDetails.getByText("查看原始日志", { exact: true }).click();
    expect(JSON.parse((await escapedDetails.locator("pre").last().textContent()) || "{}").message).toBe(escapedMessage);
    await escapedDetails.getByText("查看原始日志", { exact: true }).click();
    const other = page.locator("tr[data-row-key='synthetic-log-2']");
    await expect(other.getByText("运行", { exact: true })).toBeVisible();
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
    await expect(page.getByText(reason, { exact: true })).toBeVisible();
    await expect(page.getByText(escapedReason, { exact: true })).toBeVisible();
    await expect(page.getByText(message, { exact: false })).toHaveCount(0);
    await expect(page.getByText(escapedMessage, { exact: false })).toHaveCount(0);
    await expect(page.getByText("最近一条错误：", { exact: false })).toHaveCount(0);
    await expect(page.getByText(reason, { exact: true })).toHaveCount(1);
    await expect(page.getByText("平台任务已停止，请确认页面", { exact: true })).toBeVisible();
    await expect(page.getByText("云端状态保存失败", { exact: true })).toBeVisible();
    await expect(page.getByText(legacyBody, { exact: true })).toBeVisible();
    await expect(page.getByText(whitespaceBody, { exact: true })).toBeVisible();
    mkdirSync(output, { recursive: true });
    await page.screenshot({ path: resolve(output, `share-default-${width}.png`), fullPage: true });
    const card = page.locator(".public-diagnostic-panel").filter({ has: page.getByText(reason, { exact: true }) });
    await card.getByText("查看入参、出参和任务编号", { exact: true }).click();
    await expect(card.getByText("synthetic-input-preserved", { exact: false }).first()).toBeVisible();
    await expect(card.getByText("WORKER_ACTION_TIMEOUT", { exact: false }).first()).toBeVisible();
    await expect(card.getByText("synthetic-original-stack-preserved", { exact: false })).toBeVisible();
    expect(JSON.parse((await card.locator("pre").last().textContent()) || "{}").message).toBe(message);
    await card.getByText("查看入参、出参和任务编号", { exact: true }).click();
    const escapedCard = page.locator(".public-diagnostic-panel").filter({ has: page.getByText(escapedReason, { exact: true }) });
    await expect(escapedCard.locator(".ant-alert")).toHaveCount(0);
    await escapedCard.getByText("查看入参、出参和任务编号", { exact: true }).click();
    expect(JSON.parse((await escapedCard.locator("pre").last().textContent()) || "{}").message).toBe(escapedMessage);
    await escapedCard.getByText("查看入参、出参和任务编号", { exact: true }).click();
    await page.getByRole("textbox", { name: "搜索错误、步骤或请求编号" }).fill("SYNTHETIC_REQ_001");
    await expect(page.getByText(reason, { exact: true })).toBeVisible();
    await expect(page.getByText(message, { exact: false })).not.toBeVisible();
    await expect(page.getByText("云端状态保存失败", { exact: true })).toHaveCount(0);
    mkdirSync(output, { recursive: true });
    await page.screenshot({ path: resolve(output, `share-${width}.png`), fullPage: true });
    expect(state.errors).toEqual([]);
  });
}
