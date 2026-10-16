/**
 * 文件作用说明：为候选人、面试和推荐布局提供只监听本机的合成 HTTP API，支持真实登录页预览，不连接数据库、招聘平台或邮件系统。
 * 运行：node tests/fixtures/candidate-detail-preview.mjs；可用 CANDIDATE_PREVIEW_PORT 指定端口（默认 18164）。
 * 前端需同时设置 NEXT_PUBLIC_CLOUD_API_BASE=http://127.0.0.1:18164、NEXT_PUBLIC_LOCAL_AGENT_PORT=18164。
 * 合成登录：preview@example.invalid / Preview123；验证码登录点击发送后自动填入 1234。
 * 数据仅保存在当前进程内，重启即恢复，所有邮件发送动作只追加合成记录。
 */
import { createServer } from "node:http";
import { pathToFileURL } from "node:url";

const loginEmail = "preview@example.invalid";
const loginPassword = "Preview123";
const initialTime = "2026-10-16T09:40:00+08:00";
const fakeAvatar = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAGUlEQVR4nGNwLAj5TwlmGDVg1IBRA4aLAQBSaQQfj40p1QAAAABJRU5ErkJggg==";

/** makeNode 创建具有完整空数组字段的合成流程节点，避免空数据影响界面验证。 */
function makeNode(id, name, type, status = "pending") {
  return { id, name, type, status, owner_email: loginEmail, interviewer_emails: [loginEmail, "reviewer@example.invalid"], approver_emails: [], cc_emails: ["cc@example.invalid"], approval_mode: "all", requires_feedback: type === "interview", mail_template_id: type === "interview" ? "mail-first" : "", attempt: 1, approvals: [] };
}

/** createPreviewState 生成独立的虚构候选人、两轮面试、多位评价、长备注与推荐历史。 */
export function createPreviewState() {
  const folders = [{ id: "folder-team", name: "重点候选人", scope: "team", candidate_count: 1, can_edit: true, created_by_email: loginEmail }];
  const positions = [
    { id: "position-product", name: "高级产品经理", platform: "boss", platform_id: "boss", is_open: true, enabled: true, status: "stopped", created_at: initialTime, ai_enabled: true, keywords: ["产品经理"], detail_requirement: "5 年以上产品经验，熟悉企业服务。" },
    { id: "position-consultant", name: "企业招聘解决方案顾问（长岗位名称演示）", platform: "liepin", platform_id: "liepin", is_open: true, enabled: true, status: "stopped", created_at: initialTime },
    { id: "position-closed", name: "招聘运营 · 历史岗位", platform: "boss", platform_id: "boss", is_open: false, enabled: false, status: "stopped", created_at: initialTime },
  ];
  const notes = [
    { id: "note-one", content: "已沟通求职意向，倾向企业服务产品方向；最快两周到岗，优先考虑杭州。", author_name: "周禾", author_email: loginEmail, occurred_at: "2026-10-16T09:30:00+08:00", created_at: "2026-10-16T09:30:00+08:00" },
    { id: "note-two", content: "候选人对岗位职责和团队目标问得很细。希望下一轮重点聊跨部门协作方式、产品规划和入职后的目标，已整理给业务负责人。", author_name: "苏宁", author_email: "reviewer@example.invalid", occurred_at: "2026-10-15T15:20:00+08:00", created_at: "2026-10-15T15:20:00+08:00" },
    ...Array.from({ length: 7 }, (_, index) => ({ id: `note-history-${index}`, content: `第 ${index + 1} 次合成跟进：确认沟通时间与岗位意向，历史备注在自己的区域内滚动，保留详情的阅读空间。`, author_name: "周禾", author_email: loginEmail, occurred_at: `2026-10-${String(14 - index).padStart(2, "0")}T10:00:00+08:00`, created_at: initialTime })),
  ];
  const recommendations = [
    { id: "rec-product", candidate_id: "candidate-linxiao", position_id: "position-product", position_name: positions[0].name, stage: "interview", author_name: "周禾", author_email: loginEmail, recommended_at: "2026-10-12T10:00:00+08:00", created_at: "2026-10-12T10:00:00+08:00", updated_at: "2026-10-16T09:20:00+08:00", events: [
      { id: "rec-event-1", from_stage: "draft", to_stage: "recommended", content: "简历已推荐给业务负责人，关注产品规划及落地能力。", author_name: "周禾", author_email: loginEmail, occurred_at: "2026-10-12T10:00:00+08:00", created_at: "2026-10-12T10:00:00+08:00" },
      { id: "rec-event-2", from_stage: "recommended", to_stage: "contacting", content: "业务负责人认可相关经验，候选人对岗位有兴趣，薪资区间基本匹配。", author_name: "苏宁", author_email: "reviewer@example.invalid", occurred_at: "2026-10-13T15:30:00+08:00", created_at: "2026-10-13T15:30:00+08:00" },
      { id: "rec-event-3", from_stage: "contacting", to_stage: "interview", content: "初试通过，已安排复试。请下一轮重点确认团队管理、复杂需求取舍和跨部门推进经验。\n候选人已阅读岗位介绍，愿意进一步了解团队。", author_name: "周禾", author_email: loginEmail, occurred_at: "2026-10-16T09:20:00+08:00", created_at: "2026-10-16T09:20:00+08:00" },
    ] },
    { id: "rec-consultant", candidate_id: "candidate-linxiao", position_id: "position-consultant", position_name: positions[1].name, stage: "contacting", author_name: "苏宁", author_email: "reviewer@example.invalid", created_at: "2026-10-13T11:00:00+08:00", updated_at: "2026-10-15T16:00:00+08:00", events: [{ id: "rec-event-4", from_stage: "recommended", to_stage: "contacting", content: "另一个岗位的独立跟进历史：已介绍岗位方向，等待候选人反馈，不与产品经理岗位的面试记录混在一起。", author_name: "苏宁", author_email: "reviewer@example.invalid", occurred_at: "2026-10-15T16:00:00+08:00", created_at: "2026-10-15T16:00:00+08:00" }] },
  ];
  const candidates = [
    { id: "candidate-linxiao", candidate_name: "林晓（演示）", phone: "13800000001", email: "linxiao@example.invalid", gender: "female", age: 29, avatar_data_url: fakeAvatar, work_years: "6", current_region: "杭州", expected_region: "杭州 / 上海", education_level: "本科", expected_position: "高级产品经理", work_status: "离职，随时到岗", expected_salary_min: 25, expected_salary_max: 35, source_type: "manual", row_version: 1, user_email: loginEmail, user_name: "周禾", tags: ["企业服务", "产品规划", "跨部门协作"], folder_ids: ["folder-team"], folder_details: folders, created_at: "2026-10-12T09:00:00+08:00", updated_at: initialTime,
      basic_info: "6 年企业服务产品经验，负责从需求访谈、产品规划到项目落地的完整流程。",
      personal_description: "习惯把复杂问题拆成可推进的小目标，重视用户反馈，也愿意深入一线理解业务。期待加入有清晰目标、沟通直接的团队。",
      ai_summary: "企业服务产品经验与岗位方向匹配，有跨部门协作和项目交付经历。面试可进一步确认团队管理深度及复杂场景下的需求取舍。",
      work_experiences: [
        { company_name: "合成星野科技有限公司", position_name: "高级产品经理", start_ym: "2022-03", end_ym: "2026-09", content: "负责企业招聘协同产品的规划与迭代，覆盖候选人管理、面试安排、评价收集及团队协作。\n通过业务访谈梳理流程，组织设计、研发和运营共同推进交付；持续跟踪使用反馈，推动复杂功能的简化。\n主导多团队协作项目，建立问题清单和验收标准，在进度、体验与实施成本之间做取舍。" },
        { company_name: "合成青禾信息技术有限公司", position_name: "产品经理", start_ym: "2019-07", end_ym: "2022-02", content: "参与企业服务平台从 0 到 1 建设，负责需求研究、交互方案和版本验收。\n建立客户问题分类与回访机制，提升团队对需求优先级的共同理解。" },
      ],
      educations: [{ school_name: "合成江南大学", major_name: "信息管理与信息系统", education_level: "本科", start_ym: "2015-09", end_ym: "2019-06", content: "校内项目负责人，参与产品设计与数据分析实践。" }],
      project_experiences: [{ project_name: "企业招聘协同平台", role_name: "产品负责人", start_ym: "2023-01", end_ym: "2025-12", content: "梳理招聘全流程，设计候选人档案、面试评价与推荐跟进模块，统一分散信息的展示方式。" }],
      events: [{ event_type: "resume_read", message_text: "合成自动任务：已读取候选人详情", created_at: "2026-10-12T09:10:00+08:00" }],
    },
    { id: "candidate-empty", candidate_name: "许安（空资料演示）", phone: "13800000002", email: "", source_type: "manual", education_level: "", tags: [], folder_ids: [], row_version: 1, created_at: initialTime, work_experiences: [], educations: [], events: [] },
  ];
  const nodes = [makeNode("node-review", "简历筛选", "review", "completed"), makeNode("node-first", "初试", "interview", "completed"), makeNode("node-second", "复试", "interview", "active"), makeNode("node-approval", "录用审批", "approval"), makeNode("node-offer", "Offer", "offer"), makeNode("node-onboarding", "入职", "onboarding")];
  const applications = [{ id: "application-linxiao", candidate_id: candidates[0].id, candidate_name: candidates[0].candidate_name, candidate_email: candidates[0].email, position_id: positions[0].id, position_name: positions[0].name, recommendation_id: recommendations[0].id, owner_email: loginEmail, workflow_id: "flow-default", workflow_version: 1, workflow_name: "标准招聘流程", status: "active", stage: "复试", current_node: 2, revision: 1, nodes,
    interviews: [
      { id: "interview-second", node_id: "node-second", attempt: 1, starts_at: "2026-10-16T14:00:00+08:00", duration_minutes: 45, mode: "onsite", location: "杭州 · 合成星野科技 · 3 楼向日葵会议室", meeting_url: "", interviewer_emails: [loginEmail, "reviewer@example.invalid"], status: "scheduled", confirmation: "pending", revision: 1, feedback: [{ author_email: "reviewer@example.invalid", conclusion: "pass", content: "沟通表达清楚，对企业服务业务有比较完整的理解，建议继续推进。", created_at: "2026-10-16T15:10:00+08:00" }] },
      { id: "interview-first", node_id: "node-first", attempt: 1, starts_at: "2026-10-14T10:00:00+08:00", duration_minutes: 60, mode: "video", location: "", meeting_url: "https://example.invalid/meeting/synthetic", interviewer_emails: [loginEmail, "reviewer@example.invalid", "manager@example.invalid"], status: "completed", confirmation: "confirmed", revision: 1, feedback: [
        { author_email: loginEmail, conclusion: "pass", content: "1. 能清楚说明项目背景、个人职责和关键决策。\n2. 对需求优先级的判断有方法，表达直接，愿意承认信息不足的地方。\n3. 有跨团队推进和复杂流程梳理经验，能够兼顾交付与用户体验。\n4. 期望方向与岗位匹配，下一轮建议重点聊管理职责和长线产品规划。", created_at: "2026-10-14T11:20:00+08:00" },
        { author_email: "manager@example.invalid", conclusion: "hold", content: "产品能力符合预期，团队管理经验还需进一步了解。建议复试补充一个多人协作项目，讲清分工、冲突与结果。", created_at: "2026-10-14T11:35:00+08:00" },
      ] },
    ],
    events: [
      { id: "event-start", type: "started", note: "从简历库开启高级产品经理的招聘流程。", author_email: loginEmail, created_at: "2026-10-12T10:15:00+08:00", notify_emails: [], read_by: [] },
      { id: "event-schedule", type: "schedule", note: "已安排初试，邀请候选人和三位面试官。", author_email: loginEmail, created_at: "2026-10-13T15:30:00+08:00", notify_emails: [], read_by: [] },
      { id: "event-advance", type: "advance", note: "初试通过，进入复试；重点确认管理经验和产品规划。", author_email: loginEmail, created_at: "2026-10-15T09:20:00+08:00", notify_emails: [], read_by: [] },
    ], permissions: { can_manage: true, can_assign: true, can_view_resume: true, can_advance: true, can_schedule: true, can_feedback: true, can_approve: true },
  }];
  const templates = [{ id: "mail-first", name: "面试邀请", category: "interview_first", subject: "{公司名称} · {岗位名称}面试邀请", body: "{简历姓名}，你好：\n邀请你参加{岗位名称}面试。\n面试时间：{面试时间}\n联系人：{招聘联系人}", is_builtin: true }];
  return { user: { id: "preview-user", email: loginEmail, name: "周禾", role: "super_admin", tenant_id: "synthetic-team", password_set: true }, candidates, positions, folders, notes, recommendations, applications, workflows: [{ id: "flow-default", name: "标准招聘流程", version: 1, revision: 1, is_default: true, nodes }], templates, deliveries: [], calls: [], company: { company_name: "合成星野科技", contact_name: "周禾", contact_email: loginEmail, contact_phone: "13800000000", company_address: "杭州 · 合成地址，仅作本地界面演示" } };
}

/** readBody 只解析有限大小的本地 JSON 请求，拒绝无效正文。 */
async function readBody(request) {
  let body = "";
  for await (const chunk of request) {
    body += chunk;
    if (body.length > 4 * 1024 * 1024) throw new Error("合成预览请求正文过大");
  }
  return body ? JSON.parse(body) : {};
}

/** candidateDetail 合并当前候选人的独立备注和推荐，不混入其他候选人的资料。 */
function candidateDetail(state, candidate) {
  if (!candidate) return null;
  const notes = state.notes.filter((note) => (note.candidate_id || "candidate-linxiao") === candidate.id);
  const recommendations = state.recommendations.filter((item) => item.candidate_id === candidate.id);
  return { ...candidate, notes, latest_note: notes[0], recommendations, recommendation_count: recommendations.length, folder_details: state.folders.filter((folder) => candidate.folder_ids?.includes(folder.id)) };
}

/** applyAction 仅更新进程内的合成招聘状态，安排面试或发送通知都不会访问外部服务。 */
function applyAction(state, application, body) {
  const now = new Date().toISOString();
  const node = application.nodes[application.current_node];
  const interview = application.interviews.find((item) => item.id === body.interview_id);
  if (body.action === "schedule") application.interviews.unshift({ ...body, id: `interview-${Date.now()}`, node_id: node.id, attempt: node.attempt, status: "scheduled", confirmation: "pending", feedback: [], revision: 1 });
  if (body.action === "feedback" && interview) interview.feedback.push({ author_email: loginEmail, conclusion: body.conclusion, content: body.content, created_at: now });
  if (body.action === "cancel_interview" && interview) interview.status = "cancelled";
  if (body.action === "reschedule" && interview) Object.assign(interview, body, { revision: interview.revision + 1 });
  if (body.action === "advance") {
    if (node) node.status = "completed";
    application.current_node = Math.min(application.current_node + 1, application.nodes.length - 1);
    application.nodes[application.current_node].status = "active";
    application.stage = application.nodes[application.current_node].name;
  }
  if (body.action === "return") application.current_node = Math.max(0, Number(body.target_node ?? application.current_node - 1));
  if (body.action === "end") application.status = body.status || body.conclusion || "rejected";
  if (body.action === "assign") Object.assign(node || application, body);
  if (body.action === "approve" && node) node.approvals.push({ author_email: loginEmail, decision: body.decision, content: body.content });
  application.revision += 1;
  application.events.push({ id: `event-${Date.now()}`, type: body.action, note: body.content || body.note || "合成预览操作已记录。", author_email: loginEmail, created_at: now, notify_emails: [], read_by: [] });
  return { application, candidate: candidateDetail(state, state.candidates.find((item) => item.id === application.candidate_id)) };
}

/** routePreview 复用现有 UI 回归的数据合同处理请求；未覆盖接口明确返回 404，防止误连真实 API。 */
function routePreview(state, url, method, body) {
  const path = url.pathname;
  const query = url.searchParams;
  if (path === "/health") return { ok: true, version: "6.2.10.1", synthetic: true };
  if (path === "/__preview/status") return { synthetic: true, candidates: state.candidates.length, applications: state.applications.length, calls: state.calls.slice(-50) };
  if (path === "/api/auth/agreement-status") return { agreement_accepted: true };
  if (path === "/api/auth/send-code") return { is_new_account: false, debug_code: "1234" };
  if (path === "/api/auth/password-login" || path === "/api/auth/login") {
    if (body.email !== loginEmail || (path.endsWith("password-login") ? body.password !== loginPassword : body.code !== "1234")) return { status: 401, error: "请使用合成预览账号 preview@example.invalid。" };
    return { access_token: "candidate-detail-synthetic-only", requires_password_setup: false, user: state.user };
  }
  if (path === "/api/auth/me") return { user: state.user };
  if (path === "/api/subscription/status") return { subscription: { active: true, member_type: "plus", member_name: "Plus", remaining_days: 30, allow_ai: true, allow_auto_reply: true } };
  if (path === "/api/config/notification-profile") return { profile: { completed: true } };
  if (path === "/api/config/user-ai") return { config: { base_url: "http://127.0.0.1/synthetic-ai", model: "synthetic-preview", api_key: "", api_key_set: false, enabled: false } };
  if (path === "/api/config/user-preferences") return { config: {} };
  if (["/api/runtime/config", "/api/system/app-config"].includes(path)) return { config: { announcements_enabled: false, announcements: [], ads_enabled: false } };
  if (path === "/api/user-flow") return { flow: { version: 2, state: "completed", stage: "completed", steps: {} } };
  if (path === "/api/wechat-official/status") return { status: { following: true, bound: true, reward_claimed: true } };
  if (path.includes("/invitations")) return { invitations: [] };
  if (path === "/api/ai-wallet") return { wallet: { balance: "100.00", models: [], default_model: "synthetic" } };
  if (path === "/api/messages/summary") return { unread_count: 0, version: 1 };
  if (path === "/api/messages") return { messages: [], total: 0 };
  if (path === "/api/system/default-prompts") return { prompts: {} };
  if (path === "/api/resume-library/settings") return { settings: { auto_import_downloads: true } };
  if (path === "/api/platforms/config/") return { platforms: ["boss", "liepin", "zhaopin", "51job"].map((id) => ({ id, platform_id: id, is_open: true, open: true, enabled: true })) };
  if (path === "/api/v1/runtime/status") return { version: "6.2.10.1", node_installed: true, camoufox_installed: true, system_runtime: { ready: true, required: false } };
  if (path === "/api/v1/app-update/status") return { stage: "idle", current_version: "6.2.10.1" };
  if (["/api/v1/session/bind", "/api/v1/bind", "/api/v1/auth/bind", "/api/v1/config", "/api/v1/cloud/config", "/api/v1/auth/cloud-session"].includes(path)) return { bound: true, email: loginEmail };
  if (path === "/api/positions") {
    const purpose = query.get("purpose");
    const scope = purpose === "recommendation" ? "true" : query.get("is_open") || "true";
    const positions = state.positions.filter((position) => scope === "all" || (scope === "false" ? !position.is_open : position.is_open));
    return { positions, total: positions.length };
  }
  if (path === "/api/candidates/folders") return { folders: state.folders };
  if (path === "/api/candidates/duplicates") return { matches: [], hidden_match: false };
  if (path === "/api/candidates") {
    const keyword = query.get("q") || query.get("keyword") || "";
    const candidates = state.candidates.filter((candidate) => `${candidate.candidate_name} ${candidate.phone}`.includes(keyword)).map((candidate) => candidateDetail(state, candidate));
    return { candidates, total: candidates.length, real_total: candidates.length, builtin_total: 0, page: 1 };
  }
  if (path === "/api/candidate-recommendations") {
    const items = state.recommendations.map((recommendation) => {
      const candidate = state.candidates.find((item) => item.id === recommendation.candidate_id);
      const position = state.positions.find((item) => item.id === recommendation.position_id);
      return { ...recommendation, candidate_name: candidate?.candidate_name, phone: candidate?.phone, position_is_open: position?.is_open, can_edit: Boolean(position?.is_open), can_delete: true };
    }).filter((item) => (!query.get("stage") || item.stage === query.get("stage")) && `${item.candidate_name} ${item.phone} ${item.position_name}`.includes(query.get("q") || ""));
    return { items, total: items.length };
  }
  if (path.startsWith("/api/candidate-recommendations/") && method === "DELETE") {
    state.recommendations = state.recommendations.filter((item) => item.id !== path.split("/").at(-1));
    return {};
  }
  const candidateMatch = /^\/api\/candidates\/([^/]+)(?:\/(notes|recommendations))?$/.exec(path);
  if (candidateMatch) {
    const candidate = state.candidates.find((item) => item.id === candidateMatch[1]);
    if (!candidate) return { status: 404, error: "合成候选人不存在。" };
    const section = candidateMatch[2];
    if (section === "notes") {
      if (method !== "POST") return { notes: candidateDetail(state, candidate).notes };
      const note = { id: `note-${Date.now()}`, candidate_id: candidate.id, content: body.content, author_name: "周禾", author_email: loginEmail, occurred_at: new Date().toISOString(), created_at: new Date().toISOString() };
      state.notes.unshift(note);
      return { note };
    }
    if (section === "recommendations") {
      if (method !== "POST") return { recommendations: state.recommendations.filter((item) => item.candidate_id === candidate.id) };
      let recommendation = state.recommendations.find((item) => item.candidate_id === candidate.id && item.position_id === body.position_id);
      if (!recommendation) {
        recommendation = { id: `rec-${Date.now()}`, candidate_id: candidate.id, position_id: body.position_id, position_name: state.positions.find((item) => item.id === body.position_id)?.name || "合成岗位", stage: "draft", events: [], created_at: new Date().toISOString() };
        state.recommendations.push(recommendation);
      }
      recommendation.events.push({ id: `rec-event-${Date.now()}`, from_stage: recommendation.stage, to_stage: body.stage, content: body.content, author_name: "周禾", author_email: loginEmail, occurred_at: new Date().toISOString(), created_at: new Date().toISOString() });
      recommendation.stage = body.stage;
      recommendation.updated_at = new Date().toISOString();
      return { recommendation };
    }
    if (method === "PATCH") Object.assign(candidate, body, { row_version: candidate.row_version + 1 });
    return { candidate: candidateDetail(state, candidate) };
  }
  if (path === "/api/recruitment/workflows") return { workflows: state.workflows, can_manage: true, members: [{ email: loginEmail, name: "周禾", status: "active" }, { email: "reviewer@example.invalid", name: "苏宁", status: "active" }, { email: "manager@example.invalid", name: "陈嘉", status: "active" }, { email: "cc@example.invalid", name: "唐悦", status: "active" }] };
  if (path === "/api/recruitment/company") return { company: state.company, can_manage: true };
  if (path === "/api/recruitment/mail-templates") return { templates: state.templates, can_manage: true };
  if (path === "/api/recruitment/placeholders") return { placeholders: ["公司名称", "岗位名称", "简历姓名", "面试时间", "招聘联系人", "联系邮箱"].map((key) => ({ key, label: key, example: `合成${key}`, group: "基础" })) };
  if (path === "/api/recruitment/mail-history") return { deliveries: state.deliveries };
  if (path === "/api/recruitment/mail-preview") return { preview: { subject: "合成星野科技 · 高级产品经理面试邀请", body: "林晓，你好：\n面试时间：2026-10-16 14:00\n面试地点：合成会议室\n联系人：周禾\n此内容仅为本地预览，不会发出邮件。", recipient: "linxiao@example.invalid", reply_to: loginEmail, missing_placeholders: [], unknown_placeholders: [], can_send: true, is_example: !body.application_id, context_fingerprint: "synthetic-preview-v1" } };
  if (path === "/api/recruitment/mail-send") {
    const delivery = { ...body, id: `delivery-${Date.now()}`, recipient: "linxiao@example.invalid", reply_to: loginEmail, status: "sent", created_at: new Date().toISOString(), synthetic: true };
    state.deliveries.push(delivery);
    return { delivery };
  }
  if (path === "/api/recruitment/applications") return { applications: state.applications.filter((item) => (!query.get("candidate_id") || item.candidate_id === query.get("candidate_id")) && (!query.get("position_id") || item.position_id === query.get("position_id"))) };
  const applicationMatch = /^\/api\/recruitment\/applications\/([^/]+)(\/actions)?$/.exec(path);
  if (applicationMatch) {
    const application = state.applications.find((item) => item.id === applicationMatch[1]);
    if (!application) return { status: 404, error: "合成招聘流程不存在。" };
    if (applicationMatch[2] && method === "POST") return applyAction(state, application, body);
    return { application, candidate: candidateDetail(state, state.candidates.find((item) => item.id === application.candidate_id)) };
  }
  return { status: 404, error: `本地合成预览尚未覆盖：${method} ${path}` };
}

/** createPreviewServer 创建不含任何外部网络调用的 HTTP 服务，供本机浏览器和 UI 回归使用。 */
export function createPreviewServer(state = createPreviewState()) {
  return createServer(async (request, response) => {
    const headers = { "Content-Type": "application/json; charset=utf-8", "Access-Control-Allow-Origin": "*", "Access-Control-Allow-Headers": "Content-Type, Authorization, X-Requested-With", "Access-Control-Allow-Methods": "GET, POST, PUT, PATCH, DELETE, OPTIONS", "Access-Control-Allow-Private-Network": "true", "Cache-Control": "no-store" };
    if (request.method === "OPTIONS") { response.writeHead(204, headers); response.end(); return; }
    try {
      const url = new URL(request.url || "/", "http://127.0.0.1");
      const body = await readBody(request);
      state.calls.push({ method: request.method, path: url.pathname, query: url.search });
      const result = routePreview(state, url, request.method || "GET", body);
      const { status = 200, ...data } = result;
      if (status === 404) process.stderr.write(`${data.error}\n`);
      response.writeHead(status, headers);
      response.end(JSON.stringify({ ok: status < 400, ...data }));
    } catch (error) {
      response.writeHead(400, headers);
      response.end(JSON.stringify({ ok: false, error: error instanceof Error ? error.message : "合成预览请求没有处理成功。" }));
    }
  });
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const port = Number(process.env.CANDIDATE_PREVIEW_PORT || 18164);
  if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new Error("CANDIDATE_PREVIEW_PORT 需要是 1024–65535 之间的端口。");
  const server = createPreviewServer();
  server.listen(port, "127.0.0.1", () => {
    process.stdout.write(`合成候选人预览 API：http://127.0.0.1:${port}\n账号：${loginEmail} / ${loginPassword}；验证码：1234\n所有业务数据仅保存在内存，邮件操作不会发送邮件。\n`);
  });
}
