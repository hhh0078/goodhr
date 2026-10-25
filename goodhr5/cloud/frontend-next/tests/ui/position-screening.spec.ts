/** 文件作用说明：以合成云端记录验证本人岗位两轮历史、真实空值、服务端分页筛选和桌面手机交互，不启动本地任务或 AI。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";
import type { ScreeningRecord, ScreeningRound } from "../../lib/position-screening";

const output = resolve("../../../output/position-screening-20261026");
const longReason = `${"有相关产品经历，具体范围仍以原资料为准。".repeat(14)}完整原因末尾`;
test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "screening-synthetic-token" }] }] } });

/** round 创建包含真实零值或明确空值的单轮合成记录。 */
function round(overrides: Partial<ScreeningRound> = {}): ScreeningRound { return { kind: "ai", status: "passed", accepted: true, score: 80, threshold: 60, reason: "合成相关经历", started_at: "2026-10-26T08:00:00Z", finished_at: "2026-10-26T08:00:01.250Z", duration_ms: 1250, ...overrides }; }

/** record 创建独立筛选尝试；按索引从最新到最早排列，未执行第二轮保持全部时间和分数为空。 */
function record(index: number, positionID: string): ScreeningRecord {
  const first = index === 1 ? round({ status: "rejected", accepted: false, score: 0, duration_ms: 0, reason: longReason }) : index === 2 ? round({ kind: "keyword", score: null }) : index === 3 ? round({ status: "failed", accepted: null, score: null, finished_at: null, duration_ms: null, reason: "调用中断，完成时间未知" }) : round();
  const second = index === 1 || index === 3 ? round({ kind: "none", status: "not_executed", accepted: null, score: null, threshold: null, reason: "第一轮没有通过，未进入详情评分", started_at: null, finished_at: null, duration_ms: null }) : round({ score: 72, duration_ms: 1 });
  return { id: `${positionID}-record-${index}`, position_id: positionID, attempt_id: `attempt-${index}`, task_id: "synthetic-task", platform_id: "boss", platform_candidate_id: "", candidate_key: `key-${index}`, candidate_name: `${positionID === "position-one" ? "甲" : "乙"}合成人选${index}`, occurred_at: new Date(Date.parse("2026-10-26T10:00:00Z") - index * 60000).toISOString(), revision: 2, completed: true, termination_reason: "", first, second, created_at: "2026-10-26T10:00:00Z", updated_at: "2026-10-26T10:00:00Z" };
}

/** fixture 将本次岗位与历史接口完全拦截，失败和挂起响应只存在于测试内存。 */
async function fixture(page: Page) {
  const base = await mockUniformAdmin(page, true);
  const state = { queries: [] as { position: string; query: URLSearchParams }[], fail: false, holdNext: false, release: null as (() => void) | null };
  await page.route(/\/api\/positions(?:\/|\?|$)/, async (route) => {
    const url = new URL(route.request().url()); const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (url.pathname === "/api/positions") return route.fulfill({ headers, json: { positions: [{ id: "position-one", name: "合成甲岗位", creator_email: "uniform@example.invalid", is_current_user: true, is_open: true, status: "stopped", platform_id: "boss" }, { id: "position-two", name: "合成乙岗位", creator_email: "uniform@example.invalid", is_current_user: true, is_open: true, status: "running", platform_id: "boss" }, { id: "position-team", name: "其他同事岗位", creator_email: "other@example.invalid", is_current_user: false, is_open: true, status: "stopped", platform_id: "boss" }] } });
    const match = url.pathname.match(/^\/api\/positions\/([^/]+)\/screening-records$/);
    if (!match || route.request().method() !== "GET") { base.writes.push(route.request().method() + url.pathname); return route.abort(); }
    state.queries.push({ position: match[1], query: url.searchParams });
    if (state.holdNext) { state.holdNext = false; await new Promise<void>((resolve) => { state.release = resolve; }); }
    if (state.fail) return route.fulfill({ status: 403, headers, json: { error: "这个岗位暂时无权查看，请确认当前账号。" } });
    let records = Array.from({ length: 45 }, (_, index) => record(index + 1, match[1]));
    for (const key of ["first", "second"] as const) records = records.filter((item) => {
      const value = item[key]; const min = url.searchParams.get(`${key}_score_min`), max = url.searchParams.get(`${key}_score_max`), from = url.searchParams.get(`${key}_started_from`), to = url.searchParams.get(`${key}_started_to`);
      return (!min || value.score !== null && value.score >= Number(min)) && (!max || value.score !== null && value.score <= Number(max)) && (!from || value.started_at !== null && Date.parse(value.started_at) >= Date.parse(from)) && (!to || value.started_at !== null && Date.parse(value.started_at) < Date.parse(to));
    });
    const offset = (Number(url.searchParams.get("page")) - 1) * 20;
    return route.fulfill({ headers, json: { ok: true, records: records.slice(offset, offset + 20), total: records.length, page: Number(url.searchParams.get("page")), page_size: 20 } }).catch(() => undefined);
  });
  return { state, base };
}

/** openRecords 只通过指定岗位卡片的真实图标打开记录，不触碰其开始按钮。 */
async function openRecords(page: Page, id = "position-one") {
  await page.locator(`.ant-card[data-position-id="${id}"]`).getByRole("button", { name: "筛选记录", exact: true }).click();
  return page.getByRole("dialog", { name: "岗位筛选记录", exact: true });
}

test("开始停止旁图标仅本人可见，固定二十条和两轮空值真实零分耗时及完整原因可查看", async ({ page }) => {
  const { state, base } = await fixture(page); await page.goto("/admin/positions");
  await expect(page.getByRole("button", { name: "筛选记录", exact: true })).toHaveCount(2);
  const icon = page.locator('.ant-card[data-position-id="position-one"]').getByRole("button", { name: "筛选记录", exact: true }); await expect(icon).toHaveText(""); expect((await icon.boundingBox())?.width).toBe(28);
  const dialog = await openRecords(page); await expect(dialog.getByText("甲合成人选1", { exact: true })).toBeVisible();
  await expect(dialog.locator("tbody > tr[data-row-key]")).toHaveCount(20);
  const first = dialog.locator("tbody > tr[data-row-key]").first(); await expect(first).toContainText("甲合成人选1");
  await expect(first.getByRole("group", { name: "第一轮结果", exact: true })).toContainText("0 分"); await expect(first.getByRole("group", { name: "第一轮结果", exact: true })).toContainText("耗时：0 秒");
  await expect(first.getByRole("group", { name: "第二轮结果", exact: true })).toContainText("未执行"); await expect(first.getByRole("group", { name: "第二轮结果", exact: true })).toContainText("耗时：未记录");
  await first.getByRole("button", { name: "展开原因", exact: true }).click(); await expect(first).toContainText("完整原因末尾");
  await expect(dialog.getByText("关键词判断，不评分", { exact: true })).toBeVisible(); await expect(dialog.getByText("调用中断，完成时间未知", { exact: true })).toBeVisible();
  expect(state.queries[0].query.toString()).toBe("page=1&page_size=20"); expect(base.writes).toEqual([]); expect(base.errors).toEqual([]);
  mkdirSync(output, { recursive: true }); await page.screenshot({ path: resolve(output, "records-desktop.png"), fullPage: true, animations: "disabled" });
});

test("翻页请求云端第二页，各轮范围独立传递，查询和清空回第一页", async ({ page }) => {
  const { state, base } = await fixture(page); await page.goto("/admin/positions"); const dialog = await openRecords(page);
  await dialog.locator(".ant-pagination-item-2").click(); await expect(dialog.getByText("甲合成人选21", { exact: true })).toBeVisible();
  await dialog.getByRole("spinbutton", { name: "第一轮最低分", exact: true }).fill("0"); await dialog.getByRole("spinbutton", { name: "第一轮最高分", exact: true }).fill("90");
  await dialog.getByRole("spinbutton", { name: "第二轮最低分", exact: true }).fill("70"); await dialog.getByRole("spinbutton", { name: "第二轮最高分", exact: true }).fill("80");
  for (const [name, text] of [["第一轮开始时间从", "2026-10-26 00:00:00"], ["第一轮开始时间至", "2026-10-27 00:00:00"], ["第二轮开始时间从", "2026-10-26 00:00:00"], ["第二轮开始时间至", "2026-10-27 00:00:00"]]) { const input = dialog.getByRole("textbox", { name, exact: true }); await input.fill(text); await input.press("Enter"); }
  await dialog.getByRole("button", { name: "查询", exact: true }).click(); await expect.poll(() => state.queries.at(-1)?.query.get("second_score_min")).toBe("70");
  const query = state.queries.at(-1)!.query; expect(query.get("page")).toBe("1"); expect(query.get("first_score_min")).toBe("0"); expect(query.get("first_score_max")).toBe("90"); expect(query.get("second_score_max")).toBe("80");
  for (const round of ["first", "second"]) for (const side of ["from", "to"]) expect(query.get(`${round}_started_${side}`)).toMatch(/Z$/);
  await dialog.getByRole("button", { name: "清空", exact: true }).click(); await expect.poll(() => state.queries.at(-1)?.query.toString()).toBe("page=1&page_size=20"); await expect(dialog.getByRole("spinbutton", { name: "第一轮最低分", exact: true })).toHaveValue(""); expect(base.writes).toEqual([]);
});

test("反向分数范围不发请求，失败重试仅重新读取记录", async ({ page }) => {
  const { state, base } = await fixture(page); state.fail = true; await page.goto("/admin/positions"); const dialog = await openRecords(page);
  await expect(dialog.getByText("这个岗位暂时无权查看，请确认当前账号。", { exact: true })).toBeVisible();
  state.fail = false; await dialog.getByRole("button", { name: "重试", exact: true }).click(); await expect(dialog.getByText("甲合成人选1", { exact: true })).toBeVisible(); const count = state.queries.length;
  await dialog.getByRole("spinbutton", { name: "第一轮最低分", exact: true }).fill("90"); await dialog.getByRole("spinbutton", { name: "第一轮最高分", exact: true }).fill("10"); await dialog.getByRole("button", { name: "查询", exact: true }).click();
  await expect(dialog.getByText("第一轮最低分不能大于最高分。", { exact: true })).toBeVisible(); expect(state.queries).toHaveLength(count); expect(base.writes).toEqual([]); expect(base.errors).toEqual([]);
});

test("读取中能关闭，切换岗位后旧响应不覆盖新岗位", async ({ page }) => {
  const { state, base } = await fixture(page); state.holdNext = true; await page.goto("/admin/positions"); let dialog = await openRecords(page);
  await expect.poll(() => Boolean(state.release)).toBe(true); await dialog.locator(".ant-modal-footer").getByRole("button", { name: "关闭", exact: true }).click(); await expect(dialog).toBeHidden();
  dialog = await openRecords(page, "position-two"); await expect(dialog.getByText("乙合成人选1", { exact: true })).toBeVisible(); state.release?.();
  await expect(dialog.getByText("甲合成人选1", { exact: true })).toHaveCount(0); await expect(dialog).toContainText("当前岗位：合成乙岗位"); expect(base.writes).toEqual([]); expect(base.errors).toEqual([]);
});

test("手机筛选分页和长原因可访问，表格仅在弹框内部横向滚动", async ({ page }) => {
  const { base } = await fixture(page); await page.setViewportSize({ width: 390, height: 844 }); await page.goto("/admin/positions"); const dialog = await openRecords(page);
  await expect(dialog.getByText("甲合成人选1", { exact: true })).toBeAttached(); const box = (await dialog.boundingBox())!; expect(box.x).toBeGreaterThanOrEqual(0); expect(box.x + box.width).toBeLessThanOrEqual(390);
  await expect(dialog.getByRole("textbox", { name: "第一轮开始时间从", exact: true })).toBeVisible(); mkdirSync(output, { recursive: true }); await page.screenshot({ path: resolve(output, "records-mobile-filters.png"), fullPage: true, animations: "disabled" });
  await dialog.locator("table").scrollIntoViewIfNeeded(); await page.screenshot({ path: resolve(output, "records-mobile-table.png"), fullPage: true, animations: "disabled" });
  await dialog.locator(".ant-table-content").hover(); await page.mouse.wheel(1100, 0);
  await expect.poll(async () => {
    const bounds = (await dialog.boundingBox())!; const result = (await dialog.getByRole("group", { name: "第二轮结果", exact: true }).first().boundingBox())!;
    return result.x >= bounds.x + 20 && result.x + result.width <= bounds.x + bounds.width - 16;
  }).toBe(true);
  await page.screenshot({ path: resolve(output, "records-mobile-second-round.png"), fullPage: true, animations: "disabled" });
  await dialog.locator(".ant-pagination-item-2").click(); await expect(dialog.getByText("甲合成人选21", { exact: true })).toBeAttached(); expect(base.writes).toEqual([]); expect(base.errors).toEqual([]);
});

test("本地程序离线仍可读取云端筛选记录，查看不会发送启动或AI请求", async ({ page }) => {
  const { state, base } = await fixture(page);
  await page.route(/127\.0\.0\.1:43139\//, (route) => route.abort("connectionrefused"));
  await page.goto("/admin/positions");
  // 沿用后台已有的离线安装提醒，关闭提醒后仍可独立读取云端业务记录。
  await page.getByRole("dialog", { name: "请先安装本地程序", exact: true }).getByRole("button", { name: "关闭", exact: true }).click();
  const dialog = await openRecords(page); await expect(dialog.getByText("甲合成人选1", { exact: true })).toBeVisible();
  expect(state.queries.length).toBeGreaterThan(0); expect(state.queries.every((call) => call.position === "position-one" && call.query.get("page_size") === "20")).toBe(true); expect(base.calls.filter((call) => /\/start$|\/ai-compatible\/|\/assistant\//.test(call.path))).toEqual([]); expect(base.writes).toEqual([]); expect(base.errors).toEqual([]);
});
