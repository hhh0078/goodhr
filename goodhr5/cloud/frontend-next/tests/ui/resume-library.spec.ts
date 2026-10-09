/** 文件作用说明：用完全模拟的云端和本地接口验证新简历库查重、保存重试、推荐记录和缺号草稿，避免真实数据写入。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const previewOrigin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
const screenshotDirectory = resolve("artifacts/resume-library");
type Call = { path: string; method: string; body: any; query: URLSearchParams };
type State = { calls: Call[]; notes: any[]; recommendations: any[]; candidates: any[]; failFirstNote: boolean; failConfirmAt?: number; task: any; errors: string[] };
const existing = { id: "resume-existing", candidate_name: "原有候选人", phone: "13800138000", row_version: 3, source_type: "automatic", education_level: "本科", expected_salary_min: null, expected_salary_max: null, work_experiences: [{ company_name: "原有公司", position_name: "产品经理", start_ym: "2021-01", content: "已有工作经历保留" }], user_email: "test@example.invalid", created_at: "2026-10-10T10:00:00+08:00", ai: { detail: { score: null, reason: "" } } };
test.use({ storageState: { cookies: [], origins: [{ origin: previewOrigin, localStorage: [{ name: "goodhr5_access_token", value: "resume-test-only" }] }] } });

/** mockResumeSession 拦截全部业务请求，模拟持久化草稿与失败重试，不访问招聘平台。 */
async function mockResumeSession(page: Page): Promise<State> {
  const state: State = { calls: [], notes: [], recommendations: [], candidates: [{ ...existing }], failFirstNote: false, errors: [], task: { id: "import-test", status: "completed", total: 1, processed: 1, items: [{ id: "import-item", file_name: "候选人.txt", status: "needs_phone", candidate: { candidate_name: "待补号候选人", source_type: "ai_import", phone: "" } }] } };
  page.on("pageerror", (error) => state.errors.push(error.message));
  await page.context().route("**/*", async (route) => {
    const request = route.request(); const url = new URL(request.url()); const method = request.method();
    if (url.origin === previewOrigin && !url.pathname.startsWith("/api/")) return route.continue();
    if (![previewOrigin, "http://127.0.0.1:8084", "http://127.0.0.1:43129", "https://goodhr.58it.cn"].includes(url.origin)) return route.abort();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    let body: any; try { body = request.postDataJSON(); } catch { /* 上传和 GET 无 JSON 正文。 */ }
    state.calls.push({ path: url.pathname, method, body, query: url.searchParams });
    let data: any = { ok: true };
    if (url.port === "43129") {
      if (url.pathname === "/health") data = { ok: true, version: "6.2.10.1" };
      else if (url.pathname === "/api/v1/runtime/status") data = { version: "6.2.10.1", node_installed: true, camoufox_installed: true, system_runtime: { ready: true, required: false } };
      else if (url.pathname === "/api/v1/app-update/status") data = { stage: "idle", current_version: "6.2.10.1" };
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
    } else if (url.pathname === "/api/auth/me") data = { user: { id: "resume-user", email: "test@example.invalid", role: "super_admin", tenant_id: "test-team", password_set: true } };
    else if (url.pathname === "/api/subscription/status") data = { subscription: { active: true, member_type: "plus", member_name: "Plus", remaining_days: 30, allow_ai: true, allow_auto_reply: true } };
    else if (url.pathname === "/api/config/notification-profile") data = { profile: { completed: true } };
    else if (url.pathname === "/api/runtime/config" || url.pathname === "/api/system/app-config") data = { config: {} };
    else if (url.pathname === "/api/user-flow") data = { flow: { version: 2, state: "completed", stage: "completed", steps: {} } };
    else if (url.pathname.includes("/invitations")) data = { invitations: [] };
    else if (url.pathname === "/api/ai-wallet") data = { wallet: { balance: "100", models: [], default_model: "test" } };
    else if (url.pathname === "/api/positions") data = { positions: [{ id: "position-one", name: "产品经理" }, { id: "position-two", name: "招聘顾问" }], total: 2 };
    else if (url.pathname === "/api/candidates/duplicates") data = { ok: true, hidden_match: false, matches: body?.phone === existing.phone && body?.exclude_id !== existing.id ? [{ candidate: existing, match_type: "exact", reasons: ["手机号相同"] }] : [] };
    else if (url.pathname === "/api/candidates") {
      if (method === "POST") { const candidate = { ...body, id: "resume-new", row_version: 1 }; state.candidates.push(candidate); data = { ok: true, candidate }; }
      else data = { candidates: state.candidates.map((candidate) => ({ ...candidate, notes: state.notes, recommendations: state.recommendations, recommendation_count: state.recommendations.length })), total: state.candidates.length, page: Number(url.searchParams.get("page")) || 1 };
    } else if (url.pathname.endsWith("/notes")) {
      if (method === "POST") {
        if (state.failFirstNote) { state.failFirstNote = false; return route.fulfill({ status: 503, headers, json: { error: { message: "模拟备注暂时没保存成功" } } }); }
        const note = { id: "new-note", content: body.content, author_name: "测试HR", created_at: "2026-10-10T10:01:00+08:00" }; state.notes.push(note); data = { note };
      } else data = { notes: state.notes };
    } else if (url.pathname.endsWith("/recommendations")) {
      if (method === "POST") {
        const recommendation = { id: `rec-${body.position_id}`, position_id: body.position_id, position_name: "产品经理", stage: body.stage, events: [{ id: "event-one", to_stage: body.stage, content: body.content, author_name: "测试HR", created_at: "2026-10-10T10:02:00+08:00" }] };
        state.recommendations = [recommendation]; data = { recommendation };
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
  await page.setViewportSize({ width: 1920, height: 1000 });
  await page.goto("/admin/resumes?phone=3800&position_id=position-one");
  mkdirSync(screenshotDirectory, { recursive: true });
  await expect(page.getByLabel("手机号搜索")).toHaveValue("3800");
  await expect(page.getByRole("button", { name: "原有候选人", exact: true })).toBeVisible();
  await expect(page.getByRole("dialog", { name: "准备必要组件" })).not.toBeVisible();
  await page.screenshot({ path: resolve(screenshotDirectory, "list-desktop.png"), fullPage: true, animations: "disabled" });
  await page.getByLabel("简历关键词").fill("产品"); await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page).toHaveURL(/q=%E4%BA%A7%E5%93%81/);
  await page.getByRole("button", { name: "原有候选人", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "原有候选人", exact: true });
  await expect(drawer.getByText("未填写", { exact: true }).first()).toBeVisible();
  await expect(drawer.getByText("0–0K", { exact: true })).toHaveCount(0);
  await expect(drawer.getByText("面议（按年包沟通）", { exact: true })).toBeVisible();
  await drawer.getByPlaceholder("比如：意向不错，周五再约一次。").fill("通用备注仅在备注接口保存");
  await drawer.getByRole("button", { name: "加备注", exact: true }).click();
  await expect(drawer.getByText("通用备注仅在备注接口保存", { exact: true })).toBeVisible();
  await drawer.getByRole("tab", { name: /^推荐/ }).click();
  await drawer.getByLabel("选择推荐岗位").click(); await page.getByText("产品经理", { exact: true }).last().click();
  await drawer.getByLabel("推荐阶段").click(); await page.getByText("已推荐", { exact: true }).last().click();
  await drawer.getByPlaceholder("只记这个岗位的推荐、面试和跟进情况。").fill("岗位专属备注");
  await drawer.getByRole("button", { name: "记录推荐", exact: true }).click();
  await expect(drawer.getByText("推进记录 · 1 条", { exact: true })).toBeVisible();
  await dismissNotices(page);
  await page.screenshot({ path: resolve(screenshotDirectory, "detail-desktop.png"), fullPage: true, animations: "disabled" });
  expect(state.recommendations[0].stage).toBe("recommended"); expect(state.recommendations[0].events[0].content).toBe("岗位专属备注");
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(drawer.getByRole("heading", { name: "原有候选人", exact: true })).toBeInViewport();
  const overflow = await page.locator("body").boundingBox(); expect(overflow?.width).toBeLessThanOrEqual(390);
  const documentWidth = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(documentWidth).toBeLessThanOrEqual(390);
  await page.screenshot({ path: resolve(screenshotDirectory, "detail-mobile.png"), fullPage: true, animations: "disabled" });
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
  state.task.items[0] = { id: "import-item", file_name: "比较更新.txt", status: "ready", candidate: { candidate_name: "解析候选人", phone: existing.phone, email: "", work_experiences: [], expected_salary_min: null, source_type: "ai_import" } };
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
  await preview.getByRole("button", { name: "确认保存" }).click();
  await expect(importer.getByText("已入库", { exact: true }).first()).toBeVisible();
  const confirms = state.calls.filter((call) => call.path.endsWith("/confirm"));
  expect(confirms).toHaveLength(1);
  const submitted = confirms[0].body.items[0];
  expect(submitted.candidate_id).toBe(existing.id);
  expect(submitted.candidate).toEqual({ candidate_name: "解析候选人", phone: existing.phone, row_version: 3 });
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
