/** 文件作用说明：以完整模拟接口验证助手刷新恢复、JSON事件、向量后台任务及猎头客户岗位维护，禁止真实服务写入。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const output = resolve("../../../output/ai-headhunter-20261022");
test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "assistant-synthetic-token" }] }] } });
type State = { sessionID: string; runID: string; messages: any[]; events: string[]; finish: boolean; invalid: boolean; noText?: boolean; recommendationResult?: unknown; toolRecommendationResult?: unknown; failAfterTools?: boolean; referenceRequests: any[][]; referenceInvalid?: boolean; referenceFailure?: boolean; userID?: string; tenantID?: string; dropReply?: boolean; reloadOnMessage?: boolean; denied?: boolean; recovered?: number; vectorStarts: number; vectorEligible: boolean; customerWrites: any[]; jobWrites: any[]; customers: any[]; jobs: any[] };

/** mockFeatures 拦截本次新接口，与现有全拦截后台夹具配合，不访问真实数据库、AI或Agent。 */
async function mockFeatures(page: Page) {
  const base = await mockUniformAdmin(page, true);
  const state: State = { sessionID: "", runID: "", messages: [], events: [], finish: true, invalid: false, referenceRequests: [], vectorStarts: 0, vectorEligible: true, customerWrites: [], jobWrites: [], customers: [{ id: "customer-one", name: "合成甲方公司", short_name: "合成甲方", status: "active", owner_email: "uniform@example.invalid", revision: 1, contacts: [], contracts: [], can_manage: true }], jobs: [] };
  await page.route(/\/api\/(assistant|headhunter)\/|\/api\/resume-library\/(vectors|settings)|\/api\/auth\/me$/, async (route) => {
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname; const method = request.method();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    let body: any; try { body = request.postDataJSON(); } catch { /* GET 无正文。 */ }
    let data: any = { ok: true };
    if (path === "/api/auth/me") data = { user: { id: state.userID || "uniform-user", email: "uniform@example.invalid", role: "super_admin", tenant_id: state.tenantID || "uniform-team", has_openid: true } };
    else if (path === "/api/assistant/references/validate") {
      state.referenceRequests.push(body.references);
      if (state.referenceFailure) return route.abort("failed");
      data = { ok: true, invalid: state.referenceInvalid ? body.references : [] };
    }
    else if (path === "/api/assistant/sessions" && method === "POST") { state.sessionID = body.id; state.runID = `${body.id}:run-one`; data = { ok: true, session_id: body.id, session: { id: body.id } }; }
    else if (/\/assistant\/sessions\/[^/]+$/.test(path) && method === "GET") { state.recovered = (state.recovered || 0) + 1; data = { ok: true, session_id: state.sessionID, active_run: state.runID, runs: [{ id: state.runID, request_id: state.messages[0]?.request_id, status: "running" }] }; }
    else if (/\/assistant\/sessions\/[^/]+\/messages$/.test(path)) {
      state.messages.push(body);
      if (state.reloadOnMessage) { state.reloadOnMessage = false; void page.reload().catch(() => undefined); return route.abort("failed"); }
      if (state.dropReply) { state.dropReply = false; return route.abort("failed"); }
      data = { ok: true, session_id: state.sessionID, run_id: state.runID };
    }
    else if (path.endsWith("/events")) {
      state.events.push(url.searchParams.get("after") || "");
      if (state.denied) return route.fulfill({ status: 403, headers, json: { ok: false, error: "权限已撤销" } });
      /** event 把完整合同事件转换为SSE，重复事件用于检验刷新后的去重。 */
      const event = (id: number, type: string, data: unknown) => `data: ${JSON.stringify({ id: String(id), session_id: state.sessionID, run_id: state.runID, type, data, created_at: "2026-10-22T10:00:00Z" })}\n\n`;
      const initial = event(1, "phase", { text: "正在找岗位" }) + event(2, "summary", { text: "先核对完整岗位，再查简历库。" }) + event(3, "tool_start", { call_id: "call-one", name: "search_jobs", arguments: { keyword: "产品经理" } }) + event(4, "tool_result", { call_id: "call-one", name: "search_jobs", result: state.toolRecommendationResult || { items: [{ name: "高级产品经理" }] }, duration_ms: 25, attempt: 1 }) + (state.noText ? "" : event(5, "text", { text: "找到相关人选。" }));
      const result = state.recommendationResult || { recommendations: [{ candidate_id: "candidate-synthetic", candidate_name: "合成人选", resume_version: 1, requirements_version: 1, strengths: ["有可核对的产品设计经历"], gaps: [], questions_to_verify: ["需确认到岗时间"], recommendation_status: "建议优先沟通", evidence_refs: [{ path: "work_experiences[1].content", quote: "负责招聘协作产品的需求设计" }] }] };
      const done = (state.noText ? "" : event(5, "text", { text: "找到相关人选。" }) + event(6, "text", { text: "请查看经历证据。" })) + (state.failAfterTools ? event(7, "failed", { error: "正文未完成" }) : event(7, "completed", { result }));
      return route.fulfill({ headers: { ...headers, "content-type": "text/event-stream" }, body: state.invalid ? "data: 这不是JSON\n\n" : state.events.length === 1 ? initial + (state.finish ? done : "") : state.finish ? done : ": heartbeat\n\n" });
    }
    else if (path.endsWith("/cancel")) data = { ok: true };
    else if (path === "/api/resume-library/settings") data = { settings: { auto_import_downloads: true } };
    else if (path === "/api/resume-library/vectors") {
      if (method === "POST") { state.vectorStarts += 1; data = { ok: true, task_id: "vector-one", status: "queued" }; }
      else data = { ok: true, total: 120, indexed: 90, pending: 30, running: state.vectorStarts > 0, eligible: state.vectorEligible, reason: state.vectorEligible ? "" : "请使用本站内置 AI 后再向量化。", model: "qwen3.7-text-embedding-flash", input_price_per_million: 0.125, output_price_per_million: 0 };
    }
    else if (path === "/api/headhunter/metadata") data = { ok: true, members: [{ email: "uniform@example.invalid", role: "owner", status: "active" }], positions: [{ id: "platform-one", name: "合成平台产品岗位", platform_id: "boss", is_open: true }] };
    else if (path === "/api/headhunter/customers") {
      if (method === "POST") { state.customerWrites.push(body); const customer = { ...body, id: "customer-created", revision: 1, can_manage: true }; state.customers.push(customer); data = { ok: true, customer }; }
      else data = { ok: true, items: state.customers, total: state.customers.length, page: 1, page_size: 20 };
    }
    else if (path.startsWith("/api/headhunter/customers/")) { const customer = state.customers.find((item) => path.endsWith(item.id)); if (method === "PUT") { state.customerWrites.push(body); Object.assign(customer, body, { revision: customer.revision + 1 }); } data = { ok: true, customer }; }
    else if (path === "/api/headhunter/jobs") {
      if (method === "POST") { state.jobWrites.push(body); const job = { ...body, id: "job-created", revision: 1, can_manage: true }; state.jobs.push(job); data = { ok: true, job }; }
      else data = { ok: true, items: state.jobs, total: state.jobs.length, page: 1, page_size: 20 };
    }
    return route.fulfill({ status: 200, headers, json: data });
  });
  return { state, base };
}

/** openAssistant 使用真实顶部图标入口，打开后可恢复全局会话。 */
async function openAssistant(page: Page) { await page.getByRole("button", { name: "AI 助手", exact: true }).click(); return page.getByRole("dialog", { name: "AI 招聘助手" }); }

test("顶部只有AI图标，JSON正文流、工具记录和思考折叠可查看", async ({ page }) => {
  const { state, base } = await mockFeatures(page); await page.goto("/admin");
  const icon = page.getByRole("button", { name: "AI 助手", exact: true }); await expect(icon).toHaveText("");
  const dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill("按产品经理岗位帮我找人"); await dialog.getByRole("button", { name: "发送 AI 消息" }).click();
  await expect(dialog.getByText("找到相关人选。请查看经历证据。", { exact: true })).toBeVisible();
  await expect(dialog.getByText("先核对完整岗位，再查简历库。", { exact: true })).toBeHidden();
  await dialog.getByText("查看处理过程", { exact: true }).click(); await expect(dialog.getByText("先核对完整岗位，再查简历库。", { exact: true })).toBeVisible();
  await dialog.getByText("search_jobs", { exact: true }).click(); await expect(dialog.getByText("传入参数", { exact: true })).toBeVisible(); await expect(dialog.getByText("返回结果", { exact: true })).toBeVisible();
  await expect(dialog.getByText("合成人选", { exact: true })).toBeVisible(); await expect(dialog.getByText("需确认到岗时间", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("link", { name: "查看原简历", exact: true })).toHaveAttribute("href", "/admin/resumes/detail?candidate_id=candidate-synthetic");
  expect(state.messages).toHaveLength(1); expect(base.errors).toEqual([]);
  mkdirSync(output, { recursive: true }); await page.screenshot({ path: resolve(output, "assistant-desktop.png"), fullPage: true, animations: "disabled" });
  await page.setViewportSize({ width: 390, height: 844 }); await page.screenshot({ path: resolve(output, "assistant-mobile.png"), fullPage: true, animations: "disabled" });
});

test("刷新主动重连原任务并从IndexedDB恢复游标，不重复发送或重复追加", async ({ page }) => {
  const { state, base } = await mockFeatures(page); state.finish = false; await page.goto("/admin");
  let dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill("帮我找高级产品经理"); await dialog.getByRole("button", { name: "发送 AI 消息" }).click();
  await expect(dialog.getByText("找到相关人选。", { exact: true })).toBeVisible(); await page.waitForTimeout(300);
  state.finish = true; await page.reload();
  await expect.poll(() => state.events.some((after) => after === "5")).toBe(true);
  dialog = await openAssistant(page); await expect(dialog.getByText("找到相关人选。请查看经历证据。", { exact: true })).toBeVisible();
  expect(state.messages).toHaveLength(1); expect(base.errors).toEqual([]);
});

test("只有待确认人选且部分分析失败时独立展示，原文证据可读且不依赖正文", async ({ page }) => {
  const { state, base } = await mockFeatures(page); state.noText = true;
  state.recommendationResult = { recommendations: [], partial_failure: true, pending_verification: [{ candidate_id: "candidate-pending", name: "待核实合成人选", resume_version: 2, requirements_version: "requirements-v2", recommendation_status: "条件待确认", strengths: ["有相关项目经历"], gaps: [], questions_to_verify: ["必要证书是否有效还需确认"], requirement_checks: [{ requirement_id: "r1", status: "unknown", evidence_refs: [] }], evidence_refs: [{ requirement_id: "r2", chunk_id: "chunk-two", path: "project_experiences[1].content", quote: "参与招聘流程产品的需求整理和交付" }] }] };
  await page.goto("/admin"); const dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill("核对未知的必要条件"); await dialog.getByRole("button", { name: "发送 AI 消息" }).click();
  await expect(dialog.getByText("部分分析未完成，已核实结果见下", { exact: true })).toBeVisible();
  const pending = dialog.getByRole("region", { name: "待确认人选", exact: true }); await expect(pending).toBeVisible();
  await expect(dialog.getByRole("region", { name: "已核实推荐", exact: true })).toHaveCount(0);
  await expect(pending.getByText("待核实合成人选", { exact: true })).toBeVisible(); await expect(pending.getByText("条件待确认", { exact: true })).toBeVisible();
  await expect(pending.getByText("必要证书是否有效还需确认", { exact: true })).toBeVisible();
  await pending.getByText("查看逐项核对和原文证据", { exact: true }).click();
  await expect(pending.getByText("参与招聘流程产品的需求整理和交付", { exact: true })).toBeVisible();
  await expect(pending.getByText("来源：project_experiences[1].content", { exact: true })).toBeVisible();
  await expect(dialog.getByText("找到相关人选。请查看经历证据。", { exact: true })).toHaveCount(0); expect(base.errors).toEqual([]);
  mkdirSync(output, { recursive: true }); await page.screenshot({ path: resolve(output, "assistant-pending-desktop.png"), fullPage: true, animations: "disabled" });
  await page.setViewportSize({ width: 390, height: 844 }); await page.screenshot({ path: resolve(output, "assistant-pending-mobile.png"), fullPage: true, animations: "disabled" });
});

test("无效JSON全部拒收，仍能关闭弹框，新建第11个本地会话只保留10个", async ({ page }) => {
  const { state, base } = await mockFeatures(page); state.invalid = true; await page.goto("/admin");
  const dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill("测试无效JSON"); await dialog.getByRole("button", { name: "发送 AI 消息" }).click();
  await expect(dialog.getByText("助手返回了无效 JSON，已拒绝接收。", { exact: true })).toBeVisible();
  await expect(dialog.getByText("这不是JSON", { exact: true })).toHaveCount(0);
  for (let index = 0; index < 10; index += 1) await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await expect(dialog.getByText("10/10 个本地会话", { exact: true })).toBeVisible(); expect(base.errors).toEqual([]);
});

test("任务创建回包丢失后刷新主动核对原请求并接回，不重发消息", async ({ page }) => {
  const { state, base } = await mockFeatures(page); state.dropReply = true; await page.goto("/admin");
  let dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill("回包丢失也要接回这个任务"); await dialog.getByRole("button", { name: "发送 AI 消息" }).click();
  await expect(dialog.getByRole("button", { name: "重试发送上一条", exact: true })).toBeVisible(); await page.waitForTimeout(300); await page.reload();
  await expect.poll(() => state.recovered || 0).toBe(1); dialog = await openAssistant(page);
  await expect(dialog.getByText("找到相关人选。请查看经历证据。", { exact: true })).toBeVisible(); expect(state.messages).toHaveLength(1); expect(base.errors).toEqual([]);
});

test("发送请求刚到后端就立即刷新且丢回包，已持久保存的原编号仍能接回且只提交一次", async ({ page }) => {
  const { state, base } = await mockFeatures(page); state.reloadOnMessage = true; await page.goto("/admin");
  let dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill("发送瞬间刷新也保留原编号");
  const refreshed = page.waitForEvent("load"); await dialog.getByRole("button", { name: "发送 AI 消息" }).click(); await refreshed;
  await expect.poll(() => state.recovered || 0).toBe(1); dialog = await openAssistant(page);
  await expect(dialog.getByText("找到相关人选。请查看经历证据。", { exact: true })).toBeVisible();
  await expect(dialog.getByTestId("assistant-messages").getByText("发送瞬间刷新也保留原编号", { exact: true })).toHaveCount(1);
  expect(state.messages).toHaveLength(1); expect(state.messages[0].request_id).toMatch(/^[\da-f-]{36}$/); expect(state.events).toHaveLength(1); expect(base.errors).toEqual([]);
});

test("刷新重连发现权限撤销时清除旧正文和工具输出", async ({ page }) => {
  const { state, base } = await mockFeatures(page); state.finish = false; await page.goto("/admin");
  let dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill("检查权限变化"); await dialog.getByRole("button", { name: "发送 AI 消息" }).click();
  await expect(dialog.getByText("找到相关人选。", { exact: true })).toBeVisible(); await page.waitForTimeout(300); state.denied = true; await page.reload();
  dialog = await openAssistant(page); await expect(dialog.getByText("当前账号无法继续查看这个任务，请重新登录或检查团队权限。", { exact: true })).toBeVisible();
  await expect(dialog.getByText("找到相关人选。", { exact: true })).toHaveCount(0); await expect(dialog.getByText("search_jobs", { exact: true })).toHaveCount(0); expect(base.errors).toEqual([]);
});

for (const change of ["权限撤销", "简历版本变化", "简历删除"]) {
  test(`已完成推荐刷新后发现${change}，清除旧资料但保留会话和用户消息`, async ({ page }) => {
    const { state, base } = await mockFeatures(page); await page.goto("/admin");
    let dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
    await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill(`复核${change}`); await dialog.getByRole("button", { name: "发送 AI 消息" }).click();
    await expect(dialog.getByText("合成人选", { exact: true })).toBeVisible(); await page.waitForTimeout(300);
    state.referenceInvalid = true; await page.reload(); dialog = await openAssistant(page);
    await expect(dialog.getByText("资料或权限已变化，请重新检索", { exact: true })).toBeVisible();
    await expect(dialog.getByText("合成人选", { exact: true })).toHaveCount(0);
    await expect(dialog.getByText("找到相关人选。请查看经历证据。", { exact: true })).toHaveCount(0);
    await expect(dialog.getByText("search_jobs", { exact: true })).toHaveCount(0);
    await expect(dialog.getByRole("link", { name: "查看原简历", exact: true })).toHaveCount(0);
    await expect(dialog.getByTestId("assistant-messages").getByText(`复核${change}`, { exact: true })).toBeVisible();
    await expect(dialog.getByText("1/10 个本地会话", { exact: true })).toBeVisible();
    expect(state.referenceRequests.length).toBeGreaterThan(0); expect(state.referenceRequests[0]).toEqual([{ candidate_id: "candidate-synthetic", resume_version: 1 }]);
    expect(state.messages).toHaveLength(1); expect(state.events).toHaveLength(1); expect(base.errors).toEqual([]);
  });
}

test("历史引用核对网络失联时保留历史但隐藏已核实入口，重新打开后可恢复或发现变更", async ({ page }) => {
  const { state, base } = await mockFeatures(page); await page.goto("/admin");
  let dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill("历史网络核对"); await dialog.getByRole("button", { name: "发送 AI 消息" }).click();
  await expect(dialog.getByText("合成人选", { exact: true })).toBeVisible(); await page.waitForTimeout(300);
  state.referenceFailure = true; await page.reload(); dialog = await openAssistant(page);
  await expect(dialog.getByText("历史结果尚未核对，请联网后重新打开助手", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("region", { name: "已核实推荐", exact: true })).toHaveCount(0);
  await expect(dialog.getByRole("link", { name: "查看原简历", exact: true })).toHaveCount(0);
  await expect(dialog.getByText("合成人选", { exact: true })).toHaveCount(0);
  await expect(dialog.getByTestId("assistant-messages").getByText("历史网络核对", { exact: true })).toBeVisible();
  state.referenceFailure = false; await page.keyboard.press("Escape"); await expect(dialog).toBeHidden(); dialog = await openAssistant(page);
  await expect(dialog.getByRole("region", { name: "已核实推荐", exact: true })).toBeVisible(); await expect(dialog.getByText("合成人选", { exact: true })).toBeVisible();
  state.referenceInvalid = true; await page.keyboard.press("Escape"); await expect(dialog).toBeHidden(); dialog = await openAssistant(page);
  await expect(dialog.getByText("资料或权限已变化，请重新检索", { exact: true })).toBeVisible();
  await expect(dialog.getByText("合成人选", { exact: true })).toHaveCount(0);
  expect(state.messages).toHaveLength(1); expect(base.errors).toEqual([]);
});

test("工具已有待确认资料而最终聊天失败时，刷新同样核对并清理工具中的失效引用", async ({ page }) => {
  const { state, base } = await mockFeatures(page); state.failAfterTools = true;
  state.toolRecommendationResult = { recommendations: [], pending_verification: [{ candidate_id: "candidate-tool", resume_version: 3, name: "工具内合成人选" }] };
  await page.goto("/admin"); let dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill("核对失败任务中的工具资料"); await dialog.getByRole("button", { name: "发送 AI 消息" }).click();
  await expect(dialog.getByText("正文未完成", { exact: true })).toBeVisible(); await expect(dialog.getByText("search_jobs", { exact: true })).toBeVisible(); await page.waitForTimeout(300);
  state.referenceInvalid = true; await page.reload(); dialog = await openAssistant(page);
  await expect(dialog.getByText("资料或权限已变化，请重新检索", { exact: true })).toBeVisible();
  await expect(dialog.getByText("search_jobs", { exact: true })).toHaveCount(0);
  await expect(dialog.getByText("找到相关人选。请查看经历证据。", { exact: true })).toHaveCount(0);
  expect(state.referenceRequests[0]).toEqual([{ candidate_id: "candidate-tool", resume_version: 3 }]); expect(state.messages).toHaveLength(1); expect(base.errors).toEqual([]);
});

test("本地会话同时按团队和账号隔离，切回原身份历史仍在", async ({ page }) => {
  const { state, base } = await mockFeatures(page); await page.goto("/admin");
  let dialog = await openAssistant(page); await dialog.getByRole("button", { name: "新对话", exact: true }).click();
  await dialog.getByRole("textbox", { name: "给 AI 助手发消息" }).fill("只属于原团队的对话"); await dialog.getByRole("button", { name: "发送 AI 消息" }).click();
  await expect(dialog.getByText("找到相关人选。请查看经历证据。", { exact: true })).toBeVisible(); await page.waitForTimeout(300);
  state.tenantID = "other-team"; await page.reload(); dialog = await openAssistant(page); await expect(dialog.getByText("0/10 个本地会话", { exact: true })).toBeVisible(); await expect(dialog.getByText("只属于原团队的对话", { exact: true })).toHaveCount(0);
  state.tenantID = "uniform-team"; state.userID = "other-user"; await page.reload(); dialog = await openAssistant(page); await expect(dialog.getByText("0/10 个本地会话", { exact: true })).toBeVisible();
  state.userID = "uniform-user"; await page.reload(); dialog = await openAssistant(page); await expect(dialog.getByTestId("assistant-messages").getByText("只属于原团队的对话", { exact: true })).toBeVisible(); expect(base.errors).toEqual([]);
});

test("向量化显示真实统计、提交即返回且允许关闭，外部AI限制由后端状态展示", async ({ page }) => {
  const { state, base } = await mockFeatures(page); await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "设置", exact: true }).click(); const dialog = page.getByRole("dialog", { name: "简历库设置" });
  await expect(dialog.getByText("简历总数：", { exact: false })).toContainText("120"); await expect(dialog.getByText("已向量化：", { exact: false })).toContainText("90");
  await dialog.getByRole("button", { name: "简历向量化", exact: true }).click(); await expect(dialog.getByText("已经在后台处理，可以关掉弹框，稍后刷新页面查看数量。", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "简历向量化", exact: true })).toBeDisabled(); expect(state.vectorStarts).toBe(1);
  await expect(dialog.getByText("模型：qwen3.7-text-embedding-flash · 输入 ¥0.125/百万 Token · 输出不收费", { exact: true })).toBeVisible();
  mkdirSync(output, { recursive: true }); await page.screenshot({ path: resolve(output, "vectorization-desktop.png"), fullPage: true, animations: "disabled" });
  await dialog.getByRole("button", { name: "取消", exact: true }).click(); await expect(dialog).toBeHidden();
  state.vectorEligible = false; await page.getByRole("button", { name: "设置", exact: true }).click(); await expect(dialog.getByText("请使用本站内置 AI 后再向量化。", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "简历向量化", exact: true })).toBeDisabled(); expect(base.errors).toEqual([]);
});

test("客户资料和甲方岗位可保存，岗位绑定现有平台岗位且不启动自动任务", async ({ page }) => {
  const { state, base } = await mockFeatures(page); await page.goto("/admin/headhunter/customers");
  await expect(page.getByRole("menuitem", { name: "企业招聘", exact: true })).toBeVisible(); await expect(page.getByRole("menuitem", { name: "客户管理", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "新建客户", exact: true }).click(); const customer = page.getByRole("dialog", { name: "新建客户", exact: true });
  await customer.getByRole("textbox", { name: /甲方公司名称$/ }).fill("新建合成客户"); await customer.getByRole("button", { name: "保存资料", exact: true }).click(); await expect(customer).toBeHidden();
  expect(state.customerWrites[0].name).toBe("新建合成客户");
  expect(state.customerWrites[0]).not.toHaveProperty("id"); expect(state.customerWrites[0]).not.toHaveProperty("can_manage");
  mkdirSync(output, { recursive: true }); await page.screenshot({ path: resolve(output, "headhunter-customers-desktop.png"), fullPage: true, animations: "disabled" });
  await page.getByRole("menuitem", { name: "客户岗位管理", exact: true }).click(); await page.getByRole("button", { name: "新建甲方岗位", exact: true }).click(); const job = page.getByRole("dialog", { name: "新建甲方岗位", exact: true });
  await job.getByRole("textbox", { name: /甲方岗位名称$/ }).fill("高级产品经理"); await job.getByRole("combobox", { name: /甲方客户$/ }).click(); await page.getByTitle("新建合成客户", { exact: true }).click();
  await job.getByRole("textbox", { name: "任职要求", exact: true }).fill("具备可核对的产品设计经历，行业经验优先。");
  await job.getByRole("combobox", { name: "绑定已有平台岗位", exact: true }).click(); await page.getByTitle("合成平台产品岗位 · boss", { exact: true }).click(); await job.getByRole("textbox", { name: "任职要求", exact: true }).click();
  await job.getByRole("button", { name: "保存资料", exact: true }).click(); await expect(job).toBeHidden();
  expect(state.jobWrites[0].position_ids).toEqual(["platform-one"]); expect(state.jobWrites[0].customer_id).toBe("customer-created"); expect(base.writes).toEqual([]); expect(base.errors).toEqual([]);
  await page.screenshot({ path: resolve(output, "headhunter-jobs-desktop.png"), fullPage: true, animations: "disabled" });
  await page.setViewportSize({ width: 390, height: 844 }); await expect(page.getByText("甲方岗位和平台绑定已保存。", { exact: true })).toBeHidden({ timeout: 6000 }); await page.screenshot({ path: resolve(output, "headhunter-jobs-mobile.png"), fullPage: true, animations: "disabled" });
});

test("现有岗位、简历库、猎头推荐和企业招聘桌面手机入口继续可用", async ({ page }) => {
  test.setTimeout(120000); const { base } = await mockFeatures(page); mkdirSync(output, { recursive: true });
  for (const width of [1600, 390]) {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 });
    for (const route of ["positions", "resumes", "recommendations", "recruitment"]) {
      await page.goto(`/admin/${route}`); await expect(page.getByRole("button", { name: "AI 助手", exact: true })).toBeVisible();
      await expect(page.locator("main")).toBeVisible(); await page.screenshot({ path: resolve(output, `${route}-${width}.png`), fullPage: true, animations: "disabled" });
    }
  }
  expect(base.errors).toEqual([]); expect(base.unknown).toEqual([]); expect(base.writes).toEqual([]);
});
