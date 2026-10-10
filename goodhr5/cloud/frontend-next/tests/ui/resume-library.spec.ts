/** 文件作用说明：用完全模拟的云端和本地接口验证新简历库查重、保存重试、推荐记录和缺号草稿，避免真实数据写入。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const previewOrigin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
const screenshotDirectory = resolve("artifacts/resume-library");
// 合成16px纯色PNG只用于界面验证，不含任何真实候选人的照片。
const syntheticAvatar = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAGUlEQVR4nGNwLAj5TwlmGDVg1IBRA4aLAQBSaQQfj40p1QAAAABJRU5ErkJggg==";
type Call = { path: string; method: string; body: any; query: URLSearchParams };
type State = { calls: Call[]; notes: any[]; recommendations: any[]; recommendationSaveGate?: Promise<void>; positions: Array<{ id: string; name: string }>; user: { id: string; email: string }; candidates: any[]; agentOffline?: boolean; failFirstNote: boolean; failConfirmAt?: number; task: any; summaryTask?: any; summaryRequests?: Record<string, any>; failSummaryResponseOnce?: boolean; rejectSummaryOnce?: boolean; errors: string[] };
const existing = { id: "resume-existing", candidate_name: "原有候选人", phone: "13800138000", row_version: 3, source_type: "automatic", education_level: "本科", expected_salary_min: null, expected_salary_max: null, work_experiences: [{ company_name: "原有公司", position_name: "产品经理", start_ym: "2021-01", content: "已有工作经历保留" }], user_email: "test@example.invalid", created_at: "2026-10-10T10:00:00+08:00", ai: { detail: { score: null, reason: "" } } };
test.use({ storageState: { cookies: [], origins: [{ origin: previewOrigin, localStorage: [{ name: "goodhr5_access_token", value: "resume-test-only" }] }] } });

/** mockResumeSession 拦截全部业务请求，模拟持久化草稿与失败重试，不访问招聘平台。 */
async function mockResumeSession(page: Page): Promise<State> {
  const state: State = { calls: [], notes: [], recommendations: [], positions: [{ id: "position-one", name: "产品经理" }], user: { id: "resume-user", email: "test@example.invalid" }, candidates: [{ ...existing }], failFirstNote: false, errors: [], task: { id: "import-test", status: "completed", total: 1, processed: 1, items: [{ id: "import-item", file_name: "候选人.txt", status: "needs_phone", candidate: { candidate_name: "待补号候选人", source_type: "ai_import", phone: "" } }] } };
  page.on("pageerror", (error) => state.errors.push(error.message));
  await page.context().route("**/*", async (route) => {
    const request = route.request(); const url = new URL(request.url()); const method = request.method();
    if (url.origin === previewOrigin && !url.pathname.startsWith("/api/")) return route.continue();
    if (![previewOrigin, "http://127.0.0.1:8084", "http://127.0.0.1:43129", "http://127.0.0.1:43139", "https://goodhr.58it.cn"].includes(url.origin)) return route.abort();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    let body: any; try { body = request.postDataJSON(); } catch { /* 上传和 GET 无 JSON 正文。 */ }
    state.calls.push({ path: url.pathname, method, body, query: url.searchParams });
    let data: any = { ok: true };
    if (["43129", "43139"].includes(url.port)) {
      // 离线场景也完全拦截端口探测，绝不连接电脑上正在运行的真实本地程序。
      if (state.agentOffline) return route.abort("connectionrefused");
      if (url.pathname === "/health") data = { ok: true, version: "6.2.10.1" };
      else if (url.pathname === "/api/v1/runtime/status") data = { version: "6.2.10.1", node_installed: true, camoufox_installed: true, system_runtime: { ready: true, required: false } };
      else if (url.pathname === "/api/v1/app-update/status") data = { stage: "idle", current_version: "6.2.10.1" };
      else if (url.pathname.startsWith("/api/v1/resume-summary/tasks")) {
        if (url.pathname === "/api/v1/resume-summary/tasks" && method === "POST") {
          if (state.rejectSummaryOnce) { state.rejectSummaryOnce = false; return route.fulfill({ status: 400, headers, json: { ok: false, error: { code: "SUMMARY_INVALID", message: "源资料太长，任务未启动" } } }); }
          state.summaryRequests ||= {};
          state.summaryRequests[body.request_key] ||= { id: "summary-test", status: "queued", ai_summary: "", model: "fake-model", error: "", possibly_charged: false };
          state.summaryTask = state.summaryRequests[body.request_key];
          if (state.failSummaryResponseOnce) { state.failSummaryResponseOnce = false; return route.abort("failed"); }
        }
        else if (url.pathname.endsWith("/cancel")) state.summaryTask.status = "cancelled";
        else if (method === "GET" && ["failed", "cancelled"].includes(state.summaryTask?.status)) { /* 读取失败草稿不会隐式重试AI。 */ }
        else Object.assign(state.summaryTask, { status: "completed", ai_summary: "五年产品经验，熟悉需求整理与交付。" });
        data = { ok: true, data: { task: state.summaryTask } };
      }
      else if (url.pathname.startsWith("/api/v1/resume-import/tasks")) {
        if (url.pathname.endsWith("/confirm")) {
          if (state.calls.filter((call) => call.path.endsWith("/confirm")).length === state.failConfirmAt) return route.fulfill({ status: 503, headers, json: { error: { message: "模拟本地确认暂时没完成" } } });
          for (const item of body.items) {
            const local = state.task.items.find((row: any) => row.id === item.id);
            if (local.status === "saved") continue;
            if (local.status === "partial_success") { local.status = "saved"; continue; }
            const previous = state.candidates.find((row: any) => row.id === item.candidate_id);
            const candidate = { ...previous, ...item.candidate, id: item.candidate_id || `resume-imported-${item.id}`, row_version: previous ? previous.row_version + 1 : 1 };
            if (previous) Object.assign(previous, candidate); else state.candidates.push(candidate);
            local.candidate_id = candidate.id; local.status = "saved";
          }
        }
        data = { ok: true, data: { task: state.task } };
      }
    } else if (url.pathname === "/api/auth/me") data = { user: { ...state.user, role: "super_admin", tenant_id: "test-team", password_set: true } };
    else if (url.pathname === "/api/subscription/status") data = { subscription: { active: true, member_type: "plus", member_name: "Plus", remaining_days: 30, allow_ai: true, allow_auto_reply: true } };
    else if (url.pathname === "/api/config/notification-profile") data = { profile: { completed: true } };
    else if (url.pathname === "/api/runtime/config" || url.pathname === "/api/system/app-config") data = { config: {} };
    else if (url.pathname === "/api/user-flow") data = { flow: { version: 2, state: "completed", stage: "completed", steps: {} } };
    else if (url.pathname.includes("/invitations")) data = { invitations: [] };
    else if (url.pathname === "/api/ai-wallet") data = { wallet: { balance: "100", models: [], default_model: "test" } };
    else if (url.pathname === "/api/positions") data = { positions: state.positions, total: state.positions.length };
    else if (url.pathname === "/api/candidates/duplicates") data = { ok: true, hidden_match: false, matches: body?.phone === existing.phone && body?.exclude_id !== existing.id ? [{ candidate: existing, match_type: "exact", reasons: ["手机号相同"] }] : [] };
    else if (url.pathname === "/api/candidates") {
      if (method === "POST") { const candidate = { ...body, id: "resume-new", row_version: 1 }; state.candidates.push(candidate); data = { ok: true, candidate }; }
      else data = { candidates: state.candidates.map((candidate) => ({ ...candidate, notes: state.notes, recommendations: state.recommendations, recommendation_count: state.recommendations.length })), total: state.candidates.length, real_total: state.candidates.filter((candidate) => !candidate.is_builtin && candidate.id !== "00000000-0000-4000-8000-000000000001").length, builtin_total: state.candidates.filter((candidate) => candidate.is_builtin || candidate.id === "00000000-0000-4000-8000-000000000001").length, page: Number(url.searchParams.get("page")) || 1 };
    } else if (url.pathname.endsWith("/notes")) {
      if (method === "POST") {
        if (state.failFirstNote) { state.failFirstNote = false; return route.fulfill({ status: 503, headers, json: { error: { message: "模拟备注暂时没保存成功" } } }); }
        const note = { id: "new-note", content: body.content, author_name: "测试HR", occurred_at: "2026-10-09T09:15:00+08:00", created_at: "2026-10-10T10:01:00+08:00" }; state.notes.push(note); data = { note };
      } else data = { notes: state.notes };
    } else if (url.pathname.endsWith("/recommendations")) {
      if (method === "POST") {
        // 暂停回包用来验证忙时不能切换岗位；同一岗位只追加历史，其他岗位关系保持原样。
        await state.recommendationSaveGate;
        const previous = state.recommendations.find((item) => item.position_id === body.position_id);
        const event = { id: `event-${state.calls.filter((call) => call.path.endsWith("/recommendations") && call.method === "POST").length}`, from_stage: previous?.stage || "draft", to_stage: body.stage, content: body.content, author_name: "测试HR", occurred_at: "2026-10-08T14:20:00+08:00", created_at: "2026-10-10T10:02:00+08:00" };
        const recommendation = { ...previous, id: previous?.id || `rec-${body.position_id}`, position_id: body.position_id, position_name: previous?.position_name || (body.position_id === "position-two" ? "招聘顾问" : "产品经理"), stage: body.stage, events: [...(previous?.events || []), event] };
        state.recommendations = previous ? state.recommendations.map((item) => item.id === previous.id ? recommendation : item) : [...state.recommendations, recommendation]; data = { recommendation };
      } else data = { recommendations: state.recommendations };
    } else if (url.pathname.startsWith("/api/candidates/")) {
      const id = url.pathname.split("/").pop(); const candidate = state.candidates.find((item) => item.id === id);
      if (method === "PATCH") Object.assign(candidate, body, { row_version: candidate.row_version + 1 });
      data = { candidate };
    }
    return route.fulfill({ status: 200, headers, json: data });
  });
  return state;
}

/** openManual 通过两方式添加弹框进入手动录入。 */
async function openManual(page: Page) {
  await page.getByRole("button", { name: "添加", exact: true }).click();
  const chooser = page.getByRole("dialog", { name: "添加简历", exact: true });
  await expect(chooser.getByText("AI 批量解析", { exact: true })).toBeVisible();
  await chooser.getByRole("button", { name: "继续", exact: true }).click();
  return page.getByRole("dialog", { name: "手动添加简历", exact: true });
}
/** dismissNotices 等待短通知自动消失，避免遮住布局验收截图。 */
async function dismissNotices(page: Page) {
  await expect(page.locator(".ant-notification-notice")).toHaveCount(0);
}

test("本地程序离线时关闭安装提示后仍可搜索、手动添加、备注和推荐，AI与浏览器保持连接检查", async ({ page }) => {
  const state = await mockResumeSession(page); state.agentOffline = true;
  await page.setViewportSize({ width: 1920, height: 1000 });
  await page.goto("/admin/resumes");
  const installNotice = page.getByRole("dialog", { name: "请先安装本地程序", exact: true });
  await expect(installNotice).toBeVisible();
  await installNotice.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(installNotice).not.toBeVisible();
  await expect(page.getByRole("button", { name: "本地程序未连接", exact: true })).toBeVisible();
  await page.getByLabel("简历关键词").fill("离线测试");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page).toHaveURL(/q=%E7%A6%BB%E7%BA%BF%E6%B5%8B%E8%AF%95/);
  await expect.poll(() => state.calls.some((call) => call.path === "/api/candidates" && call.query.get("q") === "离线测试")).toBe(true);
  const editor = await openManual(page);
  await editor.getByLabel("姓名", { exact: true }).fill("离线合成候选人");
  await editor.getByLabel("手机号", { exact: true }).fill("13900139000");
  await editor.getByRole("button", { name: "检查并保存", exact: true }).click();
  await page.getByRole("dialog", { name: "确认入库内容", exact: true }).getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(editor).not.toBeVisible();
  const drawer = page.getByRole("dialog", { name: "离线合成候选人", exact: true });
  await expect(drawer).toBeVisible();
  await page.mouse.move(10, 950); await dismissNotices(page);
  await drawer.getByPlaceholder("比如：意向不错，周五再约一次。").fill("离线也能记的通用备注");
  await drawer.getByRole("button", { name: "加备注", exact: true }).click();
  await expect(drawer.getByText("离线也能记的通用备注", { exact: true })).toBeVisible();
  await page.mouse.move(10, 950); await dismissNotices(page);
  const recommendations = drawer.getByTestId("resume-recommendations-section");
  await expect(recommendations).toBeVisible();
  await recommendations.getByLabel("选择推荐岗位").click(); await page.getByText("产品经理", { exact: true }).last().click();
  await recommendations.getByRole("radio", { name: "已推荐", exact: true }).check();
  await drawer.getByPlaceholder("只记这个岗位的推荐、面试和跟进情况。").fill("离线岗位跟进");
  await drawer.getByRole("button", { name: "记录推荐", exact: true }).click();
  await expect(recommendations.getByTestId("resume-recommendation-history").getByText("离线岗位跟进", { exact: true })).toBeVisible();
  await page.mouse.move(10, 950); await dismissNotices(page);
  await drawer.getByRole("button", { name: "生成摘要", exact: true }).click();
  const summary = page.getByRole("dialog", { name: "AI 简历摘要", exact: true });
  await expect(summary.getByText("本地程序还没连接，打开后再继续。", { exact: true })).toBeVisible();
  await expect(summary.getByRole("button", { name: "开始生成摘要", exact: true })).toBeDisabled();
  await summary.getByRole("button", { name: /收起（草稿保留）$/ }).click();
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  await page.getByRole("button", { name: "添加", exact: true }).click();
  const chooser = page.getByRole("dialog", { name: "添加简历", exact: true });
  await chooser.getByText("AI 批量解析", { exact: true }).click();
  await chooser.getByRole("button", { name: "继续", exact: true }).click();
  const importer = page.getByRole("dialog", { name: "AI 批量解析简历", exact: true });
  await expect(importer.getByText(/手动添加照常可用/)).toBeVisible();
  await importer.getByPlaceholder("也可以粘贴一份简历文本。").fill("合成资料，离线时不应提交解析。");
  await expect(importer.getByRole("button", { name: "开始解析", exact: true })).toBeDisabled();
  await importer.getByRole("button", { name: "取消", exact: true }).click();
  await page.getByRole("button", { name: "打开浏览器", exact: true }).filter({ visible: true }).click();
  await expect(page.getByText("我没叫醒本地程序，你先确认它开着，再点我一次。", { exact: true })).toBeVisible();
  expect(state.calls.filter((call) => call.path === "/api/candidates" && call.method === "POST")).toHaveLength(1);
  expect(state.calls.filter((call) => call.path === "/api/candidates/resume-new/notes" && call.method === "POST")).toHaveLength(1);
  expect(state.calls.filter((call) => call.path === "/api/candidates/resume-new/recommendations" && call.method === "POST")).toHaveLength(1);
  expect(state.calls.filter((call) => call.path.startsWith("/api/v1/") && call.method === "POST")).toHaveLength(0);
  await expect(installNotice).not.toBeVisible();
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "offline-cloud-actions.png"), fullPage: true, animations: "disabled" });
  expect(state.errors).toEqual([]);
});

test("内置示例登录后可读，预留身份缺标记仍不能编辑、摘要或添加记录", async ({ page }) => {
  const state = await mockResumeSession(page);
  const builtinID = "00000000-0000-4000-8000-000000000001";
  // 联系方式使用用户明确授权的共享示例，其余职业资料均为合成虚构信息。
  state.candidates = [{ id: builtinID, candidate_name: "瓜瓜", phone: "17607080935", email: "1224299352@qq.com", source_type: "builtin", is_builtin: true, readonly: true, current_region: "四川成都", expected_region: "四川成都", expected_position: "软件开发", educations: [{ school_name: "示例计算机学院（虚构）", major_name: "软件工程（示例）" }], work_experiences: [{ company_name: "示例软件工作室（虚构）", position_name: "软件开发工程师（示例）", content: "合成工作内容" }] }];
  state.notes = [{ id: "builtin-note", content: "这份简历是管理员内置的只读示例，不可修改或删除。您的简历库需要登录后才能访问，真实简历仍按账号和团队权限隔离。", author_name: "管理员内置" }];
  await page.goto("/admin/resumes");
  await expect(page.getByText("共 0 份简历 · 另有 1 份内置示例", { exact: true })).toBeVisible();
  const row = page.getByRole("row").filter({ has: page.getByRole("button", { name: "瓜瓜", exact: true }) });
  await expect(row.getByRole("button", { name: "编辑", exact: true })).toHaveCount(0);
  await expect(row.getByRole("button", { name: "瓜瓜更多操作" })).toHaveCount(0);
  await row.getByRole("button", { name: "查看", exact: true }).click();
  const workspace = page.getByTestId("resume-detail-layout");
  await expect(page.getByTestId("resume-builtin-notice")).toBeVisible();
  await expect(workspace.getByRole("button", { name: /编辑/ })).toBeDisabled();
  await expect(workspace.getByRole("button", { name: "生成摘要", exact: true })).toBeDisabled();
  await expect(page.getByTestId("resume-notes-section").getByRole("button", { name: "加备注", exact: true })).toBeDisabled();
  await expect(page.getByRole("combobox", { name: "选择推荐岗位" })).toBeDisabled();
  await expect(page.getByRole("radio", { name: "已推荐", exact: true })).toBeDisabled();
  await expect(page.getByRole("textbox", { name: "新增岗位跟进备注" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "保存关联", exact: true })).toBeDisabled();
  await expect(page.getByTestId("resume-notes-section").getByText(state.notes[0].content, { exact: true })).toBeVisible();
  await expect(page.getByTestId("resume-auto-tasks-section")).toBeVisible();
  await expect(workspace.getByText("示例软件工作室（虚构） · 软件开发工程师（示例）", { exact: true })).toBeVisible();
  // 即使旧缓存漏了只读标记，预留ID也不能进入修改或收费流程；独立详情地址保留阅读。
  delete state.candidates[0].is_builtin; delete state.candidates[0].readonly;
  await page.goto(`/admin/resumes/detail?candidate_id=${builtinID}`);
  await expect(page.getByTestId("resume-builtin-notice")).toBeVisible();
  await expect(page.getByRole("button", { name: "生成摘要", exact: true })).toBeDisabled();
  expect(state.calls.filter((call) => call.method !== "GET" && (call.path.startsWith("/api/candidates") || call.path.includes("resume-summary") || call.path.includes("resume-import")))).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

test("手机号必填，备注失败后的重试不会再创建简历", async ({ page }) => {
  const state = await mockResumeSession(page); state.failFirstNote = true;
  await page.goto("/admin/resumes");
  await expect(page.getByRole("button", { name: "清空简历库" })).toHaveCount(0);
  const editor = await openManual(page);
  await editor.getByLabel("姓名", { exact: true }).fill("新候选人");
  await editor.getByRole("button", { name: "检查并保存" }).click();
  await expect(editor.getByText("手机号补一下，我再帮你入库。", { exact: true })).toBeVisible();
  expect(state.calls.filter((call) => call.path === "/api/candidates" && call.method === "POST")).toHaveLength(0);
  await editor.getByLabel("手机号", { exact: true }).fill("13900139000");
  await editor.getByLabel("首条备注", { exact: true }).fill("首备注失败后只重试备注");
  await expect(editor.getByLabel("首条备注", { exact: true })).toHaveValue("首备注失败后只重试备注");
  await editor.getByRole("button", { name: "检查并保存" }).click();
  await page.getByRole("dialog", { name: "确认入库内容" }).getByRole("button", { name: "确认保存" }).click();
  await expect(editor.getByText(/简历已保存，剩余记录没记全/)).toBeVisible();
  await editor.getByRole("button", { name: "重试剩余记录" }).click();
  await expect(editor).not.toBeVisible();
  expect(state.calls.filter((call) => call.path === "/api/candidates" && call.method === "POST")).toHaveLength(1);
  expect(state.notes).toHaveLength(1);
  expect(state.errors).toEqual([]);
});

test("人工摘要收起再打开保留修订和保存幂等键，轮询不会覆盖手改文字", async ({ page }) => {
  const state = await mockResumeSession(page);
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await page.getByRole("button", { name: "生成摘要", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "AI 简历摘要", exact: true });
  await dialog.getByRole("button", { name: "开始生成摘要", exact: true }).click();
  await expect(dialog.getByLabel("摘要草稿")).toHaveValue("五年产品经验，熟悉需求整理与交付。");
  await dialog.getByLabel("摘要草稿").fill("人工修订：保留产品交付经历，删除未经确认的年限。");
  const cacheKey = "goodhr5_resume_summary:resume-user:resume-existing";
  const cached = await page.evaluate((key) => JSON.parse(localStorage.getItem(key) || "null"), cacheKey);
  expect(cached.editedSummary).toBe("人工修订：保留产品交付经历，删除未经确认的年限。");
  expect(cached.saveKey).toBeTruthy();
  await dialog.getByRole("button", { name: /收起（草稿保留）$/ }).click();
  await expect(dialog).not.toBeVisible();
  await page.getByRole("button", { name: "生成摘要", exact: true }).click();
  await expect(dialog.getByLabel("摘要草稿")).toHaveValue(cached.editedSummary);
  await expect.poll(() => state.calls.filter((call) => call.path === "/api/v1/resume-summary/tasks/summary-test").length).toBeGreaterThanOrEqual(2);
  await expect(dialog.getByLabel("摘要草稿")).toHaveValue(cached.editedSummary);
  await dialog.getByRole("button", { name: /保存摘要$/ }).click();
  await expect(dialog).not.toBeVisible();
  const update = state.calls.find((call) => call.path === `/api/candidates/${existing.id}` && call.method === "PATCH");
  expect(update?.body.ai_summary).toBe(cached.editedSummary);
  expect(update?.body.idempotency_key).toBe(cached.saveKey);
  expect(state.calls.filter((call) => call.path === "/api/v1/resume-summary/tasks" && call.method === "POST")).toHaveLength(1);
  expect(state.errors).toEqual([]);
});

test("摘要启动回包丢失后资料变化，用原请求和冻结资料恢复，不新增AI任务", async ({ page }) => {
  const state = await mockResumeSession(page); state.failSummaryResponseOnce = true;
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await page.getByRole("button", { name: "生成摘要", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "AI 简历摘要", exact: true });
  await dialog.getByRole("button", { name: "开始生成摘要", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "恢复上次摘要任务", exact: true })).toBeEnabled();
  const cached = await page.evaluate(() => JSON.parse(localStorage.getItem("goodhr5_resume_summary:resume-user:resume-existing") || "null"));
  expect(cached.pending).toBe(true); expect(cached.id).toBeUndefined();
  expect(cached.source.work_experiences).toEqual(existing.work_experiences);
  for (const field of ["avatar_data_url", "phone", "email", "candidate_name"]) expect(cached.source).not.toHaveProperty(field);
  await dialog.getByRole("button", { name: /收起（草稿保留）$/ }).click();
  state.candidates[0].row_version = 4;
  state.candidates[0].work_experiences = [{ company_name: "更新后的公司", content: "资料已更新" }];
  await page.reload();
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await page.getByRole("button", { name: "生成摘要", exact: true }).click();
  await expect(dialog.getByText(/先恢复上次任务，再决定是否按新资料/)).toBeVisible();
  await expect(dialog.getByRole("button", { name: "开始生成摘要", exact: true })).toHaveCount(0);
  await dialog.getByRole("button", { name: "恢复上次摘要任务", exact: true }).click();
  await expect(dialog.getByLabel("摘要草稿")).toHaveValue("五年产品经验，熟悉需求整理与交付。");
  const requests = state.calls.filter((call) => call.path === "/api/v1/resume-summary/tasks" && call.method === "POST");
  expect(requests).toHaveLength(2);
  expect(requests[0].body.request_key).toBe(requests[1].body.request_key);
  expect(requests[1].body.candidate).toEqual(cached.source);
  expect(Object.keys(state.summaryRequests || {})).toHaveLength(1);
  await expect(dialog.getByRole("button", { name: /保存摘要$/ })).toBeDisabled();
  expect(state.calls.filter((call) => call.path === `/api/candidates/${existing.id}` && call.method === "PATCH")).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

test("摘要草稿实际写入或参数拒绝会解锁按钮且不发AI，手改存储失败保留当前文字", async ({ page }) => {
  const state = await mockResumeSession(page);
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await page.getByRole("button", { name: "生成摘要", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "AI 简历摘要", exact: true });
  await page.evaluate(() => {
    (window as any).__summaryOriginalSetItem = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key.startsWith("goodhr5_resume_summary:")) throw new DOMException("test quota", "QuotaExceededError");
      (window as any).__summaryOriginalSetItem.call(this, key, value);
    };
  });
  await dialog.getByRole("button", { name: "开始生成摘要", exact: true }).click();
  await expect(dialog.getByText(/浏览器草稿没记住，空间可能不够/)).toBeVisible();
  await expect(dialog.getByRole("button", { name: "开始生成摘要", exact: true })).toBeEnabled();
  expect(state.calls.filter((call) => call.path === "/api/v1/resume-summary/tasks" && call.method === "POST")).toHaveLength(0);
  await page.evaluate(() => { Storage.prototype.setItem = (window as any).__summaryOriginalSetItem; });
  state.rejectSummaryOnce = true;
  await dialog.getByRole("button", { name: "开始生成摘要", exact: true }).click();
  await expect(dialog.getByText("源资料太长，任务未启动", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "开始生成摘要", exact: true })).toBeEnabled();
  await expect(dialog.getByRole("button", { name: "恢复上次摘要任务", exact: true })).toHaveCount(0);
  expect(Object.keys(state.summaryRequests || {})).toHaveLength(0);
  await dialog.getByRole("button", { name: "开始生成摘要", exact: true }).click();
  await expect(dialog.getByLabel("摘要草稿")).toHaveValue("五年产品经验，熟悉需求整理与交付。");
  await page.evaluate(() => { Storage.prototype.setItem = function (key, value) { if (key.startsWith("goodhr5_resume_summary:")) throw new DOMException("test quota", "QuotaExceededError"); (window as any).__summaryOriginalSetItem.call(this, key, value); }; });
  await dialog.getByLabel("摘要草稿").fill("存储失败时仍保留的人工修订");
  await expect(dialog.getByText(/修改已留在当前窗口，但浏览器没记住/)).toBeVisible();
  await expect(dialog.getByLabel("摘要草稿")).toHaveValue("存储失败时仍保留的人工修订");
  await page.evaluate(() => { Storage.prototype.setItem = (window as any).__summaryOriginalSetItem; });
  expect(state.calls.filter((call) => call.path === "/api/v1/resume-summary/tasks" && call.method === "POST")).toHaveLength(2);
  expect(Object.keys(state.summaryRequests || {})).toHaveLength(1);
  expect(state.errors).toEqual([]);
});

test("失败摘要已过期时直接生成当前简历新任务，不重试旧资料", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.summaryTask = { id: "summary-old", status: "failed", ai_summary: "", model: "fake-model", error: "上次请求中断", possibly_charged: true };
  state.candidates[0].work_experiences = [{ company_name: "当前公司", position_name: "产品负责人", content: "最新工作内容" }];
  await page.goto("/admin/resumes");
  await page.evaluate(() => localStorage.setItem("goodhr5_resume_summary:resume-user:resume-existing", JSON.stringify({ id: "summary-old", key: "old-source-request", version: 2 })));
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await page.getByRole("button", { name: "生成摘要", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "AI 简历摘要", exact: true });
  await expect(dialog.getByText("上次请求中断", { exact: true })).toBeVisible();
  await expect(dialog.getByText(/这份简历在生成后有更新/)).toBeVisible();
  await expect(dialog.getByRole("button", { name: "确认重试摘要", exact: true })).toHaveCount(0);
  await dialog.getByRole("button", { name: "按当前简历重新生成摘要（可能再次计费）", exact: true }).click();
  await expect(dialog.getByLabel("摘要草稿")).toHaveValue("五年产品经验，熟悉需求整理与交付。");
  const local = state.calls.find((call) => call.path === "/api/v1/resume-summary/tasks" && call.method === "POST");
  expect(local?.body.request_key).not.toBe("old-source-request");
  expect(local?.body.candidate.work_experiences).toEqual(state.candidates[0].work_experiences);
  expect(state.calls.filter((call) => call.path.endsWith("/retry") && call.path.includes("resume-summary"))).toHaveLength(0);
  await dialog.getByRole("button", { name: /保存摘要$/ }).click();
  await expect(dialog).not.toBeVisible();
  const update = state.calls.find((call) => call.path === `/api/candidates/${existing.id}` && call.method === "PATCH");
  expect(update?.body.row_version).toBe(3);
  expect(state.calls.filter((call) => call.path === "/api/v1/resume-summary/tasks" && call.method === "POST")).toHaveLength(1);
  expect(state.errors).toEqual([]);
});

test("本地AI摘要先预览再保存，只更新摘要和版本且不发送头像联系方式", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.candidates[0].avatar_data_url = syntheticAvatar;
  state.candidates[0].email = "synthetic@example.invalid";
  state.candidates[0].ai_summary = "原有摘要保留到明确保存";
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await page.getByRole("button", { name: "生成摘要", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "AI 简历摘要", exact: true });
  await expect(dialog.getByText(/可能产生AI费用/)).toBeVisible();
  expect(state.calls.filter((call) => call.path === "/api/v1/resume-summary/tasks")).toHaveLength(0);
  await dialog.getByRole("button", { name: "开始生成摘要", exact: true }).click();
  await expect(dialog.getByLabel("摘要草稿")).toHaveValue("五年产品经验，熟悉需求整理与交付。");
  const local = state.calls.find((call) => call.path === "/api/v1/resume-summary/tasks" && call.method === "POST");
  expect(local?.body.candidate.work_experiences).toEqual(existing.work_experiences);
  for (const field of ["avatar_data_url", "phone", "email", "candidate_name", "ai_summary", "notes", "recommendations"]) expect(local?.body.candidate).not.toHaveProperty(field);
  expect(state.calls.filter((call) => call.path === `/api/candidates/${existing.id}` && call.method === "PATCH")).toHaveLength(0);
  await dialog.getByLabel("摘要草稿").fill("经人工核对的产品经验摘要");
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "summary-draft.png"), fullPage: true });
  await dialog.getByRole("button", { name: /保存摘要$/ }).click();
  await expect(dialog).not.toBeVisible();
  const update = state.calls.find((call) => call.path === `/api/candidates/${existing.id}` && call.method === "PATCH");
  expect(Object.keys(update?.body).sort()).toEqual(["ai_summary", "idempotency_key", "row_version"]);
  expect(update?.body.row_version).toBe(3);
  expect(update?.body.ai_summary).toBe("经人工核对的产品经验摘要");
  expect(state.candidates[0].work_experiences).toEqual(existing.work_experiences);
  expect(state.candidates[0].email).toBe("synthetic@example.invalid");
  expect(state.calls.filter((call) => call.path.endsWith("/notes") && call.method === "POST")).toHaveLength(0);
  expect(state.calls.filter((call) => call.path === "/api/v1/resume-summary/tasks" && call.method === "POST")).toHaveLength(1);
  expect(state.errors).toEqual([]);
});

test("精确重复先比较更新，空经历不会覆盖原简历", async ({ page }) => {
  const state = await mockResumeSession(page);
  await page.goto("/admin/resumes"); const editor = await openManual(page);
  await editor.getByLabel("姓名", { exact: true }).fill("更新候选人");
  await editor.getByLabel("手机号", { exact: true }).fill(existing.phone);
  await editor.getByRole("button", { name: "检查并保存" }).click();
  await expect(editor.getByText(/手机号已有简历/)).toBeVisible();
  await editor.getByRole("button", { name: "比较更新" }).click();
  const updater = page.getByRole("dialog", { name: "维护简历 · 原有候选人" });
  await updater.getByRole("button", { name: "检查并保存" }).click();
  await page.getByRole("dialog", { name: "确认更新内容" }).getByRole("button", { name: "确认保存" }).click();
  await expect(updater).not.toBeVisible();
  const update = state.calls.find((call) => call.path === `/api/candidates/${existing.id}` && call.method === "PATCH");
  expect(update?.body.row_version).toBe(3);
  expect(update?.body).not.toHaveProperty("work_experiences");
  expect(update?.body).not.toHaveProperty("source_type");
  expect(state.calls.filter((call) => call.path === "/api/candidates" && call.method === "POST")).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

test("内嵌头像在列表和详情实际加载，坏图和空图回退到姓名首字", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.candidates[0].avatar_data_url = syntheticAvatar;
  state.candidates.push({ ...existing, id: "resume-broken-avatar", candidate_name: "坏图候选人", phone: "13700137000", avatar_data_url: "data:image/png;base64,YmFk" }, { ...existing, id: "resume-no-avatar", candidate_name: "空图候选人", phone: "13900139000" });
  await page.goto("/admin/resumes");
  const row = page.getByRole("row").filter({ has: page.getByRole("button", { name: "原有候选人", exact: true }) });
  const listAvatar = row.getByRole("img", { name: "原有候选人头像", exact: true });
  await expect(listAvatar).toBeVisible(); await expect(listAvatar).toHaveAttribute("src", syntheticAvatar);
  await expect.poll(() => listAvatar.evaluate((image: HTMLImageElement) => image.naturalWidth)).toBe(16);
  await expect(row.locator(".ant-avatar-string")).toHaveCount(0);
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "avatar-list.png"), fullPage: true, animations: "disabled" });
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  const detailAvatar = drawer.getByRole("img", { name: "原有候选人头像", exact: true });
  await expect(detailAvatar).toBeVisible();
  await expect.poll(() => detailAvatar.evaluate((image: HTMLImageElement) => image.naturalWidth)).toBe(16);
  await expect(drawer.locator(".ant-avatar-string")).toHaveCount(0);
  await page.screenshot({ path: resolve(screenshotDirectory, "avatar-detail.png"), fullPage: true, animations: "disabled" });
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  for (const name of ["坏图候选人", "空图候选人"]) {
    const fallbackRow = page.getByRole("row").filter({ has: page.getByRole("button", { name, exact: true }) });
    await expect(fallbackRow.locator(".ant-avatar-string")).toHaveText(name.slice(0, 1));
    await expect(fallbackRow.getByRole("img", { name: `${name}头像`, exact: true })).toHaveCount(0);
    await page.getByRole("button", { name, exact: true }).click();
    const fallbackDrawer = page.getByRole("dialog", { name, exact: true });
    await expect(fallbackDrawer.locator(".ant-avatar-string")).toHaveText(name.slice(0, 1));
    await fallbackDrawer.getByRole("button", { name: "关闭", exact: true }).click();
  }
  expect(state.errors).toEqual([]);
});

test("手动删除末经历和清空邮箱默认保留，勾选后仅清空明确选择的字段", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.candidates[0].email = "preserved@example.invalid"; state.candidates[0].current_region = "上海";
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  for (const confirmClear of [false, true]) {
    await drawer.getByRole("button", { name: /编辑$/ }).click();
    const editor = page.getByRole("dialog", { name: "维护简历 · 原有候选人", exact: true });
    await editor.getByLabel("邮箱", { exact: true }).fill("");
    await editor.getByLabel("当前地区", { exact: true }).fill("");
    const removeExperience = editor.getByRole("button", { name: /移除这段$/ });
    if (!await removeExperience.isVisible()) await editor.getByText("工作经历", { exact: true }).click();
    await removeExperience.click();
    await editor.getByRole("button", { name: "检查并保存", exact: true }).click();
    const preview = page.getByRole("dialog", { name: "确认更新内容", exact: true });
    await expect(preview.getByRole("checkbox", { name: "确认清空邮箱", exact: true })).not.toBeChecked();
    await expect(preview.getByRole("checkbox", { name: "确认清空工作经历", exact: true })).not.toBeChecked();
    await expect(preview.getByRole("checkbox", { name: "确认清空姓名", exact: true })).toHaveCount(0);
    await expect(preview.getByRole("checkbox", { name: "确认清空手机号", exact: true })).toHaveCount(0);
    if (confirmClear) {
      await preview.getByRole("checkbox", { name: "确认清空邮箱", exact: true }).check();
      await preview.getByRole("checkbox", { name: "确认清空工作经历", exact: true }).check();
    }
    await preview.getByRole("button", { name: "确认保存", exact: true }).click();
    await expect(editor).not.toBeVisible();
    const patches = state.calls.filter((call) => call.path === `/api/candidates/${existing.id}` && call.method === "PATCH");
    const latest = patches[patches.length - 1].body;
    expect(latest).not.toHaveProperty("current_region");
    expect(state.candidates[0].current_region).toBe("上海");
    if (confirmClear) {
      expect(latest.email).toBe(""); expect(latest.work_experiences).toEqual([]); expect(latest.row_version).toBe(4);
      expect(Object.keys(latest).filter((key) => !["idempotency_key", "row_version"].includes(key)).sort()).toEqual(["email", "work_experiences"]);
      expect(state.candidates[0].email).toBe(""); expect(state.candidates[0].work_experiences).toEqual([]);
    } else {
      expect(latest).not.toHaveProperty("email"); expect(latest).not.toHaveProperty("work_experiences");
      expect(state.candidates[0].email).toBe("preserved@example.invalid"); expect(state.candidates[0].work_experiences).toEqual(existing.work_experiences);
    }
  }
  expect(state.calls.filter((call) => call.path === `/api/candidates/${existing.id}` && call.method === "PATCH")).toHaveLength(2);
  expect(state.errors).toEqual([]);
});

test("查询写回URL，详情抽屉独立保存备注和人工推荐，窄屏无横向溢出", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.candidates[0].expected_salary_text = "面议（按年包沟通）";
  state.candidates[0].events = [{ event_type: "manual_note", message_text: "通用备注仅在备注接口保存", created_at: "2026-10-10T10:00:00+08:00" }];
  await page.setViewportSize({ width: 1920, height: 1000 });
  await page.goto("/admin/resumes?phone=3800&position_id=position-one");
  mkdirSync(screenshotDirectory, { recursive: true });
  await expect(page.getByLabel("手机号搜索")).toHaveValue("3800");
  await expect(page.getByRole("button", { name: "原有候选人", exact: true })).toBeVisible();
  await expect(page.getByRole("dialog", { name: "准备必要组件" })).not.toBeVisible();
  await page.screenshot({ path: resolve(screenshotDirectory, "list-desktop.png"), fullPage: true, animations: "disabled" });
  await page.getByLabel("简历关键词").fill("产品"); await page.getByRole("button", { name: /查询$/ }).click();
  await expect(page).toHaveURL(/q=%E4%BA%A7%E5%93%81/);
  await expect.poll(() => state.calls.some((call) => call.path === "/api/candidates" && call.method === "GET" && call.query.get("q") === "产品" && call.query.get("phone") === "3800" && call.query.get("position_id") === "position-one")).toBe(true);
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  await expect(drawer.getByText("未填写", { exact: true }).first()).toBeVisible();
  await expect(drawer.getByText("0–0K", { exact: true })).toHaveCount(0);
  await expect(drawer.getByText("面议（按年包沟通）", { exact: true })).toBeVisible();
  const notes = drawer.getByTestId("resume-notes-section");
  const recommendations = drawer.getByTestId("resume-recommendations-section");
  const automaticTasks = drawer.getByTestId("resume-auto-tasks-section");
  for (const section of [notes, recommendations, automaticTasks]) await expect(section).toBeVisible();
  await expect(drawer.getByRole("tab")).toHaveCount(0);
  await notes.getByPlaceholder("比如：意向不错，周五再约一次。").fill("通用备注仅在备注接口保存");
  await notes.getByRole("button", { name: "加备注", exact: true }).click();
  await expect(notes.getByText("通用备注仅在备注接口保存", { exact: true })).toBeVisible();
  await expect(notes.getByText(/测试HR · 2026\/10\/09/)).toBeVisible();
  await recommendations.getByLabel("选择推荐岗位").click(); await page.getByText("产品经理", { exact: true }).last().click();
  await recommendations.getByRole("radio", { name: "已推荐", exact: true }).check();
  await recommendations.getByPlaceholder("只记这个岗位的推荐、面试和跟进情况。").fill("岗位专属备注");
  await recommendations.getByRole("button", { name: "记录推荐", exact: true }).click();
  await expect(recommendations.getByTestId("resume-recommendation-history").getByText("岗位专属备注", { exact: true })).toBeVisible();
  await expect(recommendations.getByText(/测试HR · 2026\/10\/08/)).toBeVisible();
  await expect(recommendations.getByText("岗位专属备注", { exact: true })).toBeVisible();
  await expect(recommendations.getByText("通用备注仅在备注接口保存", { exact: true })).toHaveCount(0);
  await expect(notes.getByText("岗位专属备注", { exact: true })).toHaveCount(0);
  await dismissNotices(page);
  await page.screenshot({ path: resolve(screenshotDirectory, "detail-desktop.png"), fullPage: true, animations: "disabled" });
  expect(state.recommendations[0].stage).toBe("recommended"); expect(state.recommendations[0].events[0].content).toBe("岗位专属备注");
  await page.setViewportSize({ width: 390, height: 844 });
  const candidateHeading = drawer.getByRole("heading", { name: "原有候选人", exact: true });
  await candidateHeading.scrollIntoViewIfNeeded();
  await expect(candidateHeading).toBeInViewport();
  const overflow = await page.locator("body").boundingBox(); expect(overflow?.width).toBeLessThanOrEqual(390);
  const documentWidth = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(documentWidth).toBeLessThanOrEqual(390);
  await page.screenshot({ path: resolve(screenshotDirectory, "detail-mobile.png"), fullPage: true, animations: "disabled" });
  await expect(automaticTasks.getByText("通用备注仅在备注接口保存", { exact: true })).toHaveCount(0);
  expect(state.errors).toEqual([]);
});

test("多岗位卡片带入阶段，保存时禁切，选中岗位历史直展且通用备注独立", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.positions = [{ id: "position-one", name: "产品经理" }, { id: "position-two", name: "招聘顾问" }];
  const commonNote = "合成通用备注：只用于候选人沟通";
  state.notes = [{ id: "note-independent", content: commonNote, author_name: "测试HR", occurred_at: "2026-10-09T09:15:00+08:00" }];
  state.candidates[0].events = [
    { event_type: "manual_note", message_text: commonNote, created_at: "2026-10-10T10:00:00+08:00" },
    { event_type: "resume_read", message_text: "合成自动任务：已读取详情", created_at: "2026-10-10T10:05:00+08:00" },
  ];
  state.recommendations = [
    { id: "rec-position-one", position_id: "position-one", position_name: "产品经理", stage: "draft", events: [{ id: "seed-event-one", from_stage: "draft", to_stage: "draft", content: "产品经理原有岗位历史", author_name: "测试HR", occurred_at: "2026-10-08T09:00:00+08:00" }] },
    { id: "rec-position-two", position_id: "position-two", position_name: "招聘顾问", stage: "contacting", events: [{ id: "seed-event-two", from_stage: "recommended", to_stage: "contacting", content: "招聘顾问原有岗位历史", author_name: "测试HR", occurred_at: "2026-10-08T11:00:00+08:00" }] },
  ];
  await page.setViewportSize({ width: 1920, height: 1000 });
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  const notes = drawer.getByTestId("resume-notes-section");
  const recommendations = drawer.getByTestId("resume-recommendations-section");
  const automaticTasks = drawer.getByTestId("resume-auto-tasks-section");
  for (const section of [notes, recommendations, automaticTasks]) await expect(section).toBeVisible();
  await expect(drawer.getByRole("tab")).toHaveCount(0);
  await expect(recommendations.getByRole("button", { name: "继续跟进", exact: true })).toHaveCount(0);
  const history = recommendations.getByTestId("resume-recommendation-history");
  await expect(history.getByText("产品经理原有岗位历史", { exact: true })).toHaveCount(1);
  await expect(history.getByText("产品经理原有岗位历史", { exact: true })).toBeVisible();
  await expect(history.getByText("招聘顾问原有岗位历史", { exact: true })).toHaveCount(0);
  const firstCard = recommendations.getByRole("button", { name: "选择推荐记录 产品经理", exact: true });
  const secondCard = recommendations.getByRole("button", { name: "选择推荐记录 招聘顾问", exact: true });
  const followUp = recommendations.getByPlaceholder("只记这个岗位的推荐、面试和跟进情况。");
  await secondCard.click();
  await expect(secondCard).toHaveAttribute("aria-pressed", "true");
  await expect(firstCard).toHaveAttribute("aria-pressed", "false");
  await expect(recommendations.getByRole("combobox", { name: "选择推荐岗位", exact: true }).locator("..")).toHaveText("招聘顾问");
  await expect(recommendations.getByRole("radio", { name: "沟通中", exact: true })).toBeChecked();
  await expect(history.getByText("招聘顾问原有岗位历史", { exact: true })).toBeVisible();
  await expect(history.getByText("产品经理原有岗位历史", { exact: true })).toHaveCount(0);
  await expect(followUp).toHaveValue("");
  await recommendations.getByRole("radio", { name: "面试中", exact: true }).check();
  await followUp.fill("招聘顾问新跟进：已约面试");
  // 重复点当前卡片不能重置尚未提交的阶段、备注或发起保存。
  await secondCard.click();
  await expect(recommendations.getByRole("radio", { name: "面试中", exact: true })).toBeChecked();
  await expect(followUp).toHaveValue("招聘顾问新跟进：已约面试");
  expect(state.calls.filter((call) => call.path.endsWith("/recommendations") && call.method === "POST")).toHaveLength(0);
  let releaseSave!: () => void;
  state.recommendationSaveGate = new Promise<void>((resolveSave) => { releaseSave = resolveSave; });
  try {
    await recommendations.getByRole("button", { name: "记录跟进", exact: true }).click();
    await expect.poll(() => state.calls.filter((call) => call.path.endsWith("/recommendations") && call.method === "POST").length).toBe(1);
    await expect(firstCard).toHaveAttribute("aria-disabled", "true");
    await expect(secondCard).toHaveAttribute("aria-disabled", "true");
    await expect(recommendations.getByRole("radio", { name: "已推荐", exact: true })).toBeDisabled();
    await expect(followUp).toBeDisabled();
    // 卡片是自定义键盘控件，分别验证鼠标与 Enter 在忙时都不会切换。
    await firstCard.click({ force: true });
    await firstCard.press("Enter");
    await expect(secondCard).toHaveAttribute("aria-pressed", "true");
    await expect(firstCard).toHaveAttribute("aria-pressed", "false");
    await expect(recommendations.getByRole("combobox", { name: "选择推荐岗位", exact: true }).locator("..")).toHaveText("招聘顾问");
    await expect(recommendations.getByRole("radio", { name: "面试中", exact: true })).toBeChecked();
    await expect(followUp).toHaveValue("招聘顾问新跟进：已约面试");
    expect(state.recommendations[1].events).toHaveLength(1);
  } finally {
    releaseSave(); state.recommendationSaveGate = undefined;
  }
  await expect(firstCard).toHaveAttribute("aria-disabled", "false");
  await expect(recommendations.getByText("招聘顾问新跟进：已约面试", { exact: true })).toBeVisible();
  await expect(followUp).toHaveValue("");
  expect(state.recommendations[0].stage).toBe("draft");
  expect(state.recommendations[0].events).toHaveLength(1);
  expect(state.recommendations[1].stage).toBe("interview");
  expect(state.recommendations[1].events).toHaveLength(2);
  await firstCard.click();
  await expect(firstCard).toHaveAttribute("aria-pressed", "true");
  await expect(recommendations.getByRole("combobox", { name: "选择推荐岗位", exact: true }).locator("..")).toHaveText("产品经理");
  await expect(recommendations.getByRole("radio", { name: "待推荐", exact: true })).toBeChecked();
  await recommendations.getByRole("radio", { name: "已推荐", exact: true }).check();
  await followUp.fill("产品经理新跟进：确认推荐");
  await recommendations.getByRole("button", { name: "记录推荐", exact: true }).click();
  await expect(recommendations.getByText("产品经理新跟进：确认推荐", { exact: true })).toBeVisible();
  for (const content of ["产品经理原有岗位历史", "产品经理新跟进：确认推荐"]) {
    await expect(history.getByText(content, { exact: true })).toHaveCount(1);
    await expect(history.getByText(content, { exact: true })).toBeVisible();
  }
  await expect(history.getByText("招聘顾问新跟进：已约面试", { exact: true })).toHaveCount(0);
  await secondCard.click();
  for (const content of ["招聘顾问原有岗位历史", "招聘顾问新跟进：已约面试"]) {
    await expect(history.getByText(content, { exact: true })).toHaveCount(1);
    await expect(history.getByText(content, { exact: true })).toBeVisible();
  }
  await expect(history.getByText("产品经理新跟进：确认推荐", { exact: true })).toHaveCount(0);
  await expect(notes.getByText(commonNote, { exact: true })).toHaveCount(1);
  await expect(recommendations.getByText(commonNote, { exact: true })).toHaveCount(0);
  await expect(automaticTasks.getByText(commonNote, { exact: true })).toHaveCount(0);
  await expect(automaticTasks.getByText("合成自动任务：已读取详情", { exact: true })).toBeVisible();
  const saved = state.calls.filter((call) => call.path.endsWith("/recommendations") && call.method === "POST");
  expect(saved.map((call) => [call.body.position_id, call.body.stage, call.body.content])).toEqual([
    ["position-two", "interview", "招聘顾问新跟进：已约面试"],
    ["position-one", "recommended", "产品经理新跟进：确认推荐"],
  ]);
  expect(state.recommendations).toHaveLength(2);
  expect(state.recommendations[1].stage).toBe("interview");
  expect(state.recommendations[1].events).toHaveLength(2);
  expect(state.calls.filter((call) => call.path.endsWith("/notes") && call.method === "POST")).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

test("推荐显示偏好重载保留并按账号隔离，显式推荐入口临时展开且隐藏保留草稿", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.recommendations = [{ id: "rec-position-one", position_id: "position-one", position_name: "产品经理", stage: "contacting", events: [{ id: "toggle-history", from_stage: "recommended", to_stage: "contacting", content: "合成岗位历史：已沟通", author_name: "测试HR", occurred_at: "2026-10-08T11:00:00+08:00" }] }];
  state.notes = [{ id: "toggle-note", content: "合成通用备注：隐藏推荐也能看", author_name: "测试HR", occurred_at: "2026-10-09T09:15:00+08:00" }];
  state.candidates[0].events = [{ event_type: "resume_read", message_text: "合成自动任务：隐藏推荐也能看", created_at: "2026-10-10T10:05:00+08:00" }];
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  const recommendations = drawer.getByTestId("resume-recommendations-section");
  const content = recommendations.getByTestId("resume-recommendation-content");
  const followUp = recommendations.getByPlaceholder("只记这个岗位的推荐、面试和跟进情况。");
  const notes = drawer.getByTestId("resume-notes-section");
  const automaticTasks = drawer.getByTestId("resume-auto-tasks-section");
  await expect(content).toBeVisible();
  await recommendations.getByRole("button", { name: "选择推荐记录 产品经理", exact: true }).click();
  await recommendations.getByRole("radio", { name: "面试中", exact: true }).check();
  await followUp.fill("合成未提交跟进：下周面试");
  await notes.getByPlaceholder("比如：意向不错，周五再约一次。").fill("合成未提交通用备注");
  await recommendations.getByRole("button", { name: "隐藏推荐", exact: true }).click();
  await expect(content).not.toBeVisible();
  await expect(recommendations.getByRole("button", { name: "显示推荐", exact: true })).toHaveAttribute("aria-expanded", "false");
  await expect(notes.getByText("合成通用备注：隐藏推荐也能看", { exact: true })).toBeVisible();
  await expect(automaticTasks.getByText("合成自动任务：隐藏推荐也能看", { exact: true })).toBeVisible();
  await recommendations.getByRole("button", { name: "显示推荐", exact: true }).click();
  await expect(content).toBeVisible();
  await expect(recommendations.getByRole("button", { name: "隐藏推荐", exact: true })).toHaveAttribute("aria-expanded", "true");
  await expect(followUp).toHaveValue("合成未提交跟进：下周面试");
  await expect(recommendations.getByRole("radio", { name: "面试中", exact: true })).toBeChecked();
  await expect(notes.getByPlaceholder("比如：意向不错，周五再约一次。")).toHaveValue("合成未提交通用备注");
  await recommendations.getByRole("button", { name: "隐藏推荐", exact: true }).click();
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  await page.reload();
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await expect(content).not.toBeVisible();
  await expect(recommendations.getByRole("button", { name: "显示推荐", exact: true })).toBeVisible();
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  // 岗位记录入口需要本次展开，但不能把账号之前的隐藏偏好改成展示。
  await page.getByRole("button", { name: "1 个岗位记录", exact: true }).click();
  await expect(content).toBeVisible();
  await expect(recommendations.getByTestId("resume-recommendation-history").getByText("合成岗位历史：已沟通", { exact: true })).toBeVisible();
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  await page.reload();
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await expect(content).not.toBeVisible();
  await expect(recommendations.getByRole("button", { name: "显示推荐", exact: true })).toBeVisible();
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  // 同一浏览器切到另一模拟账号，第一账号的隐藏偏好不应影响新账号。
  state.user = { id: "resume-user-second", email: "second@example.invalid" };
  await page.reload();
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await expect(content).toBeVisible();
  await recommendations.getByRole("button", { name: "隐藏推荐", exact: true }).click();
  await expect(content).not.toBeVisible();
  await recommendations.getByRole("button", { name: "显示推荐", exact: true }).click();
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  await page.reload();
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await expect(content).toBeVisible();
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  state.user = { id: "resume-user", email: "test@example.invalid" };
  await page.reload();
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  await expect(content).not.toBeVisible();
  await expect(notes.getByText("合成通用备注：隐藏推荐也能看", { exact: true })).toBeVisible();
  expect(state.calls.filter((call) => call.path.endsWith("/recommendations") && call.method === "POST")).toHaveLength(0);
  expect(state.calls.filter((call) => call.path.endsWith("/notes") && call.method === "POST")).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

test("多岗位横向条可真实滚动和选择，窄屏页面不横向溢出", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.positions = Array.from({ length: 8 }, (_, index) => ({ id: `strip-position-${index}`, name: `合成岗位${index + 1} · 招聘顾问` }));
  // 最后一条模拟旧历史缺少目标阶段，当前阶段与来源相同，不应显示重复的阶段箭头。
  state.recommendations = state.positions.map((position, index) => ({ id: `strip-rec-${index}`, position_id: position.id, position_name: position.name, stage: "contacting", events: [{ id: `strip-event-${index}`, from_stage: index === state.positions.length - 1 ? "contacting" : "recommended", to_stage: index === state.positions.length - 1 ? undefined : "contacting", content: `合成岗位${index + 1}唯一跟进历史`, author_name: "测试HR", occurred_at: "2026-10-08T11:00:00+08:00" }] }));
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  const recommendations = drawer.getByTestId("resume-recommendations-section");
  const picker = recommendations.getByTestId("resume-recommendation-picker");
  const history = recommendations.getByTestId("resume-recommendation-history");
  await expect(picker).toBeVisible();
  await expect(picker.getByRole("button", { name: /^选择推荐记录 合成岗位/ })).toHaveCount(8);
  await expect(history.getByText("合成岗位1唯一跟进历史", { exact: true })).toBeVisible();
  await expect.poll(() => picker.evaluate((element) => element.scrollWidth > element.clientWidth)).toBe(true);
  await picker.scrollIntoViewIfNeeded();
  const pickerBox = await picker.boundingBox();
  if (!pickerBox) throw new Error("合成岗位横向选择条没有可操作的尺寸。");
  await page.mouse.move(pickerBox.x + pickerBox.width / 2, pickerBox.y + pickerBox.height / 2);
  await page.mouse.wheel(900, 0);
  await expect.poll(() => picker.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0);
  const lastCard = picker.getByRole("button", { name: "选择推荐记录 合成岗位8 · 招聘顾问", exact: true });
  await lastCard.click();
  await expect(lastCard).toHaveAttribute("aria-pressed", "true");
  await expect(history.getByText("合成岗位8唯一跟进历史", { exact: true })).toBeVisible();
  await expect(history.getByText("沟通中", { exact: true })).toBeVisible();
  await expect(history.getByText("沟通中 → 沟通中", { exact: true })).toHaveCount(0);
  await expect(history.getByText("合成岗位1唯一跟进历史", { exact: true })).toHaveCount(0);
  const documentWidth = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(documentWidth).toBeLessThanOrEqual(390);
  const bodyBox = await page.locator("body").boundingBox();
  expect(bodyBox?.width).toBeLessThanOrEqual(390);
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "recommendation-strip-mobile.png"), fullPage: true, animations: "disabled" });
  expect(state.calls.filter((call) => call.path.endsWith("/recommendations") && call.method === "POST")).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

test("重新打开恢复本地草稿，缺手机号只能补齐后逐份确认", async ({ page }) => {
  const state = await mockResumeSession(page);
  await page.goto("/admin/resumes");
  await page.evaluate(() => localStorage.setItem("goodhr5_resume_import:resume-user", "import-test"));
  await page.getByRole("button", { name: "添加", exact: true }).click();
  const chooser = page.getByRole("dialog", { name: "添加简历", exact: true });
  await chooser.getByText("AI 批量解析", { exact: true }).click(); await chooser.getByRole("button", { name: "继续", exact: true }).click();
  const importer = page.getByRole("dialog", { name: "AI 批量解析简历", exact: true });
  await expect(importer.getByText("待补手机号", { exact: true })).toBeVisible();
  await expect(importer.getByRole("button", { name: "确认无重复的完整草稿" })).toBeDisabled();
  await importer.getByRole("button", { name: "预览 / 补充" }).click();
  const editor = page.getByRole("dialog", { name: "确认解析草稿", exact: true });
  await editor.getByLabel("手机号", { exact: true }).fill("13700137000"); await editor.getByRole("button", { name: "检查并保存" }).click();
  await page.getByRole("dialog", { name: "确认入库内容" }).getByRole("button", { name: "确认保存" }).click();
  await expect(importer.getByText("已入库", { exact: true }).first()).toBeVisible();
  expect(state.calls.filter((call) => call.path.endsWith("/confirm"))).toHaveLength(1);
  expect(state.errors).toEqual([]);
});

test("AI草稿比较更新只提交预览中的有效改动，空邮箱和空经历保留已有内容", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.candidates[0].email = "preserved@example.invalid";
  state.task.items[0] = { id: "import-item", file_name: "比较更新.txt", status: "ready", candidate: { candidate_name: "解析候选人", phone: existing.phone, email: "", work_experiences: [], expected_salary_min: null, source_type: "ai_import", avatar_data_url: syntheticAvatar } };
  await page.goto("/admin/resumes");
  await page.evaluate(() => localStorage.setItem("goodhr5_resume_import:resume-user", "import-test"));
  await page.getByRole("button", { name: "添加", exact: true }).click();
  const chooser = page.getByRole("dialog", { name: "添加简历", exact: true });
  await chooser.getByText("AI 批量解析", { exact: true }).click(); await chooser.getByRole("button", { name: "继续", exact: true }).click();
  const importer = page.getByRole("dialog", { name: "AI 批量解析简历", exact: true });
  await importer.getByRole("button", { name: "预览 / 补充" }).click();
  const editor = page.getByRole("dialog", { name: "确认解析草稿", exact: true });
  await editor.getByRole("button", { name: "检查并保存" }).click();
  await editor.getByRole("button", { name: "比较更新" }).click();
  const updater = page.getByRole("dialog", { name: "维护简历 · 原有候选人", exact: true });
  await updater.getByRole("button", { name: "检查并保存" }).click();
  const preview = page.getByRole("dialog", { name: "确认更新内容", exact: true });
  await expect(preview.getByText("邮箱", { exact: true })).toHaveCount(0);
  await expect(preview.getByText("工作经历", { exact: true })).toHaveCount(0);
  await expect(preview.getByRole("checkbox", { name: /^确认清空/ })).toHaveCount(0);
  await expect(preview.getByText("头像", { exact: true })).toBeVisible();
  await expect(preview.getByRole("img", { name: "拟保存头像", exact: true })).toBeVisible();
  await expect(preview).not.toContainText("data:image/png;base64");
  await preview.getByRole("button", { name: "确认保存" }).click();
  await expect(importer.getByText("已入库", { exact: true }).first()).toBeVisible();
  const confirms = state.calls.filter((call) => call.path.endsWith("/confirm"));
  expect(confirms).toHaveLength(1);
  const submitted = confirms[0].body.items[0];
  expect(submitted.candidate_id).toBe(existing.id);
  expect(submitted.candidate).toEqual({ candidate_name: "解析候选人", phone: existing.phone, row_version: 3, avatar_data_url: syntheticAvatar });
  expect(submitted.candidate).not.toHaveProperty("email");
  expect(submitted.candidate).not.toHaveProperty("work_experiences");
  expect(state.candidates).toHaveLength(1);
  expect(state.candidates[0].email).toBe("preserved@example.invalid");
  expect(state.candidates[0].work_experiences).toEqual(existing.work_experiences);
  expect(state.candidates[0].source_type).toBe("automatic");
  expect(state.errors).toEqual([]);
});

test("大正文批次逐份确认，途中失败后只继续未完成草稿", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.failConfirmAt = 2;
  state.task = { id: "import-test", status: "completed", total: 8, processed: 8, items: Array.from({ length: 8 }, (_, index) => ({ id: `large-item-${index}`, file_name: `大正文${index + 1}.txt`, status: "ready", candidate: { candidate_name: `批量候选人${index + 1}`, phone: `1380013810${index}`, source_type: "ai_import", raw_text: "字".repeat(180000) } })) };
  await page.goto("/admin/resumes");
  await page.evaluate(() => localStorage.setItem("goodhr5_resume_import:resume-user", "import-test"));
  await page.getByRole("button", { name: "添加", exact: true }).click();
  const chooser = page.getByRole("dialog", { name: "添加简历", exact: true });
  await chooser.getByText("AI 批量解析", { exact: true }).click(); await chooser.getByRole("button", { name: "继续", exact: true }).click();
  const importer = page.getByRole("dialog", { name: "AI 批量解析简历", exact: true });
  await expect(importer.getByText("大正文1.txt", { exact: true })).toBeVisible();
  await importer.getByRole("button", { name: "确认无重复的完整草稿" }).click();
  await expect(importer.getByText(/批量确认暂停了，已入库 1 份/)).toBeVisible();
  expect(state.task.items.filter((item: any) => item.status === "saved")).toHaveLength(1);
  await importer.getByRole("button", { name: "确认无重复的完整草稿" }).click();
  await expect(importer.getByText("已入库", { exact: true })).toHaveCount(16);
  const confirms = state.calls.filter((call) => call.path.endsWith("/confirm"));
  expect(confirms).toHaveLength(9);
  expect(confirms.every((call) => call.body.items.length === 1 && Buffer.byteLength(JSON.stringify(call.body), "utf8") < 4 * 1024 * 1024)).toBe(true);
  expect(Buffer.byteLength(JSON.stringify({ items: state.task.items.map((item: any) => ({ id: item.id, candidate: item.candidate })) }), "utf8")).toBeGreaterThan(4 * 1024 * 1024);
  expect(confirms.filter((call) => call.body.items[0].id === "large-item-0")).toHaveLength(1);
  expect(state.candidates).toHaveLength(9);
  expect(state.errors).toEqual([]);
});

test("部分成功草稿重开后只补齐记录，不重建简历或重新解析", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.task.items[0] = { id: "import-item", file_name: "部分完成.txt", status: "partial_success", candidate_id: existing.id, candidate: { ...existing }, error: "备注暂时没记全" };
  await page.goto("/admin/resumes");
  await page.evaluate(() => localStorage.setItem("goodhr5_resume_import:resume-user", "import-test"));
  await page.getByRole("button", { name: "添加", exact: true }).click();
  const chooser = page.getByRole("dialog", { name: "添加简历", exact: true });
  await chooser.getByText("AI 批量解析", { exact: true }).click(); await chooser.getByRole("button", { name: "继续", exact: true }).click();
  const importer = page.getByRole("dialog", { name: "AI 批量解析简历", exact: true });
  await expect(importer.getByText("简历已保存，备注 / 岗位待补齐", { exact: true })).toBeVisible();
  await importer.getByRole("button", { name: "继续完成记录" }).click();
  await expect(importer.getByText("已入库", { exact: true }).first()).toBeVisible();
  const confirms = state.calls.filter((call) => call.path.endsWith("/confirm"));
  expect(confirms).toHaveLength(1); expect(confirms[0].body).toEqual({ items: [{ id: "import-item" }] });
  expect(state.calls.some((call) => call.path.endsWith("/retry"))).toBe(false);
  expect(state.calls.some((call) => call.path === "/api/candidates" && call.method === "POST")).toBe(false);
  expect(state.errors).toEqual([]);
});
