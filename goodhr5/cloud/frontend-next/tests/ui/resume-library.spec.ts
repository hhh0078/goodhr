/** 文件作用说明：用完全模拟的云端和本地接口验证新简历库查重、保存重试、推荐记录和缺号草稿，避免真实数据写入。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const previewOrigin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
const cloudOrigin = new URL(process.env.NEXT_PUBLIC_CLOUD_API_BASE || "http://127.0.0.1:8084").origin;
const agentOrigin = new URL(`http://127.0.0.1:${process.env.UI_TEST_AGENT_PORT || "43129"}`).origin;
const screenshotDirectory = resolve(process.env.UI_TEST_SCREENSHOT_DIR || "../../../output/resume-folders");
// 合成16px纯色PNG只用于界面验证，不含任何真实候选人的照片。
const syntheticAvatar = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAGUlEQVR4nGNwLAj5TwlmGDVg1IBRA4aLAQBSaQQfj40p1QAAAABJRU5ErkJggg==";
type Call = { origin: string; path: string; method: string; body: any; query: URLSearchParams };
type PairApprovalReply = { gate?: Promise<void>; status?: number; error?: string };
type State = { calls: Call[]; notes: any[]; recommendations: any[]; folders?: any[]; recommendationSaveGate?: Promise<void>; positions: Array<{ id: string; name: string; is_open?: boolean; [key: string]: any }>; user: { id: string; email: string }; candidates: any[]; appConfig?: any; agentOffline?: boolean; failFirstNote: boolean; failConfirmAt?: number; task: any; taskPlain404?: boolean; taskListEmpty?: boolean; summaryTask?: any; summaryRequests?: Record<string, any>; failSummaryResponseOnce?: boolean; rejectSummaryOnce?: boolean; errors: string[]; messages?: any[]; messageVersion?: number; autoImportDownloads?: boolean; holdMessageSummary?: Promise<void>; pairApprovals?: Record<string, PairApprovalReply>; pairApprovalReplies?: string[] };
const existing = { id: "resume-existing", candidate_name: "原有候选人", phone: "13800138000", row_version: 3, source_type: "automatic", education_level: "本科", expected_salary_min: null, expected_salary_max: null, work_experiences: [{ company_name: "原有公司", position_name: "产品经理", start_ym: "2021-01", content: "已有工作经历保留" }], user_email: "test@example.invalid", created_at: "2026-10-10T10:00:00+08:00", ai: { detail: { score: null, reason: "" } } };
test.use({ storageState: { cookies: [], origins: [{ origin: previewOrigin, localStorage: [{ name: "goodhr5_access_token", value: "resume-test-only" }] }] } });

/** mockResumeSession 拦截全部业务请求，模拟持久化草稿与失败重试，不访问招聘平台。 */
async function mockResumeSession(page: Page): Promise<State> {
  const state: State = { calls: [], notes: [], recommendations: [], positions: [{ id: "position-one", name: "产品经理" }], user: { id: "resume-user", email: "test@example.invalid" }, candidates: [{ ...existing }], failFirstNote: false, errors: [], task: { id: "import-test", status: "completed", total: 1, processed: 1, items: [{ id: "import-item", file_name: "候选人.txt", status: "needs_phone", candidate: { candidate_name: "待补号候选人", source_type: "ai_import", phone: "" } }] } };
  page.on("pageerror", (error) => state.errors.push(error.message));
  await page.context().route("**/*", async (route) => {
    const request = route.request(); const url = new URL(request.url()); const method = request.method();
    if (url.origin === previewOrigin && !url.pathname.startsWith("/api/")) return route.continue();
    if (![previewOrigin, cloudOrigin, agentOrigin, "http://127.0.0.1:8084", "http://127.0.0.1:43129", "http://127.0.0.1:43139", "https://goodhr.58it.cn"].includes(url.origin)) return route.abort();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    let body: any; try { body = request.postDataJSON(); } catch { /* 上传和 GET 无 JSON 正文。 */ }
    state.calls.push({ origin: url.origin, path: url.pathname, method, body, query: url.searchParams });
    let data: any = { ok: true };
    if (["43129", "43139", new URL(agentOrigin).port].includes(url.port) && (url.pathname === "/health" || url.pathname.startsWith("/api/v1/"))) {
      // 离线场景也完全拦截端口探测，绝不连接电脑上正在运行的真实本地程序。
      if (state.agentOffline) return route.abort("connectionrefused");
      if (url.pathname === "/health") data = { ok: true, version: "6.2.10.1" };
      else if (url.pathname === "/api/v1/runtime/status") data = { version: "6.2.10.1", node_installed: true, camoufox_installed: true, system_runtime: { ready: true, required: false } };
      else if (url.pathname === "/api/v1/app-update/status") data = { stage: "idle", current_version: "6.2.10.1" };
      else if (url.pathname === "/api/v1/extensions/pair/approve" && method === "POST") {
        // 每次请求独立捕获回包，旧配对可以晚于新配对结束，且不触达真实本地授权。
        const key = `${url.port}:${body?.pairing_id}`;
        const reply = { ...(state.pairApprovals?.[key] || {}) };
        await reply.gate;
        await route.fulfill({ status: reply.status || 200, headers, json: reply.error ? { ok: false, error: { code: reply.status === 409 ? "PAIR_EXPIRED" : "PAIR_EXCHANGE_FAILED", message: reply.error } } : { ok: true, data: { approved: true, email: state.user.email } } });
        (state.pairApprovalReplies ||= []).push(key);
        return;
      }
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
        if (state.taskPlain404) return route.fulfill({ status: 404, headers, contentType: "text/plain", body: "404 page not found" });
        if (state.taskListEmpty && method === "GET" && url.pathname === "/api/v1/resume-import/tasks") return route.fulfill({ status: 200, headers, json: { ok: true, data: { tasks: [] } } });
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
    else if (url.pathname === "/api/runtime/config" || url.pathname === "/api/system/app-config") data = { config: state.appConfig || {} };
    else if (url.pathname === "/api/user-flow") data = { flow: { version: 2, state: "completed", stage: "completed", steps: {} } };
    else if (url.pathname.includes("/invitations")) data = { invitations: [] };
    else if (url.pathname === "/api/ai-wallet") data = { wallet: { balance: "100", models: [], default_model: "test" } };
    else if (url.pathname === "/api/messages/summary") {
      data = { ok: true, unread_count: (state.messages || []).filter((item) => !item.read_at).length, version: state.messageVersion || 0 };
      await state.holdMessageSummary;
    }
    else if (url.pathname === "/api/messages") data = { ok: true, messages: (state.messages || []).map(({ body, ...item }) => item), total: state.messages?.length || 0 };
    else if (url.pathname.startsWith("/api/messages/") && url.pathname.endsWith("/read")) {
      const message = state.messages?.find((item) => item.id === url.pathname.split("/")[3]);
      if (message) { message.read_at = "2026-10-10T21:00:00+08:00"; state.messageVersion = (state.messageVersion || 0) + 1; }
      data = { ok: true, message, summary: { unread_count: (state.messages || []).filter((item) => !item.read_at).length, version: state.messageVersion } };
    }
    else if (url.pathname === "/api/resume-library/settings") {
      if (method === "PATCH") state.autoImportDownloads = body.auto_import_downloads;
      data = { ok: true, settings: { auto_import_downloads: state.autoImportDownloads !== false } };
    }
    else if (url.pathname === "/api/system/default-prompts") data = { prompts: {} };
    else if (url.pathname === "/api/platforms/config/") data = { platforms: [{ id: "boss", platform_id: "boss", is_open: true, open: true, enabled: true }] };
    else if (url.pathname === "/api/positions") {
      if (method === "POST") {
        const previous = state.positions.find((item) => item.id === body.id);
        if (previous) Object.assign(previous, body);
        else state.positions.push({ ...body, id: "position-added" });
        data = { position: previous || state.positions.at(-1) };
      } else {
        const scope = url.searchParams.get("purpose") === "recommendation" ? "true" : url.searchParams.get("is_open") || "true";
        const positions = state.positions.filter((item) => scope === "all" || (scope === "false" ? item.is_open === false : item.is_open !== false));
        data = { positions, total: positions.length };
      }
    }
    else if (url.pathname === "/api/candidates/folders" || url.pathname.startsWith("/api/candidates/folders/")) {
      state.folders ||= [];
      const id = url.pathname.split("/")[4];
      if (method === "POST") { const tag = { ...body, id: `tag-created-${state.folders.length}`, candidate_count: 0, can_edit: true, created_by_email: state.user.email }; state.folders.push(tag); data = { ok: true, folder: tag }; }
      else if (method === "PATCH") { const tag = state.folders.find((item) => item.id === id); Object.assign(tag, body); data = { ok: true, folder: tag }; }
      else if (method === "DELETE") { state.folders = state.folders.filter((tag) => tag.id !== id); state.candidates.forEach((candidate) => { candidate.folder_ids = (candidate.folder_ids || []).filter((value: string) => value !== id); }); }
      else data = { ok: true, folders: state.folders.map((tag) => ({ ...tag, candidate_count: state.candidates.filter((candidate) => candidate.folder_ids?.includes(tag.id)).length })) };
    }
    else if (url.pathname === "/api/candidate-recommendations") {
      const records = state.recommendations.map((item) => {
        const candidate = state.candidates.find((candidate) => candidate.id === (item.candidate_id || existing.id));
        const position = state.positions.find((position) => position.id === item.position_id);
        return { ...item, candidate_id: candidate?.id || existing.id, candidate_name: candidate?.candidate_name || "", phone: candidate?.phone || "", position_name: position?.name || item.position_name, position_is_open: position?.is_open !== false, can_edit: position?.is_open !== false, can_delete: true };
      }).filter((item) => (!url.searchParams.get("stage") || item.stage === url.searchParams.get("stage")) && `${item.candidate_name} ${item.phone} ${item.position_name}`.includes(url.searchParams.get("q") || ""));
      const size = Number(url.searchParams.get("page_size")) || 20; const pageNumber = Number(url.searchParams.get("page")) || 1;
      data = { ok: true, items: records.slice((pageNumber - 1) * size, pageNumber * size), total: records.length };
    }
    else if (url.pathname.startsWith("/api/candidate-recommendations/") && method === "DELETE") {
      state.recommendations = state.recommendations.filter((item) => item.id !== url.pathname.split("/").at(-1));
    }
    else if (url.pathname === "/api/candidates/duplicates") data = { ok: true, hidden_match: false, matches: body?.phone === existing.phone && body?.exclude_id !== existing.id ? [{ candidate: existing, match_type: "exact", reasons: ["手机号相同"] }] : [] };
    else if (url.pathname === "/api/candidates") {
      if (method === "POST") { const candidate = { ...body, id: "resume-new", row_version: 1 }; state.candidates.push(candidate); data = { ok: true, candidate }; }
      else {
        const candidates = state.candidates.filter((candidate) => (!url.searchParams.get("folder_id") || candidate.folder_ids?.includes(url.searchParams.get("folder_id"))) && (!url.searchParams.get("tag") || candidate.tags?.some((tag: string) => tag.includes(url.searchParams.get("tag")!))));
        data = { candidates: candidates.map((candidate) => ({ ...candidate, folder_details: (state.folders || []).filter((folder) => candidate.folder_ids?.includes(folder.id)), notes: state.notes, recommendations: state.recommendations, recommendation_count: state.recommendations.length })), total: candidates.length, real_total: candidates.filter((candidate) => !candidate.is_builtin && candidate.id !== "00000000-0000-4000-8000-000000000001").length, builtin_total: candidates.filter((candidate) => candidate.is_builtin || candidate.id === "00000000-0000-4000-8000-000000000001").length, page: Number(url.searchParams.get("page")) || 1 };
      }
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
        const recommendation = { ...previous, id: previous?.id || `rec-${body.position_id}`, candidate_id: url.pathname.split("/")[3], position_id: body.position_id, position_name: previous?.position_name || (body.position_id === "position-two" ? "招聘顾问" : "产品经理"), stage: body.stage, updated_at: event.occurred_at, events: [...(previous?.events || []), event] };
        state.recommendations = previous ? state.recommendations.map((item) => item.id === previous.id ? recommendation : item) : [...state.recommendations, recommendation]; data = { recommendation };
      } else data = { recommendations: state.recommendations };
    } else if (url.pathname.startsWith("/api/candidates/")) {
      const id = url.pathname.split("/").pop(); const candidate = state.candidates.find((item) => item.id === id);
      if (method === "PATCH") Object.assign(candidate, body, { row_version: candidate.row_version + 1 });
      data = { candidate: candidate ? { ...candidate, folder_details: (state.folders || []).filter((folder) => candidate.folder_ids?.includes(folder.id)) } : candidate };
    }
    return route.fulfill({ status: 200, headers, json: data });
  });
  return state;
}

/** openManual 在同一添加弹框中选择手动标签，不经过继续按钮或第二层录入弹框。 */
async function openManual(page: Page) {
  await page.getByRole("button", { name: "添加", exact: true }).click();
  const chooser = page.getByRole("dialog", { name: "添加简历", exact: true });
  await expect(chooser.getByRole("tab", { name: "AI 批量上传", exact: true })).toBeVisible();
  await chooser.getByRole("tab", { name: "手动添加", exact: true }).click();
  return chooser;
}
/** dismissNotices 等待短通知自动消失，避免遮住布局验收截图。 */
async function dismissNotices(page: Page) {
  await expect(page.locator(".ant-notification-notice")).toHaveCount(0);
}

const pairApprovePath = "/api/v1/extensions/pair/approve";
const pairDialogTitle = "连接 GoodHR 浏览器插件";

test("收藏夹栏紧凑可滚动，筛选按编号并按账号记住最近点击顺序", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.folders = Array.from({ length: 18 }, (_, index) => ({ id: `folder-${index}`, name: `合成收藏夹${index}`, scope: index % 2 ? "personal" : "team", can_edit: true }));
  state.candidates[0].folder_ids = ["folder-1", "folder-5"];
  await page.setViewportSize({ width: 980, height: 820 });
  await page.goto("/admin/resumes");
  const bar = page.getByTestId("resume-folder-bar");
  await expect(bar.getByTitle("合成收藏夹1 · 仅自己可见", { exact: true })).toHaveText("合成收藏夹11");
  await expect(bar.getByRole("button", { name: "管理收藏夹", exact: true })).toBeInViewport();
  await bar.getByTitle("合成收藏夹5 · 仅自己可见", { exact: true }).click();
  await expect(page).toHaveURL(/folder_id=folder-5/);
  await bar.getByTitle("合成收藏夹1 · 仅自己可见", { exact: true }).click();
  await expect(page).toHaveURL(/folder_id=folder-1/);
  await expect(bar.getByRole("button").nth(1)).toHaveAttribute("title", "合成收藏夹1 · 仅自己可见");
  await page.reload();
  await expect(bar.getByRole("button").nth(1)).toHaveAttribute("title", "合成收藏夹1 · 仅自己可见");
  await expect(bar.getByRole("button").nth(2)).toHaveAttribute("title", "合成收藏夹5 · 仅自己可见");
  expect(state.calls.some((call) => call.path === "/api/candidates" && call.query.get("folder_id") === "folder-1")).toBe(true);
  state.user.id = "another-resume-user";
  await page.goto("/admin/resumes");
  await expect(bar.getByRole("button").nth(1)).toHaveAttribute("title", "合成收藏夹0 · 团队可见");
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "folder-bar.png"), fullPage: true, animations: "disabled" });
  expect(state.errors).toEqual([]);
});

test("管理收藏夹默认个人，编辑可改团队，删除收藏夹保留全部简历", async ({ page }) => {
  const state = await mockResumeSession(page);
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "管理收藏夹", exact: true }).click();
  const manager = page.getByRole("dialog", { name: "管理收藏夹", exact: true });
  await manager.getByRole("button", { name: "新增收藏夹", exact: true }).click();
  const create = page.getByRole("dialog", { name: "新增收藏夹", exact: true });
  await expect(create.getByRole("radio", { name: "个人收藏夹", exact: true })).toBeChecked();
  await create.getByLabel("收藏夹名称", { exact: true }).fill("合成待联系");
  await create.getByRole("button", { name: "保存", exact: true }).click();
  await expect(create).toBeHidden();
  const row = manager.getByRole("row").filter({ hasText: "合成待联系" });
  await expect(row).toContainText("仅自己");
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "编辑收藏夹", exact: true });
  await edit.getByRole("radio", { name: "团队收藏夹", exact: true }).check();
  await edit.getByRole("button", { name: "保存", exact: true }).click();
  await expect(row).toContainText("团队可见");
  await dismissNotices(page);
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "folder-manager.png"), fullPage: true, animations: "disabled" });
  const originalID = state.candidates[0].id;
  state.candidates[0].folder_ids = [state.folders![0].id];
  await row.getByRole("button", { name: "删除", exact: true }).click();
  const remove = page.getByRole("dialog", { name: "确认删除收藏夹", exact: true });
  await expect(remove).toContainText("对应简历会保留");
  await remove.getByRole("button", { name: "删除收藏夹", exact: true }).click();
  await expect(row).toHaveCount(0);
  expect(state.candidates.map((candidate) => candidate.id)).toEqual([originalID]);
  expect(state.calls.some((call) => call.method === "DELETE" && call.path === `/api/candidates/${originalID}`)).toBe(false);
  expect(state.calls.find((call) => call.method === "POST" && call.path === "/api/candidates/folders")?.body.scope).toBe("personal");
  expect(state.calls.find((call) => call.method === "PATCH" && call.path.startsWith("/api/candidates/folders/"))?.body.scope).toBe("team");
  expect(state.errors).toEqual([]);
});

test("整行双击打开详情，操作竖排且面试暂不提交请求", async ({ page }) => {
  const state = await mockResumeSession(page);
  await page.goto("/admin/resumes");
  const row = page.getByRole("row").filter({ has: page.getByRole("button", { name: "原有候选人", exact: true }) });
  for (const name of ["备注", "推荐", "面试"]) await expect(row.getByRole("button", { name, exact: true })).toBeVisible();
  await expect(row.getByRole("button", { name: "编辑", exact: true })).toHaveCount(0);
  await expect(row.getByRole("button", { name: "查看完整简历", exact: true })).toHaveCount(0);
  await expect(row).not.toContainText("个岗位记录");
  const resumeRequests = () => state.calls.filter((call) => call.path === `/api/candidates/${existing.id}`).length;
  const before = resumeRequests();
  await row.getByRole("button", { name: "面试", exact: true }).dblclick();
  await expect(page.getByRole("dialog", { name: "原有候选人", exact: true })).toHaveCount(0);
  expect(resumeRequests()).toBe(before);
  await row.getByRole("cell").nth(1).dblclick();
  await expect(page.getByRole("dialog", { name: "原有候选人", exact: true })).toBeVisible();
  expect(state.errors).toEqual([]);
});

test("候选人选择已有收藏夹，未知名称失焦确认，创建默认个人并保存编号", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.folders = [{ id: "folder-existing", name: "团队已有", scope: "team", can_edit: false }];
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  await drawer.getByRole("button", { name: /编辑$/ }).click();
  const editor = page.getByRole("dialog", { name: "维护简历 · 原有候选人", exact: true });
  const tags = editor.getByLabel("收藏夹", { exact: true });
  await tags.fill("团队已有");
  await page.locator(".ant-select-item-option").filter({ hasText: "团队已有 · 团队" }).click();
  await tags.fill("合成个人新收藏夹");
  await editor.getByLabel("姓名", { exact: true }).click();
  const confirm = page.getByRole("dialog", { name: "创建新收藏夹", exact: true });
  await expect(confirm).toContainText("这个收藏夹还不存在");
  await expect(confirm.getByRole("radio", { name: "个人收藏夹", exact: true })).toBeChecked();
  await confirm.getByRole("button", { name: "创建并选择", exact: true }).click();
  await expect(confirm).toBeHidden();
  await expect(page.getByTestId("resume-folder-bar")).toContainText("合成个人新收藏夹");
  await editor.getByRole("button", { name: "检查并保存", exact: true }).click();
  const preview = page.getByRole("dialog", { name: "确认更新内容", exact: true });
  await expect(preview).toContainText("合成个人新收藏夹（个人）");
  await preview.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(editor).toBeHidden();
  const patch = state.calls.find((call) => call.method === "PATCH" && call.path === `/api/candidates/${existing.id}`);
  expect(patch?.body.folder_ids).toEqual(["folder-existing", state.folders![1].id]);
  expect(patch?.body).not.toHaveProperty("tags");
  expect(state.calls.filter((call) => call.method === "POST" && call.path === "/api/candidates/folders")).toHaveLength(1);
  expect(state.errors).toEqual([]);
});

test("岗位默认筛选开启，编辑关闭保留岗位，推荐选择只读开启岗位", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.positions = [{ id: "position-one", name: "开启产品经理", platform_id: "boss", is_open: true, status: "stopped", creator_email: state.user.email }, { id: "position-closed", name: "关闭招聘顾问", platform_id: "boss", is_open: false, status: "stopped", creator_email: state.user.email }];
  state.recommendations = [{ id: "closed-recommendation", position_id: "position-closed", position_name: "关闭招聘顾问", stage: "contacting", events: [{ id: "closed-history", content: "关闭岗位原有跟进", to_stage: "contacting" }] }];
  await page.goto("/admin/positions");
  await expect(page.getByText("开启产品经理", { exact: true })).toBeVisible();
  await expect(page.getByText("关闭招聘顾问", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "编辑岗位模板", exact: true });
  await expect(edit.getByRole("switch", { name: "岗位开启状态", exact: true })).toBeChecked();
  await expect(edit).toContainText("岗位和已有推荐记录都会保留");
  await edit.getByRole("switch", { name: "岗位开启状态", exact: true }).click();
  await edit.getByRole("button", { name: "保存修改", exact: true }).click();
  await expect(edit).toBeHidden();
  await expect(page.getByText("开启产品经理", { exact: true })).toHaveCount(0);
  expect(state.positions).toHaveLength(2);
  expect(state.positions[0].is_open).toBe(false);
  await page.locator(".ant-radio-button-wrapper").filter({ hasText: "关闭" }).click();
  await expect(page.getByText("开启产品经理", { exact: true })).toBeVisible();
  await expect(page.getByText("关闭招聘顾问", { exact: true })).toBeVisible();
  await dismissNotices(page);
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "position-closed.png"), fullPage: true, animations: "disabled" });
  state.positions.push({ id: "position-new-open", name: "可推荐新岗位", platform_id: "boss", is_open: true });
  await page.goto("/admin/resumes");
  await page.getByRole("row").filter({ has: page.getByRole("button", { name: "原有候选人", exact: true }) }).getByRole("button", { name: "推荐", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  await drawer.getByRole("combobox", { name: "选择推荐岗位", exact: true }).click();
  await expect(page.getByText("可推荐新岗位", { exact: true })).toBeVisible();
  await expect(page.locator(".ant-select-item-option").filter({ hasText: "关闭招聘顾问" })).toHaveCount(0);
  await drawer.getByRole("button", { name: "选择推荐记录 关闭招聘顾问", exact: true }).click();
  await expect(drawer.getByText("关闭岗位原有跟进", { exact: true })).toBeVisible();
  await expect(drawer.getByRole("textbox", { name: "新增岗位跟进备注", exact: true })).toBeDisabled();
  await expect(drawer.getByRole("button", { name: "记录跟进", exact: true })).toBeDisabled();
  expect(state.calls.some((call) => call.path === "/api/positions" && call.query.get("purpose") === "recommendation")).toBe(true);
  expect(state.errors).toEqual([]);
});

test("管理员编辑他人团队收藏夹可改名，不能转为他人个人收藏夹", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.folders = [{ id: "folder-other-team", name: "他人团队收藏夹", scope: "team", can_edit: true, created_by_email: "someone@example.invalid" }];
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "管理收藏夹", exact: true }).click();
  const manager = page.getByRole("dialog", { name: "管理收藏夹", exact: true });
  await manager.getByRole("button", { name: "编辑", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "编辑收藏夹", exact: true });
  await expect(edit.getByRole("radio", { name: "个人收藏夹", exact: true })).toBeDisabled();
  await expect(edit).toContainText("只有创建者能改为个人收藏夹");
  await edit.getByLabel("收藏夹名称", { exact: true }).fill("管理员改名后");
  await edit.getByRole("button", { name: "保存", exact: true }).click();
  await expect(manager).toContainText("管理员改名后");
  expect(state.folders[0].scope).toBe("team");
  expect(state.errors).toEqual([]);
});

test("候选人清空全部收藏夹须明确确认，发送空编号数组而保留简历", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.folders = [{ id: "folder-clear", name: "可清空收藏夹", scope: "personal", can_edit: true }];
  Object.assign(state.candidates[0], { folder_ids: ["folder-clear"], folder_details: state.folders, tags: ["原始Java技能"] });
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  await drawer.getByRole("button", { name: /编辑$/ }).click();
  const editor = page.getByRole("dialog", { name: "维护简历 · 原有候选人", exact: true });
  await editor.locator(".ant-form-item").filter({ has: page.getByText("收藏夹", { exact: true }) }).locator(".ant-select-selection-item-remove").click();
  await editor.getByRole("button", { name: "检查并保存", exact: true }).click();
  const preview = page.getByRole("dialog", { name: "确认更新内容", exact: true });
  const clear = preview.getByRole("checkbox", { name: "确认清空收藏夹", exact: true });
  await expect(clear).not.toBeChecked();
  await clear.check();
  await preview.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(editor).toBeHidden();
  expect(state.candidates).toHaveLength(1);
  expect(state.candidates[0].folder_ids).toEqual([]);
  expect(state.candidates[0].tags).toEqual(["原始Java技能"]);
  expect(state.calls.find((call) => call.method === "PATCH" && call.path === `/api/candidates/${existing.id}`)?.body.folder_ids).toEqual([]);
  expect(state.errors).toEqual([]);
});

test("原始标签自由输入和收藏夹独立，原有标签不会自动创建或加入收藏夹", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.candidates[0].tags = ["Java", "原始项目管理"];
  await page.goto("/admin/resumes");
  const row = page.getByRole("row").filter({ has: page.getByRole("button", { name: "原有候选人", exact: true }) });
  await expect(row).toContainText("Java");
  await expect(row).toContainText("原始项目管理");
  await expect(row).toContainText("未加入收藏夹");
  await expect(page.getByTestId("resume-folder-bar")).toContainText("还没收藏夹");
  await row.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  await drawer.getByRole("button", { name: /编辑$/ }).click();
  const editor = page.getByRole("dialog", { name: "维护简历 · 原有候选人", exact: true });
  const rawTags = editor.locator(".ant-form-item").filter({ has: page.getByText("标签", { exact: true }) });
  const folders = editor.locator(".ant-form-item").filter({ has: page.getByText("收藏夹", { exact: true }) });
  await expect(rawTags).toContainText("Java");
  await expect(folders.locator(".ant-select-selection-item")).toHaveCount(0);
  await editor.getByLabel("标签", { exact: true }).fill("Node.js");
  await editor.getByLabel("标签", { exact: true }).press("Enter");
  await editor.getByLabel("姓名", { exact: true }).click();
  await expect(page.getByRole("dialog", { name: "创建新收藏夹", exact: true })).toHaveCount(0);
  await editor.getByRole("button", { name: "检查并保存", exact: true }).click();
  const preview = page.getByRole("dialog", { name: "确认更新内容", exact: true });
  await expect(preview).toContainText("Node.js");
  await preview.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(editor).toBeHidden();
  expect(state.candidates[0].tags).toEqual(["Java", "原始项目管理", "Node.js"]);
  expect(state.candidates[0].folder_ids || []).toEqual([]);
  expect(state.folders || []).toEqual([]);
  const patch = state.calls.find((call) => call.method === "PATCH" && call.path === `/api/candidates/${existing.id}`);
  expect(patch?.body.tags).toEqual(["Java", "原始项目管理", "Node.js"]);
  expect(patch?.body).not.toHaveProperty("folder_ids");
  expect(state.calls.some((call) => call.method === "POST" && call.path === "/api/candidates/folders")).toBe(false);
  expect(state.errors).toEqual([]);
});

test("清空原始标签只清 tags，保留候选人已选收藏夹", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.folders = [{ id: "folder-keep", name: "手动收藏夹保留", scope: "personal", can_edit: true }];
  Object.assign(state.candidates[0], { tags: ["Java", "原始技能"], folder_ids: ["folder-keep"] });
  await page.setViewportSize({ width: 1920, height: 1000 });
  await page.goto("/admin/resumes");
  const row = page.getByRole("row").filter({ has: page.getByRole("button", { name: "原有候选人", exact: true }) });
  await expect(row).toContainText("手动收藏夹保留");
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "resume-tags-and-folders.png"), fullPage: true, animations: "disabled" });
  await row.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  await drawer.getByRole("button", { name: /编辑$/ }).click();
  const editor = page.getByRole("dialog", { name: "维护简历 · 原有候选人", exact: true });
  const rawTags = editor.locator(".ant-form-item").filter({ has: page.getByText("标签", { exact: true }) });
  await rawTags.locator(".ant-select-selection-item-remove").first().click();
  await rawTags.locator(".ant-select-selection-item-remove").first().click();
  await editor.getByRole("button", { name: "检查并保存", exact: true }).click();
  const preview = page.getByRole("dialog", { name: "确认更新内容", exact: true });
  await expect(preview.getByRole("checkbox", { name: "确认清空标签", exact: true })).not.toBeChecked();
  await expect(preview.getByRole("checkbox", { name: "确认清空收藏夹", exact: true })).toHaveCount(0);
  await preview.getByRole("checkbox", { name: "确认清空标签", exact: true }).check();
  await preview.getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(editor).toBeHidden();
  expect(state.candidates[0].tags).toEqual([]);
  expect(state.candidates[0].folder_ids).toEqual(["folder-keep"]);
  const patch = state.calls.find((call) => call.method === "PATCH" && call.path === `/api/candidates/${existing.id}`);
  expect(patch?.body.tags).toEqual([]);
  expect(patch?.body).not.toHaveProperty("folder_ids");
  expect(state.errors).toEqual([]);
});

test("岗位与猎头推荐工具单行，无大标题和独立刷新，新增筛选照常可用", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.positions[0] = { ...state.positions[0], platform_id: "boss", creator_email: state.user.email, is_open: true };
  await page.setViewportSize({ width: 1600, height: 900 });
  for (const current of [
    { path: "/admin/positions", title: "岗位管理", toolbar: "positions-toolbar", create: "新建岗位", dialog: "新建岗位模板" },
    { path: "/admin/recommendations", title: "猎头推荐", toolbar: "recommendations-toolbar", create: "新增推荐", dialog: "新增猎头推荐" },
  ]) {
    await page.goto(current.path);
    await expect(page.getByRole("heading", { name: current.title, exact: true })).toHaveCount(0);
    const toolbar = page.getByTestId(current.toolbar);
    await expect(toolbar).toBeVisible();
    const create = toolbar.getByRole("button", { name: current.create, exact: true });
    await expect(toolbar.getByRole("button", { name: /刷新$/ })).toHaveCount(0);
    const createBox = await create.boundingBox();
    expect(createBox).not.toBeNull();
    const control = current.title === "岗位管理" ? toolbar.getByLabel("岗位状态筛选", { exact: true }) : toolbar.getByLabel("筛选推荐阶段", { exact: true });
    const controlBox = await control.boundingBox();
    expect(Math.abs(controlBox!.y + controlBox!.height / 2 - createBox!.y - createBox!.height / 2)).toBeLessThan(3);
    await create.click();
    const dialog = page.getByRole("dialog", { name: current.dialog, exact: true });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
    await expect(dialog).toBeHidden();
    mkdirSync(screenshotDirectory, { recursive: true });
    await page.screenshot({ path: resolve(screenshotDirectory, `${current.toolbar}.png`), fullPage: true, animations: "disabled" });
  }
  expect(state.errors).toEqual([]);
});

test("猎头推荐独立菜单分页搜索，新增与管理复用原推荐接口，删除保留简历岗位", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.positions = [{ id: "position-one", name: "开启产品岗位", is_open: true }, { id: "position-closed", name: "关闭顾问岗位", is_open: false }];
  state.recommendations = [{ id: "closed-rec", candidate_id: existing.id, position_id: "position-closed", position_name: "关闭顾问岗位", stage: "paused", updated_at: "2026-10-11T10:00:00+08:00", events: [{ id: "closed-event", content: "历史备注继续保留", to_stage: "paused", occurred_at: "2026-10-12T10:00:00+08:00" }, { id: "older-event", content: "最早的旧跟进备注", to_stage: "recommended", occurred_at: "2026-10-08T10:00:00+08:00" }] }];
  await page.goto("/admin/recommendations");
  await expect(page.getByRole("menuitem", { name: "猎头推荐", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "猎头推荐", exact: true })).toHaveCount(0);
  await expect(page.getByText("历史备注继续保留", { exact: true })).toBeVisible();
  const initialClosedRow = page.getByRole("row").filter({ hasText: "关闭顾问岗位" });
  await expect(initialClosedRow).not.toContainText("最早的旧跟进备注");
  await expect(initialClosedRow).toContainText("2026/10/12 10:00");
  await expect(initialClosedRow).toContainText("暂不推进");
  await page.getByLabel("搜索猎头推荐", { exact: true }).fill("关闭顾问岗位");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page).toHaveURL(/q=/);
  expect(state.calls.some((call) => call.path === "/api/candidate-recommendations" && call.query.get("q") === "关闭顾问岗位")).toBe(true);
  await page.getByRole("button", { name: "重置", exact: true }).click();
  await page.getByRole("button", { name: "新增推荐", exact: true }).click();
  const create = page.getByRole("dialog", { name: "新增猎头推荐", exact: true });
  await create.getByLabel("候选人", { exact: true }).click();
  await page.locator(".ant-select-item-option").filter({ hasText: "原有候选人 · 13800138000" }).click();
  await create.getByLabel("推荐岗位", { exact: true }).click();
  await expect(page.locator(".ant-select-item-option").filter({ hasText: "关闭顾问岗位" })).toHaveCount(0);
  await page.locator(".ant-select-item-option").filter({ hasText: "开启产品岗位" }).click();
  await create.getByLabel("本次备注", { exact: true }).fill("首次推荐合成备注");
  await create.getByRole("button", { name: "保存推荐", exact: true }).click();
  await expect(create).toBeHidden();
  const activeRow = page.getByRole("row").filter({ hasText: "开启产品岗位" });
  await expect(activeRow).toContainText("已推荐");
  await activeRow.getByRole("button", { name: "管理", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  await expect(drawer.getByRole("radio", { name: "已推荐", exact: true })).toBeChecked();
  await drawer.getByRole("radio", { name: "面试中", exact: true }).check();
  await drawer.getByRole("textbox", { name: "新增岗位跟进备注", exact: true }).fill("本次进入面试");
  await drawer.getByRole("button", { name: "记录跟进", exact: true }).click();
  await expect(drawer.getByText("本次进入面试", { exact: true })).toBeVisible();
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(activeRow).toContainText("面试中");
  await expect(drawer).toBeHidden();
  await page.getByLabel("筛选推荐阶段", { exact: true }).fill("面试中");
  await page.getByLabel("筛选推荐阶段", { exact: true }).press("Enter");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page).toHaveURL(/stage=interview/);
  await expect(page.getByRole("row").filter({ hasText: "关闭顾问岗位" })).toHaveCount(0);
  await page.getByRole("button", { name: "重置", exact: true }).click();
  await activeRow.getByRole("button", { name: "删除", exact: true }).click();
  const remove = page.getByRole("dialog", { name: "删除推荐记录", exact: true });
  await expect(remove).toContainText("简历和岗位会保留");
  await remove.getByRole("button", { name: "删除记录", exact: true }).click();
  await expect(activeRow).toHaveCount(0);
  expect(state.candidates).toHaveLength(1); expect(state.positions).toHaveLength(2);
  expect(state.calls.some((call) => call.method === "DELETE" && (call.path.startsWith("/api/candidates/") || call.path.startsWith("/api/positions/")))).toBe(false);
  const closedRow = page.getByRole("row").filter({ hasText: "关闭顾问岗位" });
  await closedRow.getByRole("button", { name: "查看", exact: true }).click();
  await expect(drawer.getByText("历史备注继续保留", { exact: true })).toBeVisible();
  await expect(drawer.getByRole("textbox", { name: "新增岗位跟进备注", exact: true })).toBeDisabled();
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(drawer).toBeHidden();
  const recordsCount = state.recommendations.length;
  await closedRow.getByRole("cell").nth(1).dblclick();
  await expect(drawer).toBeVisible();
  expect(state.recommendations).toHaveLength(recordsCount);
  await drawer.getByRole("button", { name: "关闭", exact: true }).click();
  await dismissNotices(page);
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "headhunter-recommendations.png"), fullPage: true, animations: "disabled" });
  expect(state.errors).toEqual([]);
});

/** pairApprovalURL 生成带有筛选、分页和重复参数的合成配对入口，验证关闭只移除配对参数。 */
function pairApprovalURL(pairingID: string, port = 43129) {
  const query = new URLSearchParams({ q: "配对筛选保留", phone: "8000", source_type: "automatic", page: "2", page_size: "10", tag: "待跟进", extension_pair: pairingID, agent_port: String(port) });
  query.append("extra", "first"); query.append("extra", "second");
  return `/admin/resumes?${query}`;
}

/** pairApprovalCalls 只统计显式授权提交，区分本地端口探测与授权行为。 */
function pairApprovalCalls(state: State) {
  return state.calls.filter((call) => call.path === pairApprovePath && call.method === "POST");
}

/** createPairApprovalGate 暂停模拟回包，让测试通过真实按钮验证请求进行中的操作。 */
function createPairApprovalGate() {
  let release!: () => void;
  const promise = new Promise<void>((resolveGate) => { release = resolveGate; });
  return { promise, release };
}

/** expectPairQueryCleared 检查两项配对参数已删除，原有筛选、分页和重复参数完整保留。 */
async function expectPairQueryCleared(page: Page, originalURL: string) {
  const expected = new URL(originalURL, previewOrigin);
  expected.searchParams.delete("extension_pair"); expected.searchParams.delete("agent_port");
  await expect(page).toHaveURL((url) => url.pathname === expected.pathname && url.searchParams.toString() === expected.searchParams.toString());
  await expect(page.getByLabel("简历关键词")).toHaveValue("配对筛选保留");
  await expect(page.getByLabel("手机号搜索")).toHaveValue("8000");
}

/** pushPairApproval 使用自己站点的原生历史导航切换配对，保持组件挂载并验证迟到回包隔离。 */
async function pushPairApproval(page: Page, pairingID: string, port = 43129) {
  const nextURL = pairApprovalURL(pairingID, port);
  await page.evaluate((url) => { window.history.pushState(null, "", url); }, nextURL);
  await expect(page).toHaveURL((url) => url.searchParams.get("extension_pair") === pairingID && url.searchParams.get("agent_port") === String(port));
  return nextURL;
}

/** releasePairApproval 等待浏览器收到指定回包和界面完成一轮渲染，避免在迟到回包前就结束断言。 */
async function releasePairApproval(page: Page, pairingID: string, release: () => void, port = 43129) {
  const responsePromise = page.waitForResponse((response) => {
    const request = response.request(), url = new URL(response.url());
    return url.pathname === pairApprovePath && url.port === String(port) && request.method() === "POST" && request.postDataJSON()?.pairing_id === pairingID;
  });
  release();
  await (await responsePromise).finished();
  // 仅等待自有 GoodHR 页面渲染，不访问或注入招聘平台页面。
  await page.evaluate(() => new Promise<void>((resolveFrame) => requestAnimationFrame(() => requestAnimationFrame(() => resolveFrame()))));
}

for (const closing of ["取消", "右上角", "Esc", "遮罩"] as const) {
  test(`配对未确认可通过${closing}关闭，保留筛选且不提交授权`, async ({ page }) => {
    const state = await mockResumeSession(page);
    const entryURL = pairApprovalURL(`pair-dismiss-${closing}`);
    await page.goto(entryURL);
    const dialog = page.getByRole("dialog", { name: pairDialogTitle, exact: true });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("button", { name: "确认连接", exact: true })).toBeEnabled();
    if (closing === "取消") await dialog.getByRole("button", { name: "取消", exact: true }).click();
    else if (closing === "右上角") await dialog.locator("button.ant-modal-close").click();
    else if (closing === "Esc") {
      // 等待入场和浮层注册完成，再把真实 Esc 发给弹框内的按钮。
      await expect(dialog).not.toHaveClass(/ant-zoom-(appear|enter)/);
      await dialog.getByRole("button", { name: "取消", exact: true }).press("Escape");
    }
    else await page.locator(".ant-modal-wrap").filter({ has: dialog }).click({ position: { x: 10, y: 10 } });
    await expect(dialog).toBeHidden();
    await expectPairQueryCleared(page, entryURL);
    await expect.poll(() => state.calls.some((call) => call.path === "/api/candidates" && call.query.get("q") === "配对筛选保留")).toBe(true);
    expect(pairApprovalCalls(state)).toHaveLength(0);
    expect(state.errors).toEqual([]);
  });
}

for (const lateResult of ["成功", "失败"] as const) {
  test(`配对处理中可关闭且只提交一次，迟到${lateResult}不重开弹框或提示`, async ({ page }) => {
    const state = await mockResumeSession(page), gate = createPairApprovalGate();
    const pairingID = `pair-late-${lateResult}`, entryURL = pairApprovalURL(pairingID);
    state.pairApprovals = { [`43129:${pairingID}`]: { gate: gate.promise, ...(lateResult === "失败" ? { status: 503, error: "合成的迟到授权错误" } : {}) } };
    await page.goto(entryURL);
    const dialog = page.getByRole("dialog", { name: pairDialogTitle, exact: true });
    const confirm = dialog.getByRole("button", { name: "确认连接", exact: true });
    await confirm.dblclick();
    await expect.poll(() => pairApprovalCalls(state).length).toBe(1);
    await expect(confirm).toBeDisabled();
    await confirm.click({ force: true });
    await expect(dialog.getByText("连接正在处理，关掉弹框后也可能完成，可回插件查看结果。", { exact: true })).toBeVisible();
    const close = dialog.locator(".ant-modal-footer").getByRole("button", { name: "关闭", exact: true });
    const cornerClose = dialog.locator("button.ant-modal-close");
    await expect(close).toBeEnabled();
    await expect(cornerClose).toBeEnabled();
    if (lateResult === "成功") {
      mkdirSync(screenshotDirectory, { recursive: true });
      await page.screenshot({ path: resolve(screenshotDirectory, "extension-pair-pending-closable.png"), fullPage: true, animations: "disabled" });
    }
    if (lateResult === "成功") await close.click();
    else await cornerClose.click();
    await expect(dialog).toBeHidden();
    await expectPairQueryCleared(page, entryURL);
    await releasePairApproval(page, pairingID, gate.release);
    await expect.poll(() => state.pairApprovalReplies).toContain(`43129:${pairingID}`);
    await expect(dialog).toBeHidden();
    await expect(page.getByText("插件已授权，可以回插件继续了", { exact: true })).toHaveCount(0);
    await expect(page.getByText("合成的迟到授权错误", { exact: true })).toHaveCount(0);
    await expectPairQueryCleared(page, entryURL);
    expect(pairApprovalCalls(state)).toHaveLength(1);
    expect(pairApprovalCalls(state)[0].body).toEqual({ pairing_id: pairingID, token: "resume-test-only" });
    expect(state.errors).toEqual([]);
  });
}

for (const changedKey of ["配对编号", "本地端口"] as const) {
  test(`配对切换${changedKey}时重置状态，旧回包不覆盖新配对`, async ({ page }) => {
    const state = await mockResumeSession(page), gate = createPairApprovalGate();
    const oldID = "pair-old", newID = changedKey === "配对编号" ? "pair-new" : oldID, newPort = changedKey === "本地端口" ? 43139 : 43129;
    state.pairApprovals = { [`43129:${oldID}`]: { gate: gate.promise } };
    await page.goto(pairApprovalURL(oldID));
    const dialog = page.getByRole("dialog", { name: pairDialogTitle, exact: true });
    await dialog.getByRole("button", { name: "确认连接", exact: true }).click();
    await expect.poll(() => pairApprovalCalls(state).length).toBe(1);
    await expect(dialog.getByRole("button", { name: "确认连接", exact: true })).toBeDisabled();
    const documents: string[] = [];
    page.on("request", (request) => { if (request.resourceType() === "document") documents.push(request.url()); });
    const entryURL = await pushPairApproval(page, newID, newPort);
    await expect(dialog.getByRole("button", { name: "确认连接", exact: true })).toBeEnabled();
    await expect(dialog.getByRole("button", { name: "取消", exact: true })).toBeEnabled();
    await releasePairApproval(page, oldID, gate.release);
    await expect(dialog.getByRole("button", { name: "确认连接", exact: true })).toBeEnabled();
    await expect(dialog.getByText("授权完成，请回到插件", { exact: true })).toHaveCount(0);
    await expect(page.getByText("插件已授权，可以回插件继续了", { exact: true })).toHaveCount(0);
    await dialog.getByRole("button", { name: "确认连接", exact: true }).click();
    await expect(dialog.getByText("授权完成，请回到插件", { exact: true })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "确认连接", exact: true })).toHaveCount(0);
    expect(pairApprovalCalls(state).map((call) => [call.origin, call.body.pairing_id])).toEqual([["http://127.0.0.1:43129", oldID], [`http://127.0.0.1:${newPort}`, newID]]);
    await dialog.getByRole("button", { name: "完成", exact: true }).click();
    await expectPairQueryCleared(page, entryURL);
    expect(documents).toEqual([]);
    expect(state.errors).toEqual([]);
  });
}

test("配对成功后完成关闭，新配对再次打开并可独立连接", async ({ page }) => {
  const state = await mockResumeSession(page);
  await page.goto(pairApprovalURL("pair-first-success"));
  const dialog = page.getByRole("dialog", { name: pairDialogTitle, exact: true });
  await dialog.getByRole("button", { name: "确认连接", exact: true }).click();
  await expect(dialog.getByText("授权完成，请回到插件", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "完成", exact: true }).click();
  await expect(dialog).toBeHidden();
  await expectPairQueryCleared(page, pairApprovalURL("pair-first-success"));
  const entryURL = await pushPairApproval(page, "pair-second-success");
  await expect(dialog.getByRole("button", { name: "确认连接", exact: true })).toBeEnabled();
  await expect(dialog.getByRole("button", { name: "取消", exact: true })).toBeEnabled();
  await expect(dialog.getByText("授权完成，请回到插件", { exact: true })).toHaveCount(0);
  await dialog.getByRole("button", { name: "确认连接", exact: true }).click();
  await expect(dialog.getByText("授权完成，请回到插件", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "完成", exact: true }).click();
  await expectPairQueryCleared(page, entryURL);
  expect(pairApprovalCalls(state).map((call) => call.body.pairing_id)).toEqual(["pair-first-success", "pair-second-success"]);
  expect(state.errors).toEqual([]);
});

test("配对请求失败后恢复确认按钮，可以重试且完成关闭", async ({ page }) => {
  const state = await mockResumeSession(page), pairingID = "pair-retry", entryURL = pairApprovalURL(pairingID);
  state.pairApprovals = { [`43129:${pairingID}`]: { status: 503, error: "插件授权暂时没完成，请再试一次" } };
  await page.goto(entryURL);
  const dialog = page.getByRole("dialog", { name: pairDialogTitle, exact: true });
  await dialog.getByRole("button", { name: "确认连接", exact: true }).click();
  await expect(page.getByText("插件授权暂时没完成，请再试一次", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "确认连接", exact: true })).toBeEnabled();
  await expect(dialog.getByRole("button", { name: "取消", exact: true })).toBeEnabled();
  state.pairApprovals[`43129:${pairingID}`] = {};
  await dialog.getByRole("button", { name: "确认连接", exact: true }).click();
  await expect(dialog.getByText("授权完成，请回到插件", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "完成", exact: true }).click();
  await expectPairQueryCleared(page, entryURL);
  expect(pairApprovalCalls(state)).toHaveLength(2);
  expect(state.errors).toEqual([]);
});

test("配对过期提示后可取消关闭，再发起有效配对", async ({ page }) => {
  const state = await mockResumeSession(page), entryURL = pairApprovalURL("pair-expired");
  state.pairApprovals = { "43129:pair-expired": { status: 409, error: "这个配对请求已经过期或处理过了，请重新发起" } };
  await page.goto(entryURL);
  const dialog = page.getByRole("dialog", { name: pairDialogTitle, exact: true });
  await dialog.getByRole("button", { name: "确认连接", exact: true }).click();
  await expect(page.getByText("这个配对请求已经过期或处理过了，请重新发起", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "取消", exact: true })).toBeEnabled();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toBeHidden();
  await expectPairQueryCleared(page, entryURL);
  const renewedURL = await pushPairApproval(page, "pair-renewed");
  await expect(dialog.getByRole("button", { name: "确认连接", exact: true })).toBeEnabled();
  await dialog.getByRole("button", { name: "确认连接", exact: true }).click();
  await expect(dialog.getByText("授权完成，请回到插件", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "完成", exact: true }).click();
  await expectPairQueryCleared(page, renewedURL);
  expect(pairApprovalCalls(state).map((call) => call.body.pairing_id)).toEqual(["pair-expired", "pair-renewed"]);
  expect(state.errors).toEqual([]);
});

test("配对端口无效时不提交授权且仍可取消关闭", async ({ page }) => {
  const state = await mockResumeSession(page), entryURL = pairApprovalURL("pair-invalid-port", 80);
  await page.goto(entryURL);
  const dialog = page.getByRole("dialog", { name: pairDialogTitle, exact: true });
  await dialog.getByRole("button", { name: "确认连接", exact: true }).click();
  await expect(page.getByText("配对地址不完整，请重新从插件连接", { exact: true })).toBeVisible();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toBeHidden();
  await expectPairQueryCleared(page, entryURL);
  expect(pairApprovalCalls(state)).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

/** 添加方式直接切换且草稿分别保留，避免改版后重新出现两层选择弹框。 */
test("添加默认AI三标签直接切换，文件文本和手工草稿各自保留，插件引导可前往下载", async ({ page }) => {
  const state = await mockResumeSession(page); state.taskListEmpty = true;
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "添加", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "添加简历", exact: true });
  await expect(dialog.getByRole("tab")).toHaveCount(3);
  await expect(dialog.getByRole("tab", { name: "AI 批量上传", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(dialog.getByRole("button", { name: "继续", exact: true })).toHaveCount(0);
  await dialog.getByPlaceholder("也可以粘贴一份简历文本。").fill("合成AI文本草稿，尚未解析。");
  await dialog.locator('input[type="file"]').setInputFiles({ name: "保留的合成简历.txt", mimeType: "text/plain", buffer: Buffer.from("仅用于测试的合成简历") });
  await expect(dialog.getByText("保留的合成简历.txt", { exact: true })).toBeVisible();
  await dialog.getByRole("tab", { name: "手动添加", exact: true }).click();
  await dialog.getByLabel("姓名", { exact: true }).fill("手工草稿候选人");
  await dialog.getByLabel("手机号", { exact: true }).fill("13900139000");
  await dialog.getByLabel("首条备注", { exact: true }).fill("切换标签后仍需保留的备注");
  await dialog.getByRole("tab", { name: "浏览器插件一键导入", exact: true }).click();
  await expect(dialog.getByText("当前支持：猎聘企业端、猎聘猎头端、智联招聘、前程无忧", { exact: true })).toBeVisible();
  await expect(dialog.getByText(/Chrome 和 Firefox 都可以下载/)).toBeVisible();
  await expect(dialog.getByRole("link", { name: /前往下载$/ })).toHaveAttribute("href", "/download#browser-extension");
  await expect(dialog.getByRole("link", { name: /前往下载$/ })).toHaveAttribute("target", "_blank");
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "add-plugin-guide.png"), fullPage: true, animations: "disabled" });
  await dialog.getByRole("tab", { name: "AI 批量上传", exact: true }).click();
  await expect(dialog.getByPlaceholder("也可以粘贴一份简历文本。")).toHaveValue("合成AI文本草稿，尚未解析。");
  await expect(dialog.getByText("保留的合成简历.txt", { exact: true })).toBeVisible();
  await page.screenshot({ path: resolve(screenshotDirectory, "add-ai-tabs.png"), fullPage: true, animations: "disabled" });
  await dialog.getByRole("tab", { name: "手动添加", exact: true }).click();
  await expect(dialog.getByLabel("姓名", { exact: true })).toHaveValue("手工草稿候选人");
  await expect(dialog.getByLabel("手机号", { exact: true })).toHaveValue("13900139000");
  await expect(dialog.getByLabel("首条备注", { exact: true })).toHaveValue("切换标签后仍需保留的备注");
  await page.screenshot({ path: resolve(screenshotDirectory, "add-manual-tabs.png"), fullPage: true, animations: "disabled" });
  expect(state.calls.filter((call) => call.method === "POST" && (call.path === "/api/candidates" || call.path.endsWith("/notes") || call.path.endsWith("/recommendations") || call.path.startsWith("/api/v1/resume-import")))).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

/** 顶部下载入口保持为紧凑文字，直接进入统一下载页，具体安装包仍在下载页展示教程。 */
test("简历库顶部用文字链接直接前往浏览器插件下载页", async ({ page }) => {
  const state = await mockResumeSession(page);
  await page.goto("/admin/resumes");
  await expect(page.getByText(/浏览器插件支持 猎聘企业端、猎聘猎头端、智联招聘、前程无忧/)).toBeVisible();
  const download = page.getByRole("link", { name: "下载浏览器插件", exact: true });
  await expect(download).toHaveAttribute("href", "/download#browser-extension");
  await expect(download).toHaveAttribute("target", "_blank");
  await expect(download).not.toHaveClass(/ant-btn/);
  expect(state.errors).toEqual([]);
});

/** 后台广告必须保持单行纯文字，数量变化时始终等分可用宽度。 */
test("后台广告用单行纯文字等宽展示，长文案显示省略号", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.appConfig = { admin_banners: [
    { enabled: true, text: "这是一条用于验证省略号的很长广告文字".repeat(12), url: "https://example.invalid/one", text_color: "#20413a" },
    { enabled: true, text: "第二条可点击广告", url: "https://example.invalid/two" },
    { enabled: true, text: "第三条可点击广告", url: "https://example.invalid/three" },
  ] };
  await page.setViewportSize({ width: 1115, height: 800 });
  await page.goto("/admin/resumes");
  const banners = page.getByRole("navigation", { name: "GoodHR 公告与推荐", exact: true });
  const links = banners.getByRole("link");
  await expect(links).toHaveCount(3);
  await expect(banners.locator(".ant-alert")).toHaveCount(0);
  const metrics = await links.evaluateAll((elements) => elements.map((element) => {
    const box = element.getBoundingClientRect(), style = getComputedStyle(element);
    return { width: box.width, y: box.y, height: box.height, clientWidth: element.clientWidth, scrollWidth: element.scrollWidth, whiteSpace: style.whiteSpace, overflow: style.overflow, textOverflow: style.textOverflow };
  }));
  expect(Math.max(...metrics.map((item) => item.width)) - Math.min(...metrics.map((item) => item.width))).toBeLessThanOrEqual(1);
  expect(new Set(metrics.map((item) => item.y)).size).toBe(1);
  expect(metrics.every((item) => item.height <= 26 && item.whiteSpace === "nowrap" && item.overflow === "hidden" && item.textOverflow === "ellipsis")).toBe(true);
  expect(metrics[0].scrollWidth).toBeGreaterThan(metrics[0].clientWidth);
  const verticalMetrics = await page.locator(".goodhr-admin-toolbar, .goodhr-admin-banners, .goodhr-admin-content").evaluateAll((elements) => elements.map((element) => {
    const box = element.getBoundingClientRect();
    return { top: box.top, bottom: box.bottom, height: box.height };
  }));
  const [toolbar, bannerRow, content] = verticalMetrics;
  expect(bannerRow.height).toBeLessThanOrEqual(18);
  expect(bannerRow.top).toBeGreaterThanOrEqual(toolbar.bottom);
  expect(bannerRow.top - toolbar.bottom).toBeLessThanOrEqual(6);
  expect(content.top - bannerRow.bottom).toBeLessThanOrEqual(4);
  await page.setViewportSize({ width: 760, height: 800 });
  const mobileVerticalMetrics = await page.locator(".goodhr-admin-toolbar, .goodhr-admin-banners, .goodhr-admin-content").evaluateAll((elements) => elements.map((element) => {
    const box = element.getBoundingClientRect();
    return { top: box.top, bottom: box.bottom };
  }));
  const [mobileToolbar, mobileBannerRow, mobileContent] = mobileVerticalMetrics;
  expect(mobileBannerRow.top).toBeGreaterThanOrEqual(mobileToolbar.bottom);
  expect(mobileBannerRow.top - mobileToolbar.bottom).toBeLessThanOrEqual(6);
  expect(mobileContent.top - mobileBannerRow.bottom).toBeLessThanOrEqual(4);
  expect(state.errors).toEqual([]);
});

/** 真实旧版Agent响应为纯文本404，界面应提供更新路径并继续允许其他添加方式。 */
test("本地导入纯文本404提示更新，手动及插件仍可用且不展示JSON解析异常", async ({ page }) => {
  const state = await mockResumeSession(page); state.taskPlain404 = true;
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "添加", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "添加简历", exact: true });
  await expect(dialog.getByText(/请更新到 6\.2\.10\.3 或更新版本/)).toBeVisible();
  await expect(dialog.getByRole("link", { name: "去下载更新", exact: true })).toHaveAttribute("href", "/download#local-agent");
  await expect(dialog).not.toContainText("Unexpected non-whitespace character");
  await expect(dialog).not.toContainText("SyntaxError");
  await dialog.getByRole("tab", { name: "手动添加", exact: true }).click();
  await dialog.getByLabel("姓名", { exact: true }).fill("旧版程序下的手工候选人");
  await dialog.getByLabel("手机号", { exact: true }).fill("13900139000");
  await expect(dialog.getByRole("button", { name: "检查并保存", exact: true })).toBeEnabled();
  await dialog.getByRole("tab", { name: "浏览器插件一键导入", exact: true }).click();
  await expect(dialog.getByRole("link", { name: /前往下载$/ })).toBeVisible();
  await dialog.getByRole("tab", { name: "手动添加", exact: true }).click();
  await dialog.getByRole("button", { name: "检查并保存", exact: true }).click();
  await page.getByRole("dialog", { name: "确认入库内容", exact: true }).getByRole("button", { name: "确认保存", exact: true }).click();
  await expect(dialog).toBeHidden();
  expect(state.calls.filter((call) => call.path === "/api/candidates" && call.method === "POST")).toHaveLength(1);
  expect(state.calls.filter((call) => call.path.startsWith("/api/v1/resume-import") && call.method === "POST")).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

/** 用实际元素尺寸和字重验证候选人摘要的邻接、居中与公司重点，避免只检查class存在。 */
test("候选人手机号紧邻姓名且头像垂直居中，公司名称比职位日期更粗", async ({ page }) => {
  const state = await mockResumeSession(page);
  Object.assign(state.candidates[0], { avatar_data_url: syntheticAvatar, gender: "male", age: 34, work_years: "8", expected_position: "软件研发负责人", expected_region: "合成城市示例区域", expected_salary_text: "面议", work_status: "在职，考虑机会", tags: ["合成示例标签", "无真实履历"] });
  await page.setViewportSize({ width: 1920, height: 1000 });
  await page.goto("/admin/resumes");
  const row = page.getByRole("row").filter({ has: page.getByRole("button", { name: "原有候选人", exact: true }) });
  const name = row.getByRole("button", { name: "原有候选人", exact: true });
  const phone = row.getByText(existing.phone, { exact: true });
  const avatar = row.getByRole("img", { name: "原有候选人头像", exact: true });
  const summary = name.locator("..").locator("..");
  await expect(avatar).toBeVisible();
  const [nameBox, phoneBox, avatarBox, summaryBox] = await Promise.all([name.boundingBox(), phone.boundingBox(), avatar.boundingBox(), summary.boundingBox()]);
  if (!nameBox || !phoneBox || !avatarBox || !summaryBox) throw new Error("候选人姓名、手机号或头像的实际尺寸没有读到。");
  expect(phoneBox.x - nameBox.x - nameBox.width).toBeGreaterThanOrEqual(6);
  expect(phoneBox.x - nameBox.x - nameBox.width).toBeLessThanOrEqual(10);
  expect(Math.abs(phoneBox.y + phoneBox.height / 2 - nameBox.y - nameBox.height / 2)).toBeLessThanOrEqual(2);
  expect(summaryBox.height).toBeGreaterThan(avatarBox.height);
  expect(Math.abs(avatarBox.y + avatarBox.height / 2 - summaryBox.y - summaryBox.height / 2)).toBeLessThanOrEqual(1);
  const company = row.getByText("原有公司", { exact: true });
  const companyWeight = await company.evaluate((element) => Number(getComputedStyle(element).fontWeight));
  const bodyWeight = await company.locator("..").evaluate((element) => Number(getComputedStyle(element).fontWeight));
  expect(companyWeight).toBeGreaterThan(bodyWeight);
  const privacyNotice = page.getByTitle("请勿非法买卖简历。平台不会出售您的简历，分享权限由您掌握。浏览器插件支持猎聘企业端、猎聘猎头端、智联招聘、前程无忧。", { exact: true });
  await expect(privacyNotice).toBeVisible();
  const [noticeBox, filtersBox] = await Promise.all([privacyNotice.boundingBox(), page.getByTestId("resume-filters").boundingBox()]);
  if (!noticeBox || !filtersBox) throw new Error("简历保护提示或搜索栏没有显示。");
  expect(noticeBox.y + noticeBox.height).toBeLessThanOrEqual(filtersBox.y);
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "resume-protection-desktop.png"), fullPage: true, animations: "disabled" });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(privacyNotice).toBeVisible();
  await page.screenshot({ path: resolve(screenshotDirectory, "resume-protection-mobile.png"), fullPage: true, animations: "disabled" });
  await page.setViewportSize({ width: 1920, height: 1000 });
  await name.click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  const detailName = drawer.getByRole("heading", { name: "原有候选人", exact: true });
  const detailPhone = drawer.getByText(existing.phone, { exact: true });
  const [detailNameBox, detailPhoneBox] = await Promise.all([detailName.boundingBox(), detailPhone.boundingBox()]);
  if (!detailNameBox || !detailPhoneBox) throw new Error("详情姓名或手机号的实际尺寸没有读到。");
  expect(detailPhoneBox.x - detailNameBox.x - detailNameBox.width).toBeGreaterThanOrEqual(8);
  expect(detailPhoneBox.x - detailNameBox.x - detailNameBox.width).toBeLessThanOrEqual(12);
  const detailCompany = drawer.getByText("原有公司", { exact: true });
  expect(await detailCompany.evaluate((element) => Number(getComputedStyle(element).fontWeight))).toBeGreaterThan(await detailCompany.locator("..").evaluate((element) => Number(getComputedStyle(element).fontWeight)));
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "candidate-compact-profile.png"), fullPage: true, animations: "disabled" });
  expect(state.errors).toEqual([]);
});

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
  await chooser.getByRole("tab", { name: "AI 批量上传", exact: true }).click();
  const importer = chooser;
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

test("内置示例登录后可读，预留身份缺标记仍不能编辑、分享、摘要或添加记录", async ({ page }) => {
  const state = await mockResumeSession(page);
  const builtinID = "00000000-0000-4000-8000-000000000001";
  // 联系方式使用用户明确授权的共享示例，其余职业资料均为合成虚构信息。
  state.candidates = [{ id: builtinID, candidate_name: "瓜瓜", phone: "17607080935", email: "1224299352@qq.com", source_type: "builtin", is_builtin: true, readonly: true, current_region: "四川成都", expected_region: "四川成都", expected_position: "软件开发", educations: [{ school_name: "示例计算机学院（虚构）", major_name: "软件工程（示例）" }], work_experiences: [{ company_name: "示例软件工作室（虚构）", position_name: "软件开发工程师（示例）", content: "合成工作内容" }] }];
  state.notes = [{ id: "builtin-note", content: "这份简历是管理员内置的只读示例，不可修改或删除。您的简历库需要登录后才能访问，真实简历仍按账号和团队权限隔离。", author_name: "管理员内置" }];
  await page.goto("/admin/resumes");
  await expect(page.getByText("共 0 份简历 · 另有 1 份内置示例", { exact: true })).toBeVisible();
  const row = page.getByRole("row").filter({ has: page.getByRole("button", { name: "瓜瓜", exact: true }) });
  const builtinAvatar = row.getByRole("img", { name: "瓜瓜头像", exact: true });
  await expect(builtinAvatar).toHaveAttribute("src", "/assets/resume-guagua.jpg");
  await expect.poll(() => builtinAvatar.evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 100 && image.naturalHeight > 100)).toBe(true);
  await expect(row.locator(".ant-avatar-string")).toHaveCount(0);
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "builtin-guagua-avatar.png"), fullPage: true, animations: "disabled" });
  await expect(row.getByRole("button", { name: "编辑", exact: true })).toHaveCount(0);
  await expect(row.getByRole("button", { name: "瓜瓜更多操作" })).toHaveCount(0);
  await row.getByRole("button", { name: "瓜瓜", exact: true }).click();
  const workspace = page.getByTestId("resume-detail-layout");
  await expect(workspace.getByRole("img", { name: "瓜瓜头像", exact: true })).toHaveAttribute("src", "/assets/resume-guagua.jpg");
  await expect.poll(() => workspace.getByRole("img", { name: "瓜瓜头像", exact: true }).evaluate((image: HTMLImageElement) => image.naturalWidth)).toBeGreaterThan(100);
  await expect(page.getByTestId("resume-builtin-notice")).toBeVisible();
  await expect(workspace.getByRole("button", { name: "编辑", exact: true })).toHaveCount(0);
  await expect(workspace.getByRole("button", { name: "分享", exact: true })).toHaveCount(0);
  await expect(workspace.getByText("企业招聘流程", { exact: true })).toHaveCount(0);
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
  await expect(page.getByRole("button", { name: "分享", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "编辑", exact: true })).toHaveCount(0);
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
  const updater = editor;
  await expect(updater.getByText(/将比较更新已有简历：原有候选人/)).toBeVisible();
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

for (const viewport of [{ width: 1440, height: 1000 }, { width: 390, height: 844 }]) {
  test(`候选人长备注保持限高并内部滚动，输入和加备注同行 ${viewport.width}`, async ({ page }) => {
    const state = await mockResumeSession(page);
    state.notes = Array.from({ length: 30 }, (_, index) => ({ id: `bounded-note-${index}`, content: `合成长备注 ${index + 1}：${"已沟通岗位意向，等待下次跟进。".repeat(24)}`, author_name: "合成HR", occurred_at: "2026-10-16T09:00:00+08:00" }));
    await page.setViewportSize(viewport);
    await page.goto("/admin/resumes");
    await page.getByRole("button", { name: "原有候选人", exact: true }).click();
    const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
    const notes = drawer.getByTestId("resume-notes-section");
    const history = notes.getByRole("region", { name: "历史备注", exact: true });
    const input = notes.getByPlaceholder("比如：意向不错，周五再约一次。");
    const add = notes.getByRole("button", { name: "加备注", exact: true });
    await expect(history.getByText(/^合成长备注/)).toHaveCount(30);
    await notes.scrollIntoViewIfNeeded();
    const initialHeight = (await history.boundingBox())!.height;
    expect(initialHeight).toBeLessThanOrEqual(Math.min(320, viewport.height * 0.4) + 1);
    expect(await history.evaluate((element) => element.scrollHeight)).toBeGreaterThan(initialHeight * 2);
    const inputBox = (await input.boundingBox())!;
    const addBox = (await add.boundingBox())!;
    expect(addBox.x).toBeGreaterThanOrEqual(inputBox.x + inputBox.width);
    expect(addBox.y).toBeGreaterThanOrEqual(inputBox.y);
    expect(addBox.y + addBox.height).toBeLessThanOrEqual(inputBox.y + inputBox.height + 1);
    await history.hover();
    await page.mouse.wheel(0, 700);
    await expect.poll(() => history.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
    await input.fill("合成新增备注：内部滚动后仍能保存。");
    await add.click();
    await expect(notes.getByRole("heading", { name: "备注 · 31", exact: true })).toBeVisible();
    await expect(history.getByText("合成新增备注：内部滚动后仍能保存。", { exact: true })).toHaveCount(1);
    await expect(input).toHaveValue("");
    expect((await history.boundingBox())!.height).toBeCloseTo(initialHeight, 0);
    expect(state.calls.filter((call) => call.path === `/api/candidates/${existing.id}/notes` && call.method === "POST")).toHaveLength(1);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(viewport.width);
    // 手机上的保存通知可能落在当前鼠标位置，先移开，避免悬停让自动关闭计时暂停。
    await page.mouse.move(1, 1);
    await dismissNotices(page);
    await notes.scrollIntoViewIfNeeded();
    mkdirSync(screenshotDirectory, { recursive: true });
    await page.screenshot({ path: resolve(screenshotDirectory, `notes-bounded-${viewport.width}.png`), fullPage: true, animations: "disabled" });
    expect(state.errors).toEqual([]);
  });
}

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
  await page.getByRole("row").filter({ has: page.getByRole("button", { name: "原有候选人", exact: true }) }).getByRole("button", { name: "推荐", exact: true }).click();
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
  await chooser.getByRole("tab", { name: "AI 批量上传", exact: true }).click();
  const importer = chooser;
  await expect(importer.getByText("待补手机号", { exact: true })).toBeVisible();
  await expect(importer.getByRole("button", { name: "确认无重复的完整草稿" })).toBeDisabled();
  await importer.getByRole("button", { name: "预览 / 补充" }).click();
  const editor = importer;
  await editor.getByLabel("手机号", { exact: true }).fill("13700137000"); await editor.getByRole("button", { name: "检查并保存" }).click();
  await page.getByRole("dialog", { name: "确认入库内容" }).getByRole("button", { name: "确认保存" }).click();
  await expect(importer.getByText("已入库", { exact: true }).first()).toBeVisible();
  expect(state.calls.filter((call) => call.path.endsWith("/confirm"))).toHaveLength(1);
  expect(state.errors).toEqual([]);
});

test("AI草稿比较更新只提交预览中的有效改动，空邮箱和空经历保留已有内容", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.candidates[0].email = "preserved@example.invalid";
  state.task.items[0] = { id: "import-item", file_name: "比较更新.txt", status: "ready", candidate: { candidate_name: "解析候选人", phone: existing.phone, email: "", work_experiences: [], expected_salary_min: null, source_type: "ai_import", avatar_data_url: syntheticAvatar, tags: ["AI原始Java", "AI原始产品经验"] } };
  await page.goto("/admin/resumes");
  await page.evaluate(() => localStorage.setItem("goodhr5_resume_import:resume-user", "import-test"));
  await page.getByRole("button", { name: "添加", exact: true }).click();
  const chooser = page.getByRole("dialog", { name: "添加简历", exact: true });
  await chooser.getByRole("tab", { name: "AI 批量上传", exact: true }).click();
  const importer = chooser;
  await importer.getByRole("button", { name: "预览 / 补充" }).click();
  const editor = importer;
  await editor.getByRole("button", { name: "检查并保存" }).click();
  await editor.getByRole("button", { name: "比较更新" }).click();
  const updater = editor;
  await expect(updater.getByText(/将比较更新已有简历：原有候选人/)).toBeVisible();
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
  expect(submitted.candidate).toEqual({ candidate_name: "解析候选人", phone: existing.phone, row_version: 3, avatar_data_url: syntheticAvatar, tags: ["AI原始Java", "AI原始产品经验"] });
  expect(submitted.candidate).not.toHaveProperty("email");
  expect(submitted.candidate).not.toHaveProperty("work_experiences");
  expect(state.candidates).toHaveLength(1);
  expect(state.candidates[0].email).toBe("preserved@example.invalid");
  expect(state.candidates[0].work_experiences).toEqual(existing.work_experiences);
  expect(state.candidates[0].source_type).toBe("automatic");
  expect(state.candidates[0].tags).toEqual(["AI原始Java", "AI原始产品经验"]);
  expect(submitted.candidate).not.toHaveProperty("folder_ids");
  expect(state.folders || []).toEqual([]);
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
  await chooser.getByRole("tab", { name: "AI 批量上传", exact: true }).click();
  const importer = chooser;
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
  await chooser.getByRole("tab", { name: "AI 批量上传", exact: true }).click();
  const importer = chooser;
  await expect(importer.getByText("简历已保存，备注 / 岗位待补齐", { exact: true })).toBeVisible();
  await importer.getByRole("button", { name: "继续完成记录" }).click();
  await expect(importer.getByText("已入库", { exact: true }).first()).toBeVisible();
  const confirms = state.calls.filter((call) => call.path.endsWith("/confirm"));
  expect(confirms).toHaveLength(1); expect(confirms[0].body).toEqual({ items: [{ id: "import-item" }] });
  expect(state.calls.some((call) => call.path.endsWith("/retry"))).toBe(false);
  expect(state.calls.some((call) => call.path === "/api/candidates" && call.method === "POST")).toBe(false);
  expect(state.errors).toEqual([]);
});

/** 插件来源筛选必须把平台值实际传给后端，不能仅改变标签。 */
test("四个平台插件来源分别查询并显示中文标签", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.candidates[0].source_type = "browser_extension"; state.candidates[0].platform_id = "hliepin";
  await page.goto("/admin/resumes");
  await expect(page.getByText("猎聘插件", { exact: true })).toBeVisible();
  await page.getByRole("combobox", { name: "来源筛选" }).click();
  await page.getByText("Boss插件", { exact: true }).click();
  await page.getByRole("button", { name: /查询$/ }).click();
  await expect.poll(() => state.calls.filter((call) => call.path === "/api/candidates" && call.method === "GET").at(-1)?.query.get("source_type")).toBe("browser_extension:boss");
  expect(state.errors).toEqual([]);
});

/** 设置持久化只修改下载开关，重新打开仍显示保存后的值。 */
test("简历库下载导入默认开启并可保存关闭", async ({ page }) => {
  const state = await mockResumeSession(page);
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: "设置", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "简历库设置" });
  const toggle = dialog.getByRole("switch", { name: "将下载的简历通过AI导入到GoodHR" });
  await expect(toggle).toBeChecked(); await expect(toggle).toBeEnabled();
  await toggle.click(); await dialog.getByRole("button", { name: "保存设置" }).click();
  await expect(dialog).toBeHidden();
  expect(state.calls.find((call) => call.path === "/api/resume-library/settings" && call.method === "PATCH")?.body).toEqual({ auto_import_downloads: false });
  await page.getByRole("button", { name: "设置", exact: true }).click(); await expect(toggle).not.toBeChecked();
  expect(state.errors).toEqual([]);
});

/** 消息只显示标题，打开面板不标记已读，展开后恢复对应本地导入草稿。 */
test("铃铛展开消息才已读并可打开失败导入任务", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.messages = [{ id: "message-one", title: "这份下载简历还没入库", body: "手机号没有解析完整，请补充后确认。", action_url: "/admin/resumes?resume_import_id=download-task", created_at: "2026-10-10T20:00:00+08:00" }];
  state.task.id = "download-task";
  await page.goto("/admin/resumes");
  const bell = page.getByRole("button", { name: "消息中心", exact: true });
  await expect(bell).toHaveAttribute("title", "1 条未读消息"); await bell.click();
  const panel = page.getByTestId("message-center-panel");
  await expect(panel.getByText("这份下载简历还没入库", { exact: true })).toBeVisible();
  await expect(panel.getByText("手机号没有解析完整，请补充后确认。", { exact: true })).toHaveCount(0);
  expect(state.calls.filter((call) => call.path.endsWith("/read"))).toHaveLength(0);
  await panel.getByText("这份下载简历还没入库", { exact: true }).click();
  await expect(panel.getByText("手机号没有解析完整，请补充后确认。", { exact: true })).toBeVisible();
  await expect(bell).toHaveAttribute("title", "消息中心");
  await panel.getByRole("link", { name: "去处理" }).click();
  await expect(page.getByRole("dialog", { name: "添加简历", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "AI 批量上传", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect.poll(() => state.calls.some((call) => call.path === "/api/v1/resume-import/tasks/download-task")).toBe(true);
  expect(state.calls.filter((call) => call.path.endsWith("/read"))).toHaveLength(1);
  await page.screenshot({ path: `${screenshotDirectory}/download-message-task.png`, fullPage: true });
  expect(state.errors).toEqual([]);
});

/** 慢请求结束后才安排下一轮，避免每10秒叠加同一个查询。 */
test("消息状态每十秒串行检查且关闭面板不查正文", async ({ page }) => {
  const state = await mockResumeSession(page);
  let release!: () => void; state.holdMessageSummary = new Promise<void>((resolve) => { release = resolve; });
  await page.goto("/admin/resumes");
  await expect(page.getByRole("button", { name: "消息中心", exact: true })).toBeVisible();
  await page.waitForTimeout(250);
  const initialRequests = state.calls.filter((call) => call.path === "/api/messages/summary").length;
  expect(initialRequests).toBeGreaterThan(0);
  await page.waitForTimeout(11000);
  expect(state.calls.filter((call) => call.path === "/api/messages/summary")).toHaveLength(initialRequests);
  state.holdMessageSummary = undefined; release();
  await expect.poll(() => state.calls.filter((call) => call.path === "/api/messages/summary").length, { timeout: 13000 }).toBe(initialRequests + 1);
  expect(state.calls.filter((call) => call.path === "/api/messages" || call.path.endsWith("/read"))).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

/** 旧轮询在已读请求后回包时，不能把红点数量和版本退回旧值。 */
test("迟到的轮询不会覆盖展开后的已读状态", async ({ page }) => {
  const state = await mockResumeSession(page);
  state.messages = [{ id: "late-message", title: "待处理的下载", body: "请核对手机号。" }];
  await page.goto("/admin/resumes");
  const bell = page.getByRole("button", { name: "消息中心", exact: true });
  await expect(bell).toHaveAttribute("title", "1 条未读消息");
  let release!: () => void; state.holdMessageSummary = new Promise<void>((resolve) => { release = resolve; });
  const baseline = state.calls.filter((call) => call.path === "/api/messages/summary").length;
  await expect.poll(() => state.calls.filter((call) => call.path === "/api/messages/summary").length, { timeout: 13000 }).toBe(baseline + 1);
  await bell.click(); await page.getByTestId("message-center-panel").getByText("待处理的下载", { exact: true }).click();
  await expect(bell).toHaveAttribute("title", "消息中心");
  state.holdMessageSummary = undefined; release(); await page.waitForTimeout(300);
  await expect(bell).toHaveAttribute("title", "消息中心");
  expect(state.errors).toEqual([]);
});
