/** 文件作用：为后台统一控件测试提供全拦截的合成云端与本地接口，不读取真实用户或启动本地任务。 */
import { expect, type Page } from "@playwright/test";
import { appendFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";

export const uniformOrigin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
export const uniformLongPosition = "合成长岗位名称 · 企业招聘与猎头沟通协作工具产品负责人（搜索选择时仍须完整可读）";
const auditFile = resolve(process.env.UI_UNIFORM_AUDIT_FILE || "../../../output/admin-uniform-20261015/api-fixture-audit.jsonl");
const createdAt = "2026-10-15T08:00:00Z";
export type UniformCall = { path: string; method: string; query: string; origin: string };
export type UniformState = { empty: boolean; calls: UniformCall[]; errors: string[]; unknown: string[]; writes: string[]; readMessages: string[] };

/** pageRows 按请求页码截取合成记录，验证分页确实读取第二页。 */
function pageRows<T>(rows: T[], url: URL, defaultSize = 20) {
  const page = Math.max(1, Number(url.searchParams.get("page")) || 1);
  const size = Math.max(1, Number(url.searchParams.get("page_size")) || defaultSize);
  return rows.slice((page - 1) * size, page * size);
}

/** mockUniformAdmin 将全部业务请求截到内存，未列出的接口报错，外部资源直接终止。 */
export async function mockUniformAdmin(page: Page, empty = false, withMessages = false): Promise<UniformState> {
  const state: UniformState = { empty, calls: [], errors: [], unknown: [], writes: [], readMessages: [] };
  const rows = Array.from({ length: 61 }, (_, index) => index + 1);
  const messages = rows.map((index) => ({ id: `uniform-message-${index}`, title: `合成消息${index}`, body: `只含合成内容的消息正文${index}`, created_at: createdAt }));
  const positions = rows.slice(0, 3).map((index) => ({
    id: `uniform-position-${index}`, name: index === 1 ? uniformLongPosition : `合成岗位${index}`,
    is_open: true, platform_id: "boss", creator_email: "uniform@example.invalid", status: "stopped",
    config: { mode: "candidate_search", keywords: "合成招聘", ai_enabled: false }, created_at: createdAt,
  }));
  const folders = [
    { id: "11111111-1111-4111-8111-111111111111", name: "合成团队收藏夹", scope: "team", candidate_count: 61, can_edit: true },
    { id: "22222222-2222-4222-8222-222222222222", name: "合成个人收藏夹", scope: "personal", candidate_count: 61, can_edit: true },
  ];
  const candidates = rows.map((index) => ({
    id: `uniform-candidate-${index}`, candidate_name: `合成候选人${index}`, phone: "00000000000",
    source_type: "manual", user_email: "uniform@example.invalid", education_level: "本科",
    current_region: "合成测试地区", expected_region: "合成测试地区", work_years: "5年",
    expected_position: "合成招聘产品岗位", work_status: "在职考虑机会", tags: ["合成技能"],
    work_experiences: [{ company_name: "合成测试公司", position_name: "合成产品岗位", content: "合成工作内容" }],
    folder_ids: folders.map((folder) => folder.id), folder_details: folders, row_version: 1, created_at: createdAt,
  }));
  const users = rows.map((index) => ({
    id: `uniform-user-${index}`, email: `synthetic-user-${index}@example.invalid`, role: "user", status: "active",
    resume_count: index, ai_balance: "12.3456", ai_balance_units: 123456,
    subscription: { active: true, member_type: "max", expires_at: "2026-11-15T08:00:00Z" },
    created_at: createdAt, last_login_at: createdAt, agent: { agent_version: "6.2.11.0", machine_id: "synthetic-machine" },
  }));
  const recommendations = rows.map((index) => ({
    id: `uniform-recommendation-${index}`, candidate_id: candidates[index - 1].id,
    candidate_name: candidates[index - 1].candidate_name, phone: "00000000000", position_id: positions[0].id,
    position_name: uniformLongPosition, position_is_open: true, stage: "recommended", can_edit: true, can_delete: true,
    updated_at: createdAt, created_at: createdAt, events: [{ id: `uniform-event-${index}`, content: "合成沟通记录", to_stage: "recommended", occurred_at: createdAt }],
  }));
  const applications = rows.map((index) => ({
    id: `uniform-application-${index}`, candidate_id: candidates[index - 1].id, candidate_name: candidates[index - 1].candidate_name,
    position_id: positions[0].id, position_name: uniformLongPosition, owner_email: "uniform@example.invalid",
    status: "active", stage: "review", workflow_name: "合成流程", current_node: 0, revision: 1,
    nodes: [{ id: "uniform-node", name: "合成审核", type: "review", owner_email: "uniform@example.invalid", interviewer_emails: [], approver_emails: [], cc_emails: [], approval_mode: "all", requires_feedback: false }],
    interviews: [], events: [], permissions: { can_manage: true, can_view_resume: true, can_advance: true },
  }));
  const logs = rows.map((index) => ({ id: `uniform-log-${index}`, category: "运行", account: "uniform@example.invalid", position_name: "合成岗位", message: `合成运行记录${index}`, created_at: createdAt, event: { input: {}, output: {} } }));
  const codes = rows.map((index) => ({ id: `uniform-code-${index}`, code: `SYNTHETIC-${index}`, days: 7, status: "unused", created_at: createdAt }));
  const orders = rows.map((index) => ({ order_no: `SYNTHETIC-ORDER-${index}`, user_email: "uniform@example.invalid", plan_name: "合成余额充值", amount_cents: 1000, bonus_amount_cents: 100, status: "paid", created_at: createdAt }));
  const records = rows.map((index) => ({ id: `uniform-record-${index}`, user_email: "uniform@example.invalid", category: "ai_usage", reason: "合成模型使用", change: "-0.0001", balance_after: "12.3456", created_at: createdAt }));
  const members = [{ email: "uniform@example.invalid", role: "owner", status: "active", created_at: createdAt }];
  const configs = [
    { config_key: "system.onboarding_config", description: "合成引导配置", config_value: { wechat_follow_reward_days: 2 }, enabled: true },
    { config_key: "system.app_config", description: "合成应用配置", config_value: { registration_enabled: true }, enabled: true },
    { config_key: "system.guide", description: "合成帮助配置", config_value: { cards: [] }, enabled: true },
  ];
  page.on("pageerror", (error) => state.errors.push(error.message));
  await page.context().route("**/*", async (route) => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname, method = request.method();
    const api = path.startsWith("/api/") || path === "/health";
    if (url.origin === uniformOrigin && !api) return route.continue();
    if (!api) return route.abort();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    /** fulfillFixture 先完成模拟回包再追加审计，不记录token、密码或请求正文。 */
    async function fulfillFixture(status: number, json?: unknown) {
      await route.fulfill(json === undefined ? { status, headers } : { status, headers, json });
      mkdirSync(dirname(auditFile), { recursive: true });
      appendFileSync(auditFile, JSON.stringify({ preview: uniformOrigin, origin: url.origin, path, method, status, mocked: true, real_service_called: false }) + "\n");
    }
    if (method === "OPTIONS") return fulfillFixture(204);
    state.calls.push({ path, method, query: url.search, origin: url.origin });
    // 后台初始化会自动绑定本地会话；仅对此合成握手回包，不执行真实绑定或任何任务操作。
    if (method === "POST" && path === "/api/v1/session/bind") return fulfillFixture(200, { ok: true, email: "uniform@example.invalid" });
    // 会话初始化上报引导状态也使用固定回包，合成记录不发往云端。
    if (method === "POST" && path === "/api/user-flow") return fulfillFixture(200, { ok: true, flow: { version: 2, state: "completed", stage: "completed", steps: {} } });
    // 公开页面尺寸回归通过真正的登录表单写入合成token，不使用初始化脚本注入存储。
    if (method === "POST" && path === "/api/auth/password-login") return fulfillFixture(200, { access_token: "uniform-synthetic-login-token", requires_password_setup: false });
    // 仅在消息测试实际展开时模拟已读，保存内存状态且不连接真实消息服务。
    if (method === "POST" && withMessages && /^\/api\/messages\/uniform-message-\d+\/read$/.test(path)) {
      const id = path.split("/")[3];
      if (!state.readMessages.includes(id)) state.readMessages.push(id);
      return fulfillFixture(200, { message: { ...messages.find((item) => item.id === id), read_at: createdAt }, summary: { unread_count: messages.length - state.readMessages.length, version: state.readMessages.length + 1 } });
    }
    if (method !== "GET") {
      state.writes.push(`${method} ${path}`);
      return fulfillFixture(409, { error: "界面尺寸测试不提交业务资料" });
    }
    let data: any;
    if (path === "/api/auth/me") data = { user: { id: "uniform-user", email: "uniform@example.invalid", role: "super_admin", tenant_id: "uniform-team", has_openid: true } };
    else if (path === "/api/auth/agreement-status") data = { agreement_accepted: true };
    else if (path === "/api/subscription/status") data = { subscription: { active: true, member_type: "max", member_name: "合成Max会员", remaining_days: 30, allow_ai: true, allow_auto_reply: true } };
    else if (path === "/api/config/notification-profile") data = { profile: { completed: true } };
    else if (path === "/api/wechat-official/status") data = { enabled: false, configured: false, bound: true, followed: true, gift_granted: true };
    else if (path === "/api/system/app-config" || path === "/api/runtime/config") data = { config: { builtin_ai: { models: [{ id: "synthetic-model", name: "合成测试模型", description: "仅界面模拟" }] } } };
    else if (path === "/api/system/default-prompts") data = { prompts: {} };
    else if (path === "/api/config/user-ai") data = { config: { enabled: false, model: "synthetic-model", base_url: "https://example.invalid/synthetic", api_key_set: false } };
    else if (path === "/api/config/user-preferences") data = { preferences: {} };
    else if (path === "/api/user-flow" || path === "/api/user-flow/state") data = { state: "completed", steps: {}, flow: { version: 2, state: "completed", stage: "completed", steps: {} } };
    else if (path === "/api/tenants/invitations/pending" || path === "/api/tenants/invitations") data = { invitations: [] };
    else if (path === "/api/messages/summary") data = { unread_count: withMessages ? messages.length - state.readMessages.length : 0, version: state.readMessages.length + 1 };
    else if (path === "/api/messages") data = { messages: withMessages ? pageRows(messages, url).map((item) => ({ ...item, read_at: state.readMessages.includes(item.id) ? createdAt : undefined })) : [], total: withMessages ? messages.length : 0 };
    else if (path === "/api/tenants/members") data = { members: empty ? [] : members, can_manage: true, today_greeted_count: 0 };
    else if (path === "/api/team/stats") data = { totals: {}, members: empty ? [] : members, start_date: "2026-10-01", end_date: "2026-10-15" };
    else if (path === "/api/ai-wallet") data = { balance: "12.3456", balance_units: 123456, bonus_balance: "0.0000", default_model: "synthetic-model", models: [{ id: "synthetic-model", name: "合成模型" }], wallet: { balance: "12.3456" } };
    else if (path === "/api/ai-wallet/records") data = { records: empty ? [] : pageRows(records, url), total: empty ? 0 : records.length };
    else if (path === "/api/subscription/plans") data = { plans: [] };
    else if (path === "/api/payment/orders") data = { orders: [] };
    else if (path === "/api/admin/payment/orders") data = { orders: empty ? [] : orders, stats: { monthly_ai_consumption: "1.2345" } };
    else if (path === "/api/admin/users") data = { users: empty ? [] : pageRows(users, url), total: empty ? 0 : users.length, stats: { today_registered_count: 0, agent_binding_count: 0, remaining_ai_balance: "12.3456", ai_recharge_amount: "10.00" } };
    else if (path === "/api/admin/logs") data = { logs: empty ? [] : pageRows(logs, url, 50), total: empty ? 0 : logs.length };
    else if (path === "/api/admin/activation-codes") data = { codes: empty ? [] : codes };
    else if (path === "/api/admin/emails") data = { batches: [] };
    else if (path === "/api/admin/system/configs/") data = { configs: empty ? [] : configs };
    else if (path === "/api/invitations/summary") data = { invite_id: "uniform-synthetic", config: {}, invitees: [] };
    else if (path === "/api/help/guide") data = { guide: { cards: [] } };
    else if (path === "/api/platforms/config/" || path === "/api/platforms") data = { platforms: [{ id: "boss", platform_id: "boss", name: "合成招聘平台", enabled: true, is_open: true, open: true }] };
    else if (path === "/api/platform-accounts") data = { accounts: empty ? [] : [{ id: "uniform-account", platform_id: "boss", display_name: "合成账号", status: "available" }] };
    else if (path === "/api/positions") data = { positions: empty ? [] : positions, total: empty ? 0 : positions.length };
    else if (path === "/api/candidates/folders") data = { ok: true, folders: empty ? [] : folders };
    else if (path === "/api/candidates") data = { candidates: empty ? [] : pageRows(candidates, url), total: empty ? 0 : candidates.length, page: Number(url.searchParams.get("page")) || 1 };
    else if (/^\/api\/candidates\/[^/]+\/notes$/.test(path)) data = { notes: [] };
    else if (/^\/api\/candidates\/[^/]+\/recommendations$/.test(path)) data = { recommendations: recommendations.slice(0, 1) };
    else if (/^\/api\/candidates\/[^/]+$/.test(path)) data = { candidate: candidates.find((candidate) => path.endsWith(candidate.id)) || candidates[0] };
    else if (path === "/api/candidate-recommendations") data = { ok: true, items: empty ? [] : pageRows(recommendations, url), total: empty ? 0 : recommendations.length };
    else if (path === "/api/recruitment/applications") data = { applications: empty ? [] : applications };
    else if (path === "/api/recruitment/workflows") data = { workflows: [], members, can_manage: true };
    else if (path === "/api/recruitment/company") data = { company: { company_name: "合成测试公司", contact_name: "合成联系人", contact_email: "uniform@example.invalid" } };
    else if (path === "/api/recruitment/mail/templates") data = { templates: [], placeholders: [] };
    else if (path === "/api/recruitment/mail-templates") data = { templates: [] };
    else if (path === "/api/recruitment/placeholders") data = { placeholders: [] };
    else if (path === "/health") data = { ok: true, ready: true, version: "6.2.11.0" };
    else if (path === "/api/v1/runtime/status") data = { version: "6.2.11.0", node_installed: true, camoufox_installed: true, system_runtime: { ready: true, required: false }, installed_versions: {}, extensions_dir: "C:/synthetic/extensions" };
    else if (path === "/api/v1/app-update/status") data = { stage: "idle", current_version: "6.2.11.0" };
    else if (path === "/api/v1/local/downloads") data = { downloads: [] };
    else if (path === "/api/v1/local/rules/status") data = { rules: [] };
    else if (path === "/api/v1/diagnostics") data = { version: "6.2.11.0", system: "synthetic" };
    else if (path === "/api/v1/message-center/messages") data = { messages: [], total: 0 };
    else if (/^\/api\/v1\/local\/positions\/[^/]+\/status$/.test(path)) data = { ok: true, status: "stopped", task_id: "", greeted_count: 0 };
    else {
      state.unknown.push(`${method} ${path}`);
      return fulfillFixture(501, { error: `缺少合成接口：${path}` });
    }
    return fulfillFixture(200, data);
  });
  return state;
}

/** expectUniformNetwork 确认页面没有未知业务请求、真实写入或前端运行异常。 */
export function expectUniformNetwork(state: UniformState) {
  expect(state.unknown, "全部业务API应有明确合成回包").toEqual([]);
  expect(state.writes, "尺寸检查不应提交业务操作").toEqual([]);
  expect(state.errors, "页面不应有运行异常").toEqual([]);
}
