/* 文件作用：提供插件共享白名单、字段校验及幂等保存逻辑；不访问招聘页面。 */
(function (root) {
  "use strict";
  const CLOUD_BASE = "https://goodhr.58it.cn";
  const PLATFORM_HOSTS = ["lpt.liepin.com", "h.liepin.com", "rd6.zhaopin.com", "ehire.51job.com"];
  const STAGES = { draft: "已关联", recommended: "已推荐", contacting: "沟通中", interview: "面试中", offer: "Offer", hired: "已入职", paused: "暂停" };

  /** cleanText 将不可信文本限制长度并去除空白首尾。 */
  function cleanText(value, limit = 2000) { return String(value ?? "").trim().slice(0, limit); }

  /** sanitizeFolderIDs 仅保留已选择收藏夹的 UUID，去重并限制数量，不从原技能标签生成分类。 */
  function sanitizeFolderIDs(value) { return [...new Set((Array.isArray(value) ? value : []).filter((id) => typeof id === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)).map((id) => id.toLowerCase()))].slice(0, 30); }

  /** normalizePhone 校验完整大陆号或明确国际号，拒绝掩码与混入业务编号。 */
  function normalizePhone(value) {
    let text = cleanText(value, 80).replace(/[\s()-]/g, "");
    if (/^(?:\+86|0086)/.test(text)) return /^1[3-9]\d{9}$/.test(text.replace(/^(?:\+86|0086)/, "")) ? text.replace(/^(?:\+86|0086)/, "") : "";
    return /^1[3-9]\d{9}$/.test(text) || /^\+[1-9]\d{6,14}$/.test(text) ? text : "";
  }

  /** platformForURL 按 HTTPS 精确域名判断采集平台，拒绝相似域名。 */
  function platformForURL(value) {
    try {
      const url = new URL(value);
      if (url.protocol !== "https:" || url.port || !PLATFORM_HOSTS.includes(url.hostname)) return "";
      return { "lpt.liepin.com": "liepin", "h.liepin.com": "hliepin", "rd6.zhaopin.com": "zhaopin", "ehire.51job.com": "51job" }[url.hostname];
    } catch { return ""; }
  }

  /** safeSourceURL 去掉查询与锚点，防止把页面临时令牌随简历上传。 */
  function safeSourceURL(value) {
    if (!platformForURL(value)) return "";
    const url = new URL(value); return `${url.origin}${url.pathname}`;
  }

  /** safeAvatarSource 仅保留候选人头像的有限 HTTPS 图片域或小型内嵌图片，不读取隐藏状态。 */
  function safeAvatarSource(value, pageURL) {
    const platform = platformForURL(pageURL); if (!platform || typeof value !== "string" || !value.trim()) return "";
    if (/^data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/=]+$/.test(value)) return value.length <= 1400000 ? value : "";
    if (value.length > 4096) return "";
    try {
      const url = new URL(value, pageURL), page = new URL(pageURL), hostname = url.hostname.toLowerCase();
      const liepinCDN = ["liepin", "hliepin"].includes(platform) && (hostname === "lietou-static.com" || hostname.endsWith(".lietou-static.com"));
      if (url.protocol !== "https:" || url.port || url.username || url.password || !(hostname === page.hostname || liepinCDN || (platform === "zhaopin" && hostname === "mypics.zhaopin.cn"))) return "";
      return url.href;
    } catch { return ""; }
  }

  /** validStoredAvatar 判断已规范化的内嵌头像，不允许 UI 加载任何外部图片地址。 */
  function validStoredAvatar(value) { if (typeof value !== "string" || value.length > 131072) return false; const match = value.match(/^data:image\/(?:png|jpeg);base64,([A-Za-z0-9+/]*={0,2})$/); return Boolean(match?.[1] && match[1].length % 4 === 0 && /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(match[1])); }

  /** newKey 为每次用户明确发起的保存生成独立幂等键。 */
  function newKey() { return root.crypto.randomUUID(); }

  /** resumeIdentity 使用平台编号或明确姓名线索隔离草稿，人工补手机号不会切换候选人。 */
  function resumeIdentity(resume) {
    if (!resume?.candidate_name) return "";
    return JSON.stringify([resume.platform_id || "", resume.platform_candidate_id || "", resume.source_url || "", resume.candidate_name, ...(resume.platform_candidate_id ? [] : [resume.birth_ym || "", resume.educations?.[0]?.school_name || ""])]);
  }

  /** duplicateInput 只向查重接口提交手机号与结构化身份依据，不上传头像或全文。 */
  function duplicateInput(input, keyword = "", excludeID = "") {
    const resume = sanitizeResume(input);
    return { phone: resume.phone, candidate_name: resume.candidate_name, platform_id: resume.platform_id, platform_candidate_id: resume.platform_candidate_id, gender: resume.gender, age: resume.age, birth_ym: resume.birth_ym, work_experiences: resume.work_experiences.map(({ company_name, start_ym }) => ({ company_name, start_ym })), educations: resume.educations.map(({ school_name, start_ym }) => ({ school_name, start_ym })), keyword: cleanText(keyword, 200), exclude_id: cleanText(excludeID, 100) };
  }

  /** classifyDuplicates 仅唯一精确命中允许自动选择，相似和多条命中都留给用户确认。 */
  function classifyDuplicates(result, phone) {
    const matches = result?.matches || [], exact = matches.filter((item) => item.match_type === "exact" && item.candidate?.id);
    if (result?.hidden_match) return { state: "hidden", title: "有重复资料暂时无权查看", selected: null };
    if (matches.length === 1 && exact.length === 1) return { state: "existing", title: "已入库，可以更新或单独加备注", selected: exact[0].candidate };
    if (matches.length) return { state: "possible", title: exact.length ? "有多条精确线索，请确认对应简历" : "可能重复，请确认是不是同一个人", selected: null };
    return { state: phone ? "new" : "no-phone", title: phone ? "未入库，可以添加简历" : "没有找到已有资料，手机号还需要补一下", selected: null };
  }

  /** compareVersions 比较三段正式版本，开发新版本不会因线上旧配置被锁定。 */
  function compareVersions(left, right) { if (!/^\d+\.\d+\.\d+$/.test(left || "") || !/^\d+\.\d+\.\d+$/.test(right || "")) return 0; const a = left.split(".").map(Number), b = right.split(".").map(Number); for (let index = 0; index < 3; index++) if (a[index] !== b[index]) return a[index] > b[index] ? 1 : -1; return 0; }

  /** stripSummaryContacts 移除摘要请求的姓名联系方式及来源编号，保留履历内容。 */
  function stripSummaryContacts(input) { const resume = sanitizeResume(input); for (const key of ["candidate_name", "phone", "email", "avatar_data_url", "source_url", "platform_candidate_id", "folder_ids"]) delete resume[key]; return resume; }

  /** summaryAction 根据任务与职业资料判断恢复、等待、明确重试或生成新草稿。 */
  function summaryAction(task, unknown, original, current) { if (unknown) return "recover"; if (["queued", "running"].includes(task?.status)) return "wait"; const changed = original && JSON.stringify(stripSummaryContacts(original)) !== JSON.stringify(stripSummaryContacts(current)); if (["failed", "cancelled"].includes(task?.status)) return changed ? "changed" : "retry"; return task?.status === "completed" ? "new" : "start"; }

  /** sanitizeResume 只接受候选人资料白名单，拒绝运行态、Cookie 和页面未知数据。 */
  function sanitizeResume(input = {}) {
    const fields = ["candidate_name", "phone", "email", "birth_ym", "gender", "work_region", "current_region", "expected_region", "work_years", "education_level", "current_position", "expected_position", "current_salary_text", "expected_salary_text", "personal_description", "work_status", "online_status", "platform_id", "platform_candidate_id", "source_url", "basic_info"];
    const result = Object.fromEntries(fields.map((field) => [field, cleanText(input[field], field === "personal_description" ? 6000 : 500)]));
    result.phone = normalizePhone(input.phone);
    result.age = Number.isInteger(input.age) && input.age > 0 && input.age <= 120 ? input.age : null;
    result.source_url = safeSourceURL(input.source_url);
    result.source_type = "browser_extension";
    if (validStoredAvatar(input.avatar_data_url)) result.avatar_data_url = input.avatar_data_url;
    result.birth_ym = /^(?:19|20)\d{2}-(?:0[1-9]|1[0-2])$/.test(result.birth_ym) ? result.birth_ym : "";
    result.current_region = cleanText(input.current_region || input.work_region, 500);
    result.expected_region = cleanText(input.expected_region || input.expected_city, 500);
    const tags = Array.isArray(input.tags) ? input.tags : String(input.tags || "").split(/[,，;；\n]/);
    result.tags = [...new Set(tags.map((tag) => Array.from(cleanText(tag, 200)).slice(0, 50).join("")).filter(Boolean))].slice(0, 30);
    if (Object.hasOwn(input, "folder_ids")) result.folder_ids = sanitizeFolderIDs(input.folder_ids);
    for (const key of ["expected_salary_min", "expected_salary_max"]) if (typeof input[key] === "number" && Number.isFinite(input[key]) && input[key] >= 0 && input[key] <= 1000000) result[key] = input[key];
    if (result.expected_salary_min !== undefined && result.expected_salary_max !== undefined && result.expected_salary_min > result.expected_salary_max) { delete result.expected_salary_min; delete result.expected_salary_max; }
    const arrays = {
      work_experiences: ["company_name", "position_name", "content", "start_ym", "end_ym"],
      educations: ["school_name", "major_name", "education_level", "start_ym", "end_ym"],
      project_experiences: ["project_name", "role_name", "content", "start_ym", "end_ym"],
      certificates: ["certificate_name", "issued_by", "issued_ym"],
      honors: ["honor_name", "issued_by", "issued_ym", "description"]
    };
    for (const [field, keys] of Object.entries(arrays)) {
      result[field] = (Array.isArray(input[field]) ? input[field] : []).slice(0, 40).map((item) => Object.fromEntries(keys.map((key) => [key, cleanText(item?.[key], key === "content" ? 6000 : 500)])));
    }
    return result;
  }

  /** nonEmptyResumePatch 保留原技能标签的非空更新规则；显式收藏夹选择只清空可见分类，不清空采集资料。 */
  function nonEmptyResumePatch(input = {}) {
    const result = {};
    for (const [key, original] of Object.entries(sanitizeResume(input))) {
      if (key === "folder_ids") { result[key] = original; continue; }
      const value = Array.isArray(original) ? original.filter((item) => typeof item === "string" ? item.trim() : item && Object.values(item).some((field) => field !== null && field !== undefined && String(field).trim())) : original;
      if (value === null || value === undefined || (typeof value === "string" && !value.trim()) || (Array.isArray(value) && !value.length)) continue;
      result[key] = value;
    }
    return result;
  }

  /** candidateWriteResume 正常保存原技能标签；收藏夹只提交手动选中的已有编号，两类数据互不转换。 */
  function candidateWriteResume(input = {}, updating = false) { const result = updating ? nonEmptyResumePatch(input) : sanitizeResume(input); if (!updating && !Object.hasOwn(result, "folder_ids")) result.folder_ids = []; return result; }

  /** comparableValue 将字段顺序标准化，避免 JSON 对象排列和数字字符串产生假差异。 */
  function comparableValue(value) {
    if (Array.isArray(value)) return value.map(comparableValue);
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).sort(([left], [right]) => left.localeCompare(right)).map(([key, item]) => [key, comparableValue(item)]));
    return value === null || value === undefined ? "" : String(value);
  }

  /** readableFieldValue 用短中文值概括字段变化，不把程序字段名交给用户。 */
  function readableFieldValue(value) {
    if (validStoredAvatar(value)) return "已保存头像";
    if (Array.isArray(value)) {
      const names = value.slice(0, 3).map((item) => typeof item === "string" ? item : [item.company_name || item.school_name || item.project_name || item.certificate_name || item.honor_name, item.position_name || item.major_name || item.role_name].filter(Boolean).join(" / "));
      return `${value.length}项${names.filter(Boolean).length ? `（${cleanText(names.filter(Boolean).join("；"), 90)}${value.length > 3 ? "…" : ""}）` : ""}`;
    }
    const words = { browser_extension: "浏览器插件", manual: "手动添加", ai_import: "AI解析", liepin: "猎聘企业端", hliepin: "猎聘猎头端", zhaopin: "智联", "51job": "前程无忧", male: "男", female: "女" };
    return words[value] || cleanText(value, 90) || "未填写";
  }

  /** describeResumeChanges 比较非空字段，返回确认更新前可阅读的具体差异。 */
  function describeResumeChanges(existing = {}, input = {}) {
    const labels = { candidate_name: "姓名", phone: "手机号", email: "邮箱", birth_ym: "出生年月", gender: "性别", age: "年龄", work_region: "所在城市", current_region: "当前地区", expected_region: "期望地区", work_years: "工作年限", education_level: "学历", current_position: "当前职位", expected_position: "期望职位", current_salary_text: "当前薪资", expected_salary_text: "期望薪资", expected_salary_min: "期望最低薪资(K)", expected_salary_max: "期望最高薪资(K)", personal_description: "个人简介", work_status: "求职状态", online_status: "活跃情况", basic_info: "基本资料", source_type: "资料来源", platform_id: "招聘平台", platform_candidate_id: "平台简历编号", source_url: "来源页面", tags: "技能标签", work_experiences: "工作经历", educations: "教育经历", project_experiences: "项目经历", certificates: "证书", honors: "荣誉" };
    return Object.entries(nonEmptyResumePatch(input)).filter(([key, value]) => JSON.stringify(comparableValue(key === "folder_ids" ? sanitizeFolderIDs(existing[key]).sort() : existing[key])) !== JSON.stringify(comparableValue(key === "folder_ids" ? [...value].sort() : value))).map(([key, value]) => `${key === "avatar_data_url" ? "头像" : key === "folder_ids" ? "收藏夹" : labels[key] || "其他资料"}：${readableFieldValue(existing[key])} → ${readableFieldValue(value)}`);
  }

  /** saveFollowups 分别保存备注和岗位关联，保留失败项的幂等键供安全重试。 */
  async function saveFollowups(request, candidateID, draft) {
    const result = { notes_saved: !cleanText(draft.note), recommendation_saved: !draft.position_id, errors: [] };
    const note = String(draft.note ?? "").trim();
    if (note && Array.from(note).length > 1000) result.errors.push({ kind: "note", message: "备注超过1000字，原文保留了，请缩短或分次记录" });
    else if (note) {
      try {
        await request(`/api/candidates/${encodeURIComponent(candidateID)}/notes`, { content: note, idempotency_key: draft.note_key, ...(draft.note_time ? { occurred_at: draft.note_time } : {}) });
        result.notes_saved = true;
      } catch (error) { result.errors.push({ kind: "note", message: error.message || "备注没有保存成功" }); }
    }
    const recommendation = String(draft.recommendation_note ?? "").trim();
    if (draft.position_id && Array.from(recommendation).length > 1000) result.errors.push({ kind: "recommendation", message: "推荐备注超过1000字，原文保留了，请缩短或分次记录" });
    else if (draft.position_id) {
      try {
        if (draft.stage && !STAGES[draft.stage]) throw new Error("推荐状态没选好，请重新选择");
        await request(`/api/candidates/${encodeURIComponent(candidateID)}/recommendations`, { position_id: draft.position_id, stage: draft.stage || "draft", content: recommendation, idempotency_key: draft.recommendation_key, ...(draft.recommendation_time ? { occurred_at: draft.recommendation_time } : {}) });
        result.recommendation_saved = true;
      } catch (error) { result.errors.push({ kind: "recommendation", message: error.message || "岗位没有关联成功" }); }
    }
    return result;
  }

  const api = { CLOUD_BASE, PLATFORM_HOSTS, STAGES, cleanText, sanitizeFolderIDs, normalizePhone, platformForURL, safeSourceURL, safeAvatarSource, validStoredAvatar, newKey, resumeIdentity, duplicateInput, classifyDuplicates, compareVersions, stripSummaryContacts, summaryAction, sanitizeResume, nonEmptyResumePatch, candidateWriteResume, describeResumeChanges, saveFollowups };
  root.GoodHRShared = api;
  if (typeof module !== "undefined" && module.exports) module.exports = api;
})(globalThis);
