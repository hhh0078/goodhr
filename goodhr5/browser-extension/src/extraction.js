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
      avatar: [".nest-resume-personal-avatar img", "img.nest-resume-personal-avatar", ".nest-resume-personal-photo img", ".nest-resume-personal img.c-special-face"],
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
      avatar: ["#resume-detail-basic-info img.c-special-face"],
      work: ["#resume-detail-work-info .rd-work-item-cont"], education: ["#resume-detail-edu-info .rd-edu-info-item"],
      project: ["#resume-detail-project-info .rd-project-item-cont"],
      expected: ["#resume-detail-job-exp-info .tabs .title"],
      description: ["#resume-detail-self-eva-info .resume-detail-template-cont"]
    },
    "51job": {
      roots: [".con-container .resume"],
      name: [".baseinfo-container .name"], facts: [".baseinfo-container .center-top-item", ".baseinfo-container .detail-item"],
      phone: [".eh_take_phone_popper .eh_im_take_phone_wrap .action"],
      avatar: [".baseinfo-container .el-avatar img"],
      work: [".work-container-item"], education: [".school-container-item, .edu-container-item"],
      project: [".project-container-item"],
      expected: [".intention-container-item .item-info-position"], description: [".advantage-container", ".IM-base-module .IM-info-desc"]
    },
    zhaopin: {
      roots: [".new-resume-detail--inner", ".resume-detail"],
      name: [".resume-basic-new__name"], facts: [".resume-basic-new__meta-item"],
      phone: [".resume-basic-new__contacts--phone"],
      avatar: [".resume-basic-new__portrait img"],
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

  /** firstField 读取可见字段的完整标题或文字，保留平台省略显示的职位、学校和公司名称。 */
  function firstField(scope, selectors, limit = 2000) {
    for (const selector of selectors) {
      const element = visibleNodes(scope, selector)[0];
      const value = element && shared.cleanText(element.getAttribute?.("title") || readNode(element, limit), limit);
      if (value) return value;
    }
    return "";
  }

  /** uniqueTexts 汇总同一简历的多个可见期望或标签，去重后保留页面顺序。 */
  function uniqueTexts(values) { return [...new Set(values.map((value) => shared.cleanText(value, 500)).filter(Boolean))]; }

  /** splitFacts 拆分基础资料中的可见分隔文本，不读取隐藏缓存或页面内部对象。 */
  function splitFacts(value) { return String(value || "").split(/[|｜·/\n]+|\s{2,}/).map((item) => item.trim()).filter(Boolean); }

  /** readSeparatedFacts 读取猎聘分隔图标之间的可见字段，忽略隐藏子节点并避免文字粘连。 */
  function readSeparatedFacts(element) {
    if (!isVisible(element)) return [];
    if (!Array.from(element.childNodes || []).some((child) => child.tagName === "I")) return splitFacts(readNode(element));
    const parts = []; let current = "";
    for (const child of element.childNodes) {
      if (child.tagName === "I") { if (current.trim()) parts.push(current.trim()); current = ""; }
      else if (child.nodeType === 3) current += child.textContent || "";
      else if (isVisible(child)) current += readNode(child);
    }
    if (current.trim()) parts.push(current.trim());
    return parts;
  }

  /** normalizeRegion 去掉平台现居与城市方括号，保存可用于查询的地区原文。 */
  function normalizeRegion(value) { return shared.cleanText(value, 500).replace(/^现居\s*[·:：]?\s*/, "").replace(/[\[\]【】]/g, "").trim(); }

  /** readCurrentRegion 按各平台基础资料读取现居地，猎聘仅从第一行筛掉明确非地区字段。 */
  function readCurrentRegion(scope, adapter, platform, factValues) {
    const explicit = platform === "liepin" ? firstText(scope, [".personal-detail-location", ".nest-resume-personal-info .location", "[class*='personalInfo--'] [class*='location--']"]) : "";
    if (explicit) return normalizeRegion(explicit);
    const labeled = factValues.find((item) => /^现居/.test(item)) || factValues.flatMap(splitFacts).find((item) => /^现居/.test(item));
    if (labeled) return normalizeRegion(labeled);
    if (platform !== "hliepin") return "";
    const first = visibleNodes(scope, "#resume-detail-basic-info .basic-cont > .sep-info")[0] || visibleNodes(scope, adapter.facts[0])[0];
    return readSeparatedFacts(first).find((item) => !/^(?:男|女|\d{1,3}\s*岁|工作\s*\d+(?:\.\d+)?\s*年|博士|硕士|本科|大专|高中|中专|中共党员|党员|群众|保密|(?:\d+(?:\.\d+)?\s*[-~—]?\s*)+[kK](?:\s*[×xX*]\s*\d+\s*薪)?|在职.*|离职.*|未婚|已婚)$/.test(item)) || "";
  }

  /** readExpectedInfo 保留智联多个期望职位与城市，薪资按参考取首个明确范围并保留原文。 */
  function readExpectedInfo(scope, adapter, platform) {
    if (platform === "zhaopin") {
      const items = visibleNodes(scope, ".new-resume-purposes__item");
      if (items.length) return {
        position: uniqueTexts(items.map((item) => firstField(item, [".new-resume-purposes__item-type"]))).join("、"),
        region: uniqueTexts(items.map((item) => normalizeRegion(firstText(item, [".new-resume-purposes__item-city"])))).join("、"),
        salary: uniqueTexts(items.map((item) => firstText(item, [".new-resume-purposes__item-salary"])))[0] || ""
      };
    }
    const expected = parseExpectedText(firstField(scope, adapter.expected));
    return { ...expected, region: normalizeRegion(firstText(scope, ["#resume-detail-job-exp-info .dqname-wrap", ".new-resume-purposes__item-city", ".item-info-city"])), salary: firstText(scope, ["#resume-detail-job-exp-info .salary", ".new-resume-purposes__item-salary", ".item-info-salary", ".expected-salary"]) || expected.salary };
  }

  /** readTags 保留智联技能熟练等级及经历标签，避免把同一技能拆成两条或丢失等级。 */
  function readTags(scope, platform) {
    const tags = ["#resume-detail-skill-info .skill-tag", ".new-resume-detail-skill-tag"].flatMap((selector) => visibleNodes(scope, selector).map((item) => readNode(item, 100)));
    if (platform === "zhaopin") {
      const skills = visibleNodes(scope, ".new-professional-skills__item");
      if (skills.length) tags.push(...skills.map((item) => [firstField(item, [".new-professional-skills__item-name"], 100), firstText(item, [".new-professional-skills__item-tag"], 100)].filter(Boolean).join("-")));
      else tags.push(...visibleNodes(scope, ".new-professional-skills__item-name").map((item) => readNode(item, 100)));
    }
    return uniqueTexts(tags);
  }

  /** findModule 按可见标题限定前程无忧模块，防止把工作职责误当个人简介。 */
  function findModule(scope, labels) {
    return visibleNodes(scope, ".IM-resume-item, .IM-base-module").find((item) => labels.some((label) => firstText(item, [".module-title-text"]).includes(label))) || null;
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
      const period = parsePeriod(firstText(item, [".rd-work-time", ".school-time", ".new-work-experiences__work-time", ".new-education-experiences__item-time", ".new-education-experiences__edu-time", ".time-detail", ".item-top-time", ".detail-time", ".item-time", ".IM-info-time", ".time", "[class*='time--']"]));
      if (kind === "work") return {
        company_name: firstField(item, [".rd-work-comp h5", ".new-work-experiences__work-name > span", ".new-work-experiences__work-name", ".info-company", ".company-name", "[class*='companyName--']"]),
        position_name: firstField(item, [".job-name", ".new-work-experiences__work-job-title", ".info-position", ".position-name", "[class*='positionName--']"]),
        content: readLabeledText(item, "职责业绩") || firstText(item, [".new-work-experiences__desc", ".item-bottom.IM-info-desc", ".work-content", "[class*='content--']"], 6000), ...period
      };
      const bottom = visibleNodes(item, ".new-education-experiences__item-bottom > span").map((child) => readNode(child));
      return { school_name: firstField(item, [".school-name", ".new-education-experiences__item-name", ".detail-name", ".IM-list-title", "[class*='schoolName--']"]), major_name: firstField(item, [".school-special", ".major-name", "[class*='major--']"]) || bottom.find((text) => !/博士|硕士|本科|大专|中专|高中|\d{4}/.test(text)) || (readNode(item).match(/[^，,|｜\s]+专业/) || [])[0] || "", education_level: firstField(item, [".school-degree", ".education-level"]) || (readNode(item).match(/博士|硕士|本科|大专|中专|高中/) || [])[0] || "", ...period };
    }).filter((item) => item.company_name || item.school_name || item.position_name || item.major_name || item.education_level || item.content);
  }

  /** hasSingleName 拒绝同一弹框容器内的多个可见姓名，避免不同候选人的字段被混合读取。 */
  function hasSingleName(scope, adapter) {
    for (const selector of adapter.name) {
      const names = visibleNodes(scope, selector).filter((item) => shared.cleanText(item.getAttribute?.("title") || readNode(item, 100), 100));
      if (names.length) return names.length === 1;
    }
    return false;
  }

  /** findRoot 确定唯一可见简历详情；多位候选人同时可见时拒绝猜测。 */
  function findRoot(doc, adapter) {
    for (const selector of adapter.roots) {
      const candidates = visibleNodes(doc, selector).filter((item) => hasSingleName(item, adapter) && (!adapter.marker || visibleNodes(item, adapter.marker).length));
      const roots = candidates.filter((outer) => !candidates.some((inner) => inner !== outer && outer.contains?.(inner)));
      if (roots.length === 1) return roots[0];
      if (roots.length > 1) return null;
    }
    return null;
  }

  /** extractPhone 只在联系方式区域查完整号码，不扫整个页面的业务编号。 */
  function extractPhone(scope, selectors) {
    const phones = new Set();
    for (const selector of selectors) for (const item of visibleNodes(scope, selector)) {
      const text = readNode(item).replace(/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/g, "").replace(/(?:手机号|手机|电话|联系方式)[：:]?/g, "").trim();
      if (/(?:编号|简历号|验证码)\s*[:：]?\s*\d/.test(text)) continue;
      const phone = shared.normalizePhone(text);
      if (phone) phones.add(phone);
      // 参考猎聘逐行匹配号码，兼容号码旁的“复制”等文案；不扩展到整页文字。
      for (const match of text.matchAll(/(?<![\d+*])(?:1[3-9]\d{9}|(?:\+|00)\d[\d\s()-]{6,24}\d)(?![\d*])/g)) {
        const matched = shared.normalizePhone(match[0]); if (matched) phones.add(matched);
      }
      // 只读取当前可见联系方式中的电话链接，不读取隐藏属性、缓存或网络。
      for (const link of [item, ...visibleNodes(item, "a[href^='tel:']")]) {
        const href = link.getAttribute?.("href") || "";
        if (!/^tel:/i.test(href) || !isVisible(link) || /[*＊•]/.test(readNode(link))) continue;
        const linked = shared.normalizePhone(href.slice(4)); if (linked) phones.add(linked);
      }
    }
    return phones.size === 1 ? [...phones][0] : "";
  }

  /** readLabeledText 读取猎聘经历中带标签的正文，避免把公司规模误当职责。 */
  function readLabeledText(scope, label) {
    const row = visibleNodes(scope, ".rd-info-row").find((item) => firstText(item, [".rd-info-col-title"]).includes(label));
    return row ? firstText(row, [".rd-info-col-cont"], 6000) : "";
  }

  /** readGender 从可见文字或智联性别图标读取性别，不访问页面缓存。 */
  function readGender(scope, facts, name = "") {
    if (/(?:^|[\s/])女(?:[\s/]|$)/.test(facts)) return "female";
    if (/(?:^|[\s/])男(?:[\s/]|$)/.test(facts)) return "male";
    const image = visibleNodes(scope, ".resume-basic-new__gender img")[0];
    const icon = String(image?.getAttribute?.("src") || "").toLowerCase();
    return icon.includes("female") || icon.includes("woman") || name.endsWith("女士") ? "female" : icon.includes("male") || icon.includes("man") || name.endsWith("先生") ? "male" : "";
  }

  /** readProjects 读取当前详情可见项目节点，保持平台职责与页面操作分离。 */
  function readProjects(scope, adapter) {
    return allRows(scope, adapter.project).map((item) => ({
      project_name: firstField(item, [".rd-project-name h5", ".new-project-experiences__item-name", ".item-top-left", ".IM-list-title"]),
      role_name: readLabeledText(item, "项目职务") || firstText(item, [".new-project-experiences__item-role", ".item-center"]),
      content: readLabeledText(item, "项目描述") || firstText(item, [".new-project-experiences__item-desc", ".item-bottom.IM-info-desc"], 6000),
      ...parsePeriod(firstText(item, [".rd-project-time", ".new-project-experiences__item-time", ".item-top-right", ".IM-info-time"]))
    })).filter((item) => item.project_name || item.content);
  }

  /** readWorkYears 只提取页面明确写出的工作年限，兼容“工作7年”和“7年经验”。 */
  function readWorkYears(facts) { return (facts.match(/(?:工作|工作经验)\s*(\d+(?:\.\d+)?)\s*年/) || facts.match(/(\d+(?:\.\d+)?)\s*年(?:工作|经验|工作经验)/) || [])[1] || ""; }

  /** readPlatformWorkYears 兼容智联和前程无忧独立的“8年”字段，应届与无经验记零，生日年份不作经验。 */
  function readPlatformWorkYears(factValues, facts, platform) {
    const explicit = readWorkYears(facts); if (explicit) return explicit;
    if (!["zhaopin", "51job"].includes(platform)) return "";
    const values = factValues.flatMap(splitFacts);
    if (values.some((value) => /应届生|无经验/.test(value))) return "0";
    return values.map((value) => value.match(/^(\d{1,2}(?:\.\d+)?)\s*年(?:工作经验)?$/)?.[1]).find(Boolean) || "";
  }

  /** parseExpectedText 分开期望区域明确显示的职位和 K 薪资，保留薪资原格式。 */
  function parseExpectedText(text) {
    const salary = String(text || "").match(/\d+(?:\.\d+)?\s*(?:[-~—]\s*\d+(?:\.\d+)?)?\s*[kK](?:\s*[/／]\s*(?:月|年))?(?:\s*[×xX*]\s*\d+\s*薪)?/);
    return salary ? { position: shared.cleanText(text.slice(0, salary.index).replace(/[·|：:\s]+$/, ""), 500), salary: salary[0] } : { position: text, salary: "" };
  }

  /** readAvatarSource 只读当前候选人区域唯一可见头像的图片地址，不请求图片或读取缓存。 */
  function readAvatarSource(scope, adapter, href) {
    for (const selector of adapter.avatar || []) {
      const images = visibleNodes(scope, selector);
      if (images.length > 1) return "";
      const image = images[0], source = image?.currentSrc || image?.getAttribute?.("src") || "";
      const safe = shared.safeAvatarSource(source, href); if (safe) return safe;
    }
    return "";
  }

  /** extractCurrentCandidate 采集一个当前可见候选人，白名单外和未打开详情返回空值。 */
  function extractCurrentCandidate(doc, href) {
    const platform = shared.platformForURL(href), adapter = ADAPTERS[platform];
    if (!adapter || (platform === "hliepin" && !new URL(href).pathname.startsWith("/resume/"))) return null;
    const url = new URL(href);
    let scope = findRoot(doc, adapter);
    if (!scope && platform === "liepin" && url.pathname === "/resume/detail" && visibleNodes(doc, ".nest-resume-personal-name").length === 1) scope = visibleNodes(doc, "body")[0] || null;
    if (!scope) return null;
    const name = firstField(scope, adapter.name, 100); if (!name) return null;
    const factValues = adapter.facts.flatMap((selector) => visibleNodes(scope, selector).map((item) => platform === "hliepin" ? readSeparatedFacts(item).join(" / ") : readNode(item, 500))).filter(Boolean);
    const facts = factValues.join(" / ").slice(0, 1500);
    const basicFacts = platform === "51job" ? firstText(scope, [".baseinfo-container"], 1500) || facts : facts;
    const works = readExperiences(scope, adapter, "work"), educations = readExperiences(scope, adapter, "education");
    if (!educations.length && platform === "liepin") {
      const school = firstText(scope, [".resume-university"]);
      if (school) { const parts = school.split(/[·|]/).map((item) => item.trim()).filter(Boolean); educations.push({ school_name: parts[0], major_name: parts[1] && !/博士|硕士|本科|大专|中专|高中|统招/.test(parts[1]) ? parts[1] : "", education_level: parts.find((item) => /^(?:博士|硕士|本科|大专|中专|高中)$/.test(item)) || "", start_ym: "", end_ym: "" }); }
    }
    const age = Number((basicFacts.match(/(?:^|\D)(\d{1,2})\s*岁/) || [])[1]) || null;
    const emailPattern = /[\w.+-]+@[\w.-]+\.[a-z]{2,}/i;
    const email = firstText(scope, [".resume-basic-new__email", ".email"]).match(emailPattern)?.[0] || visibleNodes(scope, ".connect-box dd").map((item) => readNode(item)).join(" ").match(emailPattern)?.[0] || (platform === "51job" ? readNode(scope, 50000).match(emailPattern)?.[0] : "") || "";
    const expected = readExpectedInfo(scope, adapter, platform), currentRegion = readCurrentRegion(scope, adapter, platform, factValues);
    const hunterRows = platform === "hliepin" ? visibleNodes(scope, "#resume-detail-basic-info .basic-cont > .sep-info") : [];
    const currentPosition = readSeparatedFacts(hunterRows[1])[0] || works[0]?.position_name || (platform === "51job" ? firstText(scope, [".info-position"]) : "") || (platform === "zhaopin" ? expected.position : "");
    const personalModule = platform === "51job" ? findModule(scope, ["个人优势", "自我评价", "个人简介"]) : null;
    const personalDescription = platform === "51job" ? firstText(personalModule || scope, personalModule ? [".advantage-container", ".IM-info-desc"] : [".advantage-container"], 6000) : firstText(scope, adapter.description, 6000);
    const result = shared.sanitizeResume({
      candidate_name: name, phone: extractPhone(scope, adapter.phone) || (platform === "51job" ? extractPhone(doc, adapter.phone) : ""), email, age,
      gender: readGender(scope, basicFacts, name),
      birth_ym: (facts.match(/(\d{4})[年./-](\d{1,2})月?/) || []).slice(1, 3).map((part) => part.padStart(2, "0")).join("-"),
      work_years: readPlatformWorkYears(factValues, facts, platform),
      work_region: currentRegion,
      education_level: (facts.match(/博士|硕士|本科|大专|中专|高中/) || [])[0] || educations[0]?.education_level || "",
      expected_position: expected.position, current_position: currentPosition,
      expected_region: expected.region,
      expected_salary_text: expected.salary,
      current_salary_text: firstText(scope, [".current-salary", ".personal-detail-salary"]),
      personal_description: personalDescription, basic_info: facts,
      work_status: firstText(scope, [".user-status-tag", ".work-status", ".workStatus"]) || (facts.match(/在职[，,、\s]*(?:急寻新工作|正在找工作|考虑机会|暂不找工作)|离职[，,、\s]*(?:随时到岗|正在找工作)/) || [])[0] || "",
      online_status: firstText(scope, [".global-active-tag", ".avtive-time", ".online-status"]),
      work_experiences: works, educations, project_experiences: readProjects(scope, adapter),
      certificates: visibleNodes(scope, ".new-resume-certificates__item-name").slice(0, 40).map((item) => ({ certificate_name: shared.cleanText(item.getAttribute?.("title") || readNode(item, 200), 200) })),
      tags: readTags(scope, platform),
      platform_id: platform, source_url: href,
      platform_candidate_id: shared.cleanText(url.searchParams.get("resIdEncode") || url.searchParams.get("res_id_encode") || url.searchParams.get("resumeId") || url.searchParams.get("resume_id") || "", 120)
    });
    const avatarSource = readAvatarSource(scope, adapter, href);
    if (avatarSource) result.avatar_source_url = avatarSource;
    return result;
  }

  const api = { ADAPTERS, isVisible, readNode, parsePeriod, readWorkYears, parseExpectedText, extractPhone, readAvatarSource, extractCurrentCandidate };
  root.GoodHRExtraction = api; if (typeof module !== "undefined" && module.exports) module.exports = api;
})(globalThis);
