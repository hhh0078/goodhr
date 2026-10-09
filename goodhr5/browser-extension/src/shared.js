/* 文件作用：提供插件共享白名单、字段校验及幂等保存逻辑；不访问招聘页面。 */
(function (root) {
  "use strict";
  const CLOUD_BASE = "https://goodhr.58it.cn";
  const PLATFORM_HOSTS = ["lpt.liepin.com", "h.liepin.com", "rd6.zhaopin.com", "ehire.51job.com"];
  const STAGES = { draft: "已关联", recommended: "已推荐", contacting: "沟通中", interview: "面试中", offer: "Offer", hired: "已入职", paused: "暂停" };

  /** cleanText 将不可信文本限制长度并去除空白首尾。 */
  function cleanText(value, limit = 2000) { return String(value ?? "").trim().slice(0, limit); }

  /** normalizePhone 校验完整大陆手机号，兼容 +86 和空格，拒绝掩码与混入编号。 */
  function normalizePhone(value) {
    let text = cleanText(value, 80).replace(/[\s()-]/g, "");
    if (/^\+?86/.test(text) && text.length > 11) text = text.replace(/^\+?86/, "");
    return /^1[3-9]\d{9}$/.test(text) ? text : "";
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

  /** newKey 为每次用户明确发起的保存生成独立幂等键。 */
  function newKey() { return root.crypto.randomUUID(); }

  /** sanitizeResume 只接受候选人资料白名单，拒绝运行态、Cookie 和页面未知数据。 */
  function sanitizeResume(input = {}) {
    const fields = ["candidate_name", "phone", "email", "birth_ym", "gender", "work_region", "current_region", "expected_region", "work_years", "education_level", "current_position", "expected_position", "current_salary_text", "expected_salary_text", "personal_description", "work_status", "online_status", "platform_id", "platform_candidate_id", "source_url", "basic_info"];
    const result = Object.fromEntries(fields.map((field) => [field, cleanText(input[field], field === "personal_description" ? 6000 : 500)]));
    result.phone = normalizePhone(input.phone);
    result.age = Number.isInteger(input.age) && input.age > 0 && input.age <= 120 ? input.age : null;
    result.source_url = safeSourceURL(input.source_url);
    result.source_type = "browser_extension";
    result.birth_ym = /^(?:19|20)\d{2}-(?:0[1-9]|1[0-2])$/.test(result.birth_ym) ? result.birth_ym : "";
    result.current_region = cleanText(input.current_region || input.work_region, 500);
    result.expected_region = cleanText(input.expected_region || input.expected_city, 500);
    const tags = Array.isArray(input.tags) ? input.tags : String(input.tags || "").split(/[,，;；\n]/);
    result.tags = [...new Set(tags.map((tag) => Array.from(cleanText(tag, 200)).slice(0, 50).join("")).filter(Boolean))].slice(0, 30);
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

  /** nonEmptyResumePatch 只提交非空采集字段，插件更新不能清空主库已有资料。 */
  function nonEmptyResumePatch(input = {}) {
    const result = {};
    for (const [key, original] of Object.entries(sanitizeResume(input))) {
      const value = Array.isArray(original) ? original.filter((item) => typeof item === "string" ? item.trim() : item && Object.values(item).some((field) => field !== null && field !== undefined && String(field).trim())) : original;
      if (value === null || value === undefined || (typeof value === "string" && !value.trim()) || (Array.isArray(value) && !value.length)) continue;
      result[key] = value;
    }
    return result;
  }

  /** comparableValue 将字段顺序标准化，避免 JSON 对象排列和数字字符串产生假差异。 */
  function comparableValue(value) {
    if (Array.isArray(value)) return value.map(comparableValue);
    if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).sort(([left], [right]) => left.localeCompare(right)).map(([key, item]) => [key, comparableValue(item)]));
    return value === null || value === undefined ? "" : String(value);
  }

  /** readableFieldValue 用短中文值概括字段变化，不把程序字段名交给用户。 */
  function readableFieldValue(value) {
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
    return Object.entries(nonEmptyResumePatch(input)).filter(([key, value]) => JSON.stringify(comparableValue(existing[key])) !== JSON.stringify(comparableValue(value))).map(([key, value]) => `${labels[key] || "其他资料"}：${readableFieldValue(existing[key])} → ${readableFieldValue(value)}`);
  }

  /** saveFollowups 分别保存备注和岗位关联，保留失败项的幂等键供安全重试。 */
  async function saveFollowups(request, candidateID, draft) {
    const result = { notes_saved: !cleanText(draft.note), recommendation_saved: !draft.position_id, errors: [] };
    if (cleanText(draft.note)) {
      try {
        await request(`/api/candidates/${encodeURIComponent(candidateID)}/notes`, { content: cleanText(draft.note, 6000), idempotency_key: draft.note_key });
        result.notes_saved = true;
      } catch (error) { result.errors.push({ kind: "note", message: error.message || "备注没有保存成功" }); }
    }
    if (draft.position_id) {
      try {
        await request(`/api/candidates/${encodeURIComponent(candidateID)}/recommendations`, { position_id: draft.position_id, stage: "draft", idempotency_key: draft.recommendation_key });
        result.recommendation_saved = true;
      } catch (error) { result.errors.push({ kind: "recommendation", message: error.message || "岗位没有关联成功" }); }
    }
    return result;
  }

  const api = { CLOUD_BASE, PLATFORM_HOSTS, STAGES, cleanText, normalizePhone, platformForURL, safeSourceURL, newKey, sanitizeResume, nonEmptyResumePatch, describeResumeChanges, saveFollowups };
  root.GoodHRShared = api;
  if (typeof module !== "undefined" && module.exports) module.exports = api;
})(globalThis);
