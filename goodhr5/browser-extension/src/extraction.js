/* 文件作用：按猎聘企业、猎聘猎头、智联及前程无忧适配器只读采集当前可见候选人；不修改 DOM。 */
(function (root) {
  "use strict";
  const shared = root.GoodHRShared || (typeof require === "function" ? require("./shared.js") : null);
  const ADAPTERS = {
    liepin: {
      roots: [".ant-lpt-modal", ".resume-detail", "[class*='resumeDetail--']"],
      name: [".nest-resume-personal-name", "[class*='personalName--']", "[class*='name--']"],
      facts: [".nest-resume-personal-info", "[class*='personalInfo--']", ".personal-detail-age", ".personal-detail-edulevel", ".personal-detail-location"],
      phone: ["a[href^='tel:']", "[class*='phone--']", ".phone", ".contact-phone", ".connect-box dd"],
      work: [".rd-work-item-cont", ".work-experience-item", "[class*='workExperienceItem--']"],
      education: [".rd-edu-info-item", ".education-item", "[class*='educationItem--']"],
      expected: [".expected-position", ".personal-expect-content", "[class*='intention--']"],
      description: [".resume-description", ".nest-resume-personal-skills", "[class*='selfEvaluation--']"]
    },
    hliepin: {
      roots: [".resume-detail-template", "#resume-detail", "body"],
      marker: "#resume-detail-basic-info",
      name: ["#resume-detail-basic-info .name"], facts: ["#resume-detail-basic-info .sep-info"],
      phone: ["#resume-detail-basic-info .connect-box dd"],
      work: ["#resume-detail-work-info .rd-work-item-cont"], education: ["#resume-detail-edu-info .rd-edu-info-item"],
      project: ["#resume-detail-project-info .rd-project-item-cont"],
      expected: ["#resume-detail-job-exp-info .tabs .title"],
      description: ["#resume-detail-self-eva-info .resume-detail-template-cont"]
    },
    "51job": {
      roots: [".con-container .resume"],
      name: [".baseinfo-container .name"], facts: [".baseinfo-container .center-top-item", ".baseinfo-container .detail-item"],
      phone: [".eh_take_phone_popper .eh_im_take_phone_wrap .action"],
      work: [".work-container-item"], education: [".school-container-item", ".edu-container-item"],
      project: [".project-container-item"],
      expected: [".intention-container-item .item-info-position"], description: [".advantage-container", ".IM-base-module .IM-info-desc"]
    },
    zhaopin: {
      roots: [".new-resume-detail--inner", ".resume-detail"],
      name: [".resume-basic-new__name"], facts: [".resume-basic-new__meta-item"],
      phone: [".resume-basic-new__contacts--phone"],
      work: [".new-work-experiences__item"], education: [".new-education-experiences__item"],
      project: [".new-project-experiences__item"],
      expected: [".new-resume-purposes__item-type"], description: [".resume-section-self-evaluation"]
    }
  };

  /** isVisible 检查节点及祖先是否隐藏，仅读取用户当前可见的资料。 */
  function isVisible(element) {
    if (!element || element.hidden || element.closest?.("[hidden],[aria-hidden='true']")) return false;
    if (typeof element.getClientRects === "function" && element.getClientRects().length === 0) return false;
    const view = element.ownerDocument?.defaultView;
    for (let node = element; node && view?.getComputedStyle; node = node.parentElement) {
      const style = view.getComputedStyle(node);
      if (style.display === "none" || style.visibility === "hidden" || style.visibility === "collapse" || style.opacity === "0") return false;
    }
    return true;
  }

  /** visibleNodes 返回选择器对应的可见节点，排除隐藏候选人缓存。 */
  function visibleNodes(scope, selector) { return Array.from(scope?.querySelectorAll(selector) || []).filter(isVisible); }

  /** readNode 读取可见节点的可见文字，浏览器不回退读取隐藏 textContent。 */
  function readNode(element, limit = 2000) { return isVisible(element) ? shared.cleanText(element.innerText, limit) : ""; }

  /** firstText 按平台选择器顺序提取第一个可见字段。 */
  function firstText(scope, selectors, limit = 2000) {
    for (const selector of selectors) { const value = readNode(visibleNodes(scope, selector)[0], limit); if (value) return value; }
    return "";
  }

  /** allRows 按选择器优先级获取一组经历，避免兼容选择器重复采集。 */
  function allRows(scope, selectors) {
    for (const selector of selectors || []) { const items = visibleNodes(scope, selector); if (items.length) return items.slice(0, 40); }
    return [];
  }

  /** parsePeriod 将页面年月转为 YYYY-MM，在职/在读结束时间保持空值。 */
  function parsePeriod(text) {
    const matched = String(text || "").match(/(\d{4})[.年\/-](\d{1,2})(?:月)?\s*[-—~至]\s*(?:(\d{4})[.年\/-](\d{1,2})|至今|现在)/);
    if (!matched) return { start_ym: "", end_ym: "" };
    if (Number(matched[2]) < 1 || Number(matched[2]) > 12 || (matched[4] && (Number(matched[4]) < 1 || Number(matched[4]) > 12))) return { start_ym: "", end_ym: "" };
    return { start_ym: `${matched[1]}-${matched[2].padStart(2, "0")}`, end_ym: matched[3] ? `${matched[3]}-${matched[4].padStart(2, "0")}` : "" };
  }

  /** readExperiences 只从当前详情的可见工作、教育节点读取结构化经历。 */
  function readExperiences(scope, adapter, kind) {
    return allRows(scope, adapter[kind]).map((item) => {
      const period = parsePeriod(firstText(item, [".rd-work-time", ".school-time", ".new-work-experiences__work-time", ".new-education-experiences__edu-time", ".time-detail", ".item-top-time", ".detail-time", ".item-time", ".IM-info-time", ".time", "[class*='time--']"]));
      if (kind === "work") return {
        company_name: firstText(item, [".rd-work-comp h5", ".new-work-experiences__work-name", ".info-company", ".company-name", "[class*='companyName--']"]),
        position_name: firstText(item, [".job-name", ".new-work-experiences__work-job-title", ".info-position", ".position-name", "[class*='positionName--']"]),
        content: readLabeledText(item, "职责业绩") || firstText(item, [".new-work-experiences__desc", ".item-bottom.IM-info-desc", ".work-content", "[class*='content--']"], 6000), ...period
      };
      const bottom = visibleNodes(item, ".new-education-experiences__item-bottom > span").map((child) => readNode(child));
      return { school_name: firstText(item, [".school-name", ".new-education-experiences__item-name", ".detail-name", ".IM-list-title", "[class*='schoolName--']"]), major_name: firstText(item, [".school-special", ".major-name", "[class*='major--']"]) || bottom.find((text) => !/博士|硕士|本科|大专|中专|高中|\d{4}/.test(text)) || "", education_level: firstText(item, [".school-degree", ".education-level"]) || (readNode(item).match(/博士|硕士|本科|大专|中专|高中/) || [])[0] || "", ...period };
    }).filter((item) => item.company_name || item.school_name || item.position_name);
  }

  /** findRoot 确定唯一可见简历详情；多位候选人同时可见时拒绝猜测。 */
  function findRoot(doc, adapter) {
    for (const selector of adapter.roots) {
      const candidates = visibleNodes(doc, selector).filter((item) => firstText(item, adapter.name) && (!adapter.marker || visibleNodes(item, adapter.marker).length));
      const roots = candidates.filter((outer) => !candidates.some((inner) => inner !== outer && outer.contains?.(inner)));
      if (roots.length === 1) return roots[0];
      if (roots.length > 1) return null;
    }
    return null;
  }

  /** extractPhone 只在联系方式区域查完整号码，不扫整个页面的业务编号。 */
  function extractPhone(scope, selectors) {
    for (const selector of selectors) for (const item of visibleNodes(scope, selector)) {
      const text = readNode(item).replace(/(?:手机号|手机|电话|联系方式)[：:]?/g, "").trim();
      const phone = shared.normalizePhone(text);
      if (phone) return phone;
    }
    return "";
  }

  /** readLabeledText 读取猎聘经历中带标签的正文，避免把公司规模误当职责。 */
  function readLabeledText(scope, label) {
    const row = visibleNodes(scope, ".rd-info-row").find((item) => firstText(item, [".rd-info-col-title"]).includes(label));
    return row ? firstText(row, [".rd-info-col-cont"], 6000) : "";
  }

  /** readGender 从可见文字或智联性别图标读取性别，不访问页面缓存。 */
  function readGender(scope, facts) {
    if (/(?:^|[\s/])女(?:[\s/]|$)/.test(facts)) return "female";
    if (/(?:^|[\s/])男(?:[\s/]|$)/.test(facts)) return "male";
    const image = visibleNodes(scope, ".resume-basic-new__gender img")[0];
    const icon = String(image?.getAttribute?.("src") || "").toLowerCase();
    return icon.includes("female") || icon.includes("woman") ? "female" : icon.includes("male") || icon.includes("man") ? "male" : "";
  }

  /** readProjects 读取当前详情可见项目节点，保持平台职责与页面操作分离。 */
  function readProjects(scope, adapter) {
    return allRows(scope, adapter.project).map((item) => ({
      project_name: firstText(item, [".rd-project-name h5", ".new-project-experiences__item-name", ".item-top-left", ".IM-list-title"]),
      role_name: readLabeledText(item, "项目职务") || firstText(item, [".new-project-experiences__item-role", ".item-center"]),
      content: readLabeledText(item, "项目描述") || firstText(item, [".new-project-experiences__item-desc", ".item-bottom.IM-info-desc"], 6000),
      ...parsePeriod(firstText(item, [".rd-project-time", ".new-project-experiences__item-time", ".item-top-right", ".IM-info-time"]))
    })).filter((item) => item.project_name || item.content);
  }

  /** readWorkYears 只提取页面明确写出的工作年限，兼容“工作7年”和“7年经验”。 */
  function readWorkYears(facts) { return (facts.match(/(?:工作|工作经验)\s*(\d+(?:\.\d+)?)\s*年/) || facts.match(/(\d+(?:\.\d+)?)\s*年(?:工作|经验|工作经验)/) || [])[1] || ""; }

  /** parseExpectedText 分开期望区域明确显示的职位和 K 薪资，保留薪资原格式。 */
  function parseExpectedText(text) {
    const salary = String(text || "").match(/\d+(?:\.\d+)?\s*(?:[-~—]\s*\d+(?:\.\d+)?)?\s*[kK](?:\s*[/／]\s*(?:月|年))?(?:\s*[×xX*]\s*\d+\s*薪)?/);
    return salary ? { position: shared.cleanText(text.slice(0, salary.index).replace(/[·|：:\s]+$/, ""), 500), salary: salary[0] } : { position: text, salary: "" };
  }

  /** extractCurrentCandidate 采集一个当前可见候选人，白名单外和未打开详情返回空值。 */
  function extractCurrentCandidate(doc, href) {
    const platform = shared.platformForURL(href), adapter = ADAPTERS[platform];
    if (!adapter || (platform === "hliepin" && !new URL(href).pathname.startsWith("/resume/"))) return null;
    const url = new URL(href);
    let scope = findRoot(doc, adapter);
    if (!scope && platform === "liepin" && url.pathname === "/resume/detail" && visibleNodes(doc, ".nest-resume-personal-name").length === 1) scope = visibleNodes(doc, "body")[0] || null;
    if (!scope) return null;
    const name = firstText(scope, adapter.name, 100); if (!name) return null;
    const facts = adapter.facts.map((selector) => visibleNodes(scope, selector).map((item) => readNode(item, 500)).join(" / ")).filter(Boolean).join(" / ").slice(0, 1500);
    const works = readExperiences(scope, adapter, "work"), educations = readExperiences(scope, adapter, "education");
    if (!educations.length && platform === "liepin") {
      const school = firstText(scope, [".resume-university"]);
      if (school) { const parts = school.split(/[·|]/).map((item) => item.trim()).filter(Boolean); educations.push({ school_name: parts[0], major_name: parts[1] && !/博士|硕士|本科|大专|中专|高中|统招/.test(parts[1]) ? parts[1] : "", education_level: parts.find((item) => /^(?:博士|硕士|本科|大专|中专|高中)$/.test(item)) || "", start_ym: "", end_ym: "" }); }
    }
    const age = Number((facts.match(/(?:^|\D)(\d{1,2})\s*岁/) || [])[1]) || null;
    const email = firstText(scope, [".resume-basic-new__email", ".email"]).match(/[\w.+-]+@[\w.-]+\.[a-z]{2,}/i)?.[0] || visibleNodes(scope, ".connect-box dd").map((item) => readNode(item)).join(" ").match(/[\w.+-]+@[\w.-]+\.[a-z]{2,}/i)?.[0] || "";
    const expected = parseExpectedText(firstText(scope, adapter.expected));
    return shared.sanitizeResume({
      candidate_name: name, phone: extractPhone(scope, adapter.phone) || (platform === "51job" ? extractPhone(doc, adapter.phone) : ""), email, age,
      gender: readGender(scope, facts),
      birth_ym: (facts.match(/(\d{4})[年./-](\d{1,2})月?/) || []).slice(1, 3).map((part) => part.padStart(2, "0")).join("-"),
      work_years: readWorkYears(facts),
      work_region: firstText(scope, [".personal-detail-location", ".location", "[class*='location--']"]) || (facts.match(/现居[：:]?\s*([^/|\s]+)/) || [])[1] || "",
      education_level: (facts.match(/博士|硕士|本科|大专|中专|高中/) || [])[0] || educations[0]?.education_level || "",
      expected_position: expected.position, current_position: works[0]?.position_name || "",
      expected_region: firstText(scope, ["#resume-detail-job-exp-info .dqname-wrap", ".new-resume-purposes__item-city", ".item-info-city"]),
      expected_salary_text: firstText(scope, ["#resume-detail-job-exp-info .salary", ".new-resume-purposes__item-salary", ".item-info-salary", ".expected-salary"]) || expected.salary,
      current_salary_text: firstText(scope, [".current-salary", ".personal-detail-salary"]),
      personal_description: firstText(scope, adapter.description, 6000), basic_info: facts,
      work_status: firstText(scope, [".user-status-tag", ".work-status", ".workStatus"]) || (facts.match(/在职[，,、\s]*(?:急寻新工作|正在找工作|考虑机会|暂不找工作)|离职[，,、\s]*(?:随时到岗|正在找工作)/) || [])[0] || "",
      online_status: firstText(scope, [".global-active-tag", ".avtive-time", ".online-status"]),
      work_experiences: works, educations, project_experiences: readProjects(scope, adapter),
      certificates: visibleNodes(scope, ".new-resume-certificates__item-name").slice(0, 40).map((item) => ({ certificate_name: readNode(item, 200) })),
      tags: ["#resume-detail-skill-info .skill-tag", ".new-resume-detail-skill-tag", ".new-professional-skills__item-name"].flatMap((selector) => visibleNodes(scope, selector).map((item) => readNode(item, 100))).filter(Boolean),
      platform_id: platform, source_url: href,
      platform_candidate_id: shared.cleanText(url.searchParams.get("resIdEncode") || url.searchParams.get("res_id_encode") || url.searchParams.get("resumeId") || url.searchParams.get("resume_id") || "", 120)
    });
  }

  const api = { ADAPTERS, isVisible, readNode, parsePeriod, readWorkYears, parseExpectedText, extractPhone, extractCurrentCandidate };
  root.GoodHRExtraction = api; if (typeof module !== "undefined" && module.exports) module.exports = api;
})(globalThis);
