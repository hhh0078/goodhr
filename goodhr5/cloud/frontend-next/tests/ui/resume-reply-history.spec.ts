/** 文件作用说明：在隔离GoodHR控制台和全模拟接口下验证在线资料、中文回执、可见轮询及阅读位置，绝不访问招聘页面。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";
import type { ReplyHistory } from "../../lib/resume-reply-history";

const output = resolve(process.env.UI_TEST_SCREENSHOT_DIR || "../../../output/resume-reply-history");
test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "reply-history-synthetic-token" }] }] } });
test.setTimeout(90000);

/** mockReplyHistory 只在内存提供两份合成会话，记录请求并可控制回包，不连接任何真实账号或Agent。 */
async function mockReplyHistory(page: Page) {
  const base = await mockUniformAdmin(page);
  const created = "2026-11-10T08:00:00+08:00";
  await page.route("**/api/candidates/uniform-candidate-1", async route => {
    const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*" };
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    return route.fulfill({ headers, json: { candidate: { id: "uniform-candidate-1", candidate_name: "合成候选人1", phone: "", row_version: 1, education_level: "本科", raw_text: "合成原始在线资料：保留真实原文折叠状态，不是完整附件解析。" } } });
  });
  const threads = ["成都", "重庆"].map((city, i) => ({ id: "synthetic-thread-" + i, position_name: city + "招聘顾问", platform: "boss", candidate_name: "合成候选人", status: "active", updated_at: created }));
  const state = { reads: [] as string[], live: 0, maxLive: 0, generation: 0, pageSize: 0, hold: undefined as Promise<void> | undefined };
  await page.route(/\/api\/candidates\/[^/]+\/auto-reply(?:\?|$)/, async route => {
    const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    const url = new URL(route.request().url());
    if (url.searchParams.get("section") === "recommendations") return route.fulfill({ headers, json: { auto_reply: { recommendations: [] } } });
    const id = url.searchParams.get("thread_id") || threads[0].id;
    const generation = state.generation;
    const hold = state.hold;
    state.reads.push(id); state.live++; state.maxLive = Math.max(state.maxLive, state.live);
    const profile = {
      candidate_name: "合成候选人", age: 28, education_level: "本科", earliest_company_name: "合成长期服务与招聘协作公司（用于检查手机换行）", earliest_company_year: 2016,
      earliest_school_name: "合成大学", earliest_school_year: null, mode: "ai", match_status: generation ? "matched" : "temporary", is_temporary: generation === 0, updated_at: created,
    };
    const snapshot: ReplyHistory = {
      threads, thread_id: id, online_profile: profile, contacts: { phone: generation ? "13900001111" : "", wechat: "synthetic_wechat" },
      info_requests: [
        { kind: "resume", status: "requested", status_label: "已申请", updated_at: created },
        { kind: "phone", status: generation ? "received" : "unknown", status_label: generation ? "已收到" : "结果待核实", updated_at: created },
        { kind: "wechat", status: "received", status_label: "已收到", updated_at: created },
      ],
      messages: Array.from({ length: 30 + generation }, (_, i) => ({ id: id + "-message-" + i, side: i % 2 ? "recruiter" : "candidate", sender_type: i % 2 ? "ai" : "human", text: `${id} 合成沟通 ${i}：确认岗位的具体安排，所有内容仅用于界面测试。`, sent_at: created })),
      events: [{ id: id + "-event", title: "索要手机号", status: "unknown", status_label: "结果待核实", description: "原生资料索要结果未确认，暂不重复。", occurred_at: created, duration_ms: 533 }],
      goals: [
        { id: id + "-goal-one", title: "确认意向城市", status: "matched", source_type: "ai", evidence: "候选人明确意向成都。", evidence_message_ids: [id + "-message-0"], updated_at: created },
        { id: id + "-goal-two", title: "确认方便沟通时间", status: generation ? "matched" : "pending", source_type: "ai", evidence: generation ? "候选人明确下午方便。" : "", updated_at: created },
      ],
      next_message_cursor: "", next_event_cursor: "",
    };
    if (state.pageSize) {
      const end = Number(url.searchParams.get("message_cursor")) || snapshot.messages.length;
      const start = Math.max(0, end - state.pageSize);
      snapshot.messages = snapshot.messages.slice(start, end);
      snapshot.next_message_cursor = start ? String(start) : "";
    }
    try { await hold; await route.fulfill({ headers, json: { auto_reply: snapshot } }); }
    finally { state.live--; }
  });
  return { base, state };
}

for (const width of [1366, 390]) {
  test(`在线资料与两目标 ${width}px 沿用共用详情和中文资料进度`, async ({ page }) => {
    const { base } = await mockReplyHistory(page);
    await page.setViewportSize({ width, height: 960 });
    await page.goto("/admin/resumes");
    await page.getByText("合成候选人1", { exact: true }).first().click();
    const detail = page.locator(".goodhr-detail-dialog:visible").last();
    await detail.getByRole("tab", { name: "自动回复", exact: true }).click();
    const section = detail.getByRole("region", { name: "自动回复沟通记录" });
    const profile = section.getByRole("region", { name: "在线基础资料", exact: true });
    await expect(profile.getByText("已确认 6 / 7 项", { exact: true })).toBeVisible();
    await expect(profile.getByText("AI 识别", { exact: true })).toBeVisible();
    await expect(profile.getByText("在线临时资料", { exact: true })).toBeVisible();
    await expect(profile.getByText("暂未确认", { exact: true })).toHaveCount(1);
    await expect(section.getByText("手机号 · 不确定，暂不重复", { exact: true })).toBeVisible();
    await expect(section.getByText("简历 · 已申请", { exact: true })).toBeVisible();
    await expect(section.getByText("微信 · 已收到", { exact: true })).toBeVisible();
    await expect(section.getByRole("status")).toContainText("1 / 2");
    await section.getByRole("tab", { name: "沟通目标", exact: true }).click();
    await expect(section.getByText("确认意向城市", { exact: true })).toBeVisible();
    await expect(section.getByText("确认方便沟通时间", { exact: true })).toBeVisible();
    await expect(section.getByText("待确认", { exact: true })).toBeVisible();
    mkdirSync(output, { recursive: true });
    await section.scrollIntoViewIfNeeded();
    await page.screenshot({ path: resolve(output, `online-profile-goals-${width}.png`), animations: "disabled", fullPage: false });
    const box = await section.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
    await section.getByRole("tab", { name: "操作时间线", exact: true }).click();
    await expect(section.getByText("索要手机号", { exact: true })).toBeVisible();
    await expect(section.getByText("耗时 533 毫秒", { exact: true })).toBeVisible();
    await expect(section.getByText("unknown", { exact: true })).toHaveCount(0);
    expect(base.errors).toEqual([]); expect(base.writes).toEqual([]); expect(base.unknown).toEqual([]);
  });
}

test("可见时串行刷新保留阅读位置和展开目标，隐藏停止并丢弃迟到响应", async ({ page }) => {
  const { state, base } = await mockReplyHistory(page);
  await page.goto("/admin/resumes/detail?candidate_id=uniform-candidate-1");
  await page.getByText("查看完整简历正文", { exact: true }).click();
  await expect(page.getByText("合成原始在线资料：保留真实原文折叠状态，不是完整附件解析。", { exact: true })).toBeVisible();
  expect(state.reads).toHaveLength(0);
  await page.getByRole("tab", { name: "自动回复", exact: true }).click();
  const section = page.getByRole("region", { name: "自动回复沟通记录" });
  const chat = section.getByLabel("聊天记录滚动区", { exact: true });
  await expect(section.getByText(/synthetic-thread-0 合成沟通 29/)).toBeVisible();
  await page.clock.install();
  await chat.hover(); await page.mouse.wheel(0, -480);
  const before = await chat.evaluate(node => node.scrollTop);
  state.generation = 1;
  await page.clock.fastForward(11000);
  await expect(section.getByText(/synthetic-thread-0 合成沟通 30/)).toHaveCount(1);
  await expect(section.getByText("13900001111", { exact: true })).toBeVisible();
  await expect(section.getByText("synthetic_wechat", { exact: true })).toBeVisible();
  await expect(section.getByText("已入库资料", { exact: true })).toBeVisible();
  expect(await chat.evaluate(node => node.scrollTop)).toBeCloseTo(before, 0);
  await section.getByRole("tab", { name: "沟通目标", exact: true }).click();
  await section.getByText("查看关联原话（1）", { exact: true }).click();
  state.generation = 2;
  await page.clock.fastForward(11000);
  await expect(section.getByRole("status")).toContainText("2 / 2");
  await expect(section.locator("details[open]")).toHaveCount(1);
  await section.getByRole("combobox", { name: "选择沟通会话" }).click();
  await page.getByTitle(/重庆招聘顾问/).click();
  await expect(section.getByText("重庆招聘顾问", { exact: true }).first()).toBeVisible();
  let release!: () => void;
  state.hold = new Promise<void>(resolve => { release = resolve; });
  const count = state.reads.length;
  await page.clock.fastForward(11000);
  await expect.poll(() => state.reads.length).toBe(count + 1);
  await page.clock.fastForward(10000);
  expect(state.reads.length).toBe(count + 1);
  expect(state.maxLive).toBe(1);
  await page.getByRole("tab", { name: "简历", exact: true }).click();
  await expect(section).not.toBeVisible();
  await expect(page.getByText("合成原始在线资料：保留真实原文折叠状态，不是完整附件解析。", { exact: true })).toBeVisible();
  state.hold = undefined; release();
  await page.clock.fastForward(25000);
  expect(state.reads.length).toBe(count + 1);
  await page.getByRole("tab", { name: "自动回复", exact: true }).click();
  await expect.poll(() => state.reads.length).toBe(count + 2);
  expect(state.reads.at(-1)).toBe("synthetic-thread-1");
  await expect(section.getByText(/synthetic-thread-0 合成沟通/)).toHaveCount(0);
  expect(base.errors).toEqual([]); expect(base.writes).toEqual([]); expect(base.unknown).toEqual([]);
});

test("增量超过一页时先补齐锚点，旧消息和更早分页不丢不重复", async ({ page }) => {
  const { state, base } = await mockReplyHistory(page);
  state.pageSize = 5;
  await page.goto("/admin/resumes/detail?candidate_id=uniform-candidate-1");
  await page.getByRole("tab", { name: "自动回复", exact: true }).click();
  const section = page.getByRole("region", { name: "自动回复沟通记录" });
  const chat = section.getByRole("list", { name: /双方聊天记录/ });
  await expect(chat.getByRole("listitem")).toHaveCount(5);
  // 这一项保留真实计时，避免按钮波纹尚在清理时安装虚拟时钟干扰Ant Design的卸载微任务。
  await section.getByRole("button", { name: "查看更早消息" }).click();
  await expect(chat.getByRole("listitem")).toHaveCount(10);
  state.generation = 12;
  await expect(chat.getByRole("listitem")).toHaveCount(22, { timeout: 20000 });
  await expect(chat.getByRole("listitem").first()).toContainText("合成沟通 20：");
  await expect(chat.getByRole("listitem").last()).toContainText("合成沟通 41：");
  await section.getByRole("button", { name: "查看更早消息" }).click();
  await expect(chat.getByRole("listitem")).toHaveCount(27);
  await expect(chat.getByRole("listitem").first()).toContainText("合成沟通 15：");
  expect(state.maxLive).toBe(1);
  expect(base.errors).toEqual([]); expect(base.writes).toEqual([]); expect(base.unknown).toEqual([]);
});
