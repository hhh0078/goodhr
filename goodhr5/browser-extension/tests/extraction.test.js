/* 文件作用：用无个人资料的可见 DOM 场景验证平台隔离、详情选择、手机号及年月采集。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict");
const shared = require("../src/shared.js"), extraction = require("../src/extraction.js");

/** node 构造不可写的 DOM 测试替身，模拟可见和隐藏元素。 */
function node(text = "", selectors = {}, hidden = false) {
  return Object.freeze({ innerText: text, hidden, querySelectorAll: (selector) => selectors[selector] || [], contains: (other) => Object.values(selectors).flat().some((child) => child === other || child.contains?.(other)), closest: () => hidden ? {} : null, getClientRects: () => hidden ? [] : [{}] });
}

/** imageNode 构造只读可见图片替身，只提供已显示节点的src，不伪造个人头像。 */
function imageNode(source, hidden = false) { return Object.freeze({ ...node("", {}, hidden), currentSrc: source, getAttribute: (name) => name === "src" ? source : "" }); }

/** titledNode 模拟页面省略文字和鼠标可查看的完整标题，仍然保持只读。 */
function titledNode(text, title, hidden = false) { return Object.freeze({ ...node(text, {}, hidden), getAttribute: (name) => name === "title" ? title : "" }); }

/** separatedNode 模拟猎聘以分隔图标划分字段的可见基础行。 */
function separatedNode(values) { return Object.freeze({ ...node(values.join("")), childNodes: values.flatMap((value, index) => [...(index ? [{ nodeType: 1, tagName: "I" }] : []), { nodeType: 3, textContent: value }]) }); }

/** fixture 为指定平台构造只包含当前详情的测试页面。 */
function fixture(platform, phone = "13812345678", hiddenPhone = false) {
  const adapter = extraction.ADAPTERS[platform];
  const map = { [adapter.name[0]]: [node("测试候选人")], [adapter.facts[0]]: [node("男 / 32岁 / 本科 / 8年工作经验 / 现居上海")], [adapter.phone[0]]: [node(phone, {}, hiddenPhone)] };
  if (adapter.marker) map[adapter.marker] = [node("详情")];
  const resume = node("", map), doc = node("", { [adapter.roots[0]]: [resume] });
  return { doc, resume, map };
}

test("平台域名使用精确 HTTPS 白名单，拒绝相似域名与非 HTTPS", () => {
  assert.equal(shared.platformForURL("https://h.liepin.com/resume/showresumedetail"), "hliepin");
  assert.equal(shared.platformForURL("https://h.liepin.com.evil.example/resume/"), "");
  assert.equal(shared.platformForURL("http://lpt.liepin.com/resume"), "");
  assert.equal(shared.platformForURL("https://rd6.zhaopin.com:8443/app"), "");
});

test("手机号只接受完整号码，掩码/编号/多号码不能误入库", () => {
  assert.equal(shared.normalizePhone("+86 138-1234-5678"), "13812345678");
  for (const value of ["138****5678", "编号13812345678", "138123456789", "13812345678 13912345678", "11111111111", null]) assert.equal(shared.normalizePhone(value), "");
});

test("四种平台都可只读提取当前可见姓名与手机号", () => {
  const urls = { liepin: "https://lpt.liepin.com/chat/im#preview", hliepin: "https://h.liepin.com/resume/showresumedetail?res_id_encode=demo", zhaopin: "https://rd6.zhaopin.com/app/recommend", "51job": "https://ehire.51job.com/feature/im" };
  for (const [platform, url] of Object.entries(urls)) {
    const value = extraction.extractCurrentCandidate(fixture(platform).doc, url);
    assert.equal(value.candidate_name, "测试候选人"); assert.equal(value.phone, "13812345678"); assert.equal(value.platform_id, platform); assert.equal(value.age, 32); assert.equal(value.work_years, "8");
    assert.equal(value.source_url.includes("?"), false);
  }
});

test("掩码与隐藏联系方式留空，不读取隐藏缓存和整页文本", () => {
  const hidden = fixture("zhaopin", "13812345678", true);
  assert.equal(extraction.extractCurrentCandidate(hidden.doc, "https://rd6.zhaopin.com/app").phone, "");
  const masked = fixture("hliepin", "138****5678");
  assert.equal(extraction.extractCurrentCandidate(masked.doc, "https://h.liepin.com/resume/showresumedetail").phone, "");
});

test("详情关闭或两位候选人同时可见时拒绝猜测", () => {
  assert.equal(extraction.extractCurrentCandidate(node(), "https://lpt.liepin.com/recommend"), null);
  const first = fixture("zhaopin"), second = fixture("zhaopin");
  const doc = node("", { ".new-resume-detail--inner": [first.resume, second.resume] });
  assert.equal(extraction.extractCurrentCandidate(doc, "https://rd6.zhaopin.com/app"), null);
});

test("经历年月规范化，在职结束时间留空", () => {
  assert.deepEqual(extraction.parsePeriod("2020.01 - 至今"), { start_ym: "2020-01", end_ym: "" });
  assert.deepEqual(extraction.parsePeriod("2020/1 - 2023/6"), { start_ym: "2020-01", end_ym: "2023-06" });
  assert.deepEqual(extraction.parsePeriod("未知"), { start_ym: "", end_ym: "" });
  assert.deepEqual(extraction.parsePeriod("2020/19 - 2023/6"), { start_ym: "", end_ym: "" });
});

test("猎聘企业嵌套预览只取内层可见详情，隐藏背景卡片和外层文案不串人", () => {
  const fixtureData = require("./fixtures/enterprise-preview.json");
  const detail = node("", {
    ".nest-resume-personal-name": [node(fixtureData.visible.name)],
    ".personal-detail-age": [node("34岁")], ".personal-detail-edulevel": [node("本科")], ".personal-detail-location": [node(fixtureData.visible.city)],
    ".contact-phone": [node(fixtureData.visible.phone)], ".resume-university": [node(fixtureData.visible.school)],
    ".resume-description": [node(fixtureData.visible.description)], ".personal-expect-content": [node(fixtureData.visible.expected_position)]
  });
  const outer = node("", { ".ant-lpt-modal": [detail], ".nest-resume-personal-name": [node(fixtureData.background.name)], ".contact-phone": [node(fixtureData.background.phone)] });
  const hiddenBackground = node("", { ".nest-resume-personal-name": [node(fixtureData.background.name)], ".contact-phone": [node(fixtureData.background.phone)] }, true);
  const doc = node("", { ".ant-lpt-modal": [outer, detail, hiddenBackground] });
  const result = extraction.extractCurrentCandidate(doc, "https://lpt.liepin.com/chat/im#preview");
  assert.equal(result.candidate_name, fixtureData.visible.name); assert.equal(result.phone, fixtureData.visible.phone); assert.equal(result.work_region, fixtureData.visible.city);
  assert.equal(result.educations[0].school_name, fixtureData.visible.school); assert.equal(result.personal_description, fixtureData.visible.description); assert.equal(result.expected_position, fixtureData.visible.expected_position);
  assert.equal(JSON.stringify(result).includes(fixtureData.background.name), false);
});

test("输入白名单不携带 Cookie、运行态或 source_url 临时查询", () => {
  const value = shared.sanitizeResume({ candidate_name: "测试", phone: "13812345678", cookie: "forbidden", token: "forbidden", runtime: { id: 1 }, source_url: "https://h.liepin.com/resume/showresumedetail?token=not-uploaded#hidden", work_experiences: [{ company_name: "测试公司", private_key: "forbidden" }] });
  assert.equal(value.cookie, undefined); assert.equal(value.token, undefined); assert.equal(value.runtime, undefined); assert.equal(value.source_url, "https://h.liepin.com/resume/showresumedetail"); assert.equal(value.work_experiences[0].private_key, undefined);
});

test("企业独立详情仅唯一明确姓名时允许body，chat不读整页且resIdEncode只保存合成ID", () => {
  const names = [node("合成独立候选人")], body = node("", { ".nest-resume-personal-name": names, ".personal-detail-age": [node("28 岁 / 工作7年 / 在职，急寻新工作")], ".resume-university": [node("合成信息学院 · 测试工程 · 大专 · 统招")], ".contact-phone": [node("13800000001")], ".current-salary": [node("4k×12薪")], ".personal-expect-content": [node("测试开发14-15k×12薪测试城市")] });
  const doc = node("", { body: [body], ".nest-resume-personal-name": names });
  const result = extraction.extractCurrentCandidate(doc, "https://lpt.liepin.com/resume/detail?resIdEncode=synthetic-resume-001&ejobId=synthetic-job&sfrom=fixture");
  assert.equal(result.candidate_name, "合成独立候选人"); assert.equal(result.work_years, "7"); assert.equal(result.age, 28); assert.equal(result.work_status, "在职，急寻新工作");
  assert.equal(result.educations[0].school_name, "合成信息学院"); assert.equal(result.educations[0].major_name, "测试工程"); assert.equal(result.educations[0].education_level, "大专");
  assert.equal(result.current_salary_text, "4k×12薪"); assert.equal(result.expected_salary_text, "14-15k×12薪"); assert.equal(result.expected_position, "测试开发");
  assert.equal(result.platform_candidate_id, "synthetic-resume-001"); assert.equal(result.source_url, "https://lpt.liepin.com/resume/detail");
  assert.equal(extraction.extractCurrentCandidate(doc, "https://lpt.liepin.com/chat/im#preview"), null);
  assert.equal(extraction.extractCurrentCandidate(node("", { body: [body], ".nest-resume-personal-name": [...names, node("另一个合成候选人")] }), "https://lpt.liepin.com/resume/detail"), null);
  assert.equal(extraction.readWorkYears("7年经验"), "7"); assert.equal(extraction.readWorkYears("2020年至今 任职记录"), "");
});

test("四平台头像仅来自当前详情明确可见头像节点，隐藏、两个头像和未授权CDN不采集", () => {
  const urls = { liepin: "https://lpt.liepin.com/resume/detail", hliepin: "https://h.liepin.com/resume/showresumedetail", zhaopin: "https://rd6.zhaopin.com/app", "51job": "https://ehire.51job.com/feature/im" };
  for (const [platform, url] of Object.entries(urls)) {
    const scene = fixture(platform), selector = extraction.ADAPTERS[platform].avatar[0], source = new URL("/synthetic-avatar.png", url).href;
    scene.map[selector] = [imageNode(source)]; const result = extraction.extractCurrentCandidate(scene.doc, url); assert.equal(result.avatar_source_url, source);
    scene.map[selector] = [imageNode(source, true)]; assert.equal(extraction.extractCurrentCandidate(scene.doc, url).avatar_source_url, undefined);
    scene.map[selector] = [imageNode(source), imageNode(source)]; assert.equal(extraction.extractCurrentCandidate(scene.doc, url).avatar_source_url, undefined);
    scene.map[selector] = [imageNode("https://evil.test/logo.png")]; assert.equal(extraction.extractCurrentCandidate(scene.doc, url).avatar_source_url, undefined);
  }
});

test("猎聘猎头分隔基础资料、完整职务、履历年月与期望字段和参考一致", () => {
  const scene = fixture("hliepin", "13800000002"), rows = [separatedNode(["女", "35岁", "工作12年", "硕士", "上海", "中共党员"]), separatedNode(["当前合成岗位", "合成公司"])];
  scene.map["#resume-detail-basic-info .sep-info"] = rows;
  scene.map["#resume-detail-basic-info .basic-cont > .sep-info"] = rows;
  scene.map["#resume-detail-basic-info .name"] = [titledNode("测试…", "合成猎头候选人")];
  scene.map["#resume-detail-job-exp-info .tabs .title"] = [titledNode("期望…", "合成期望职务")];
  scene.map["#resume-detail-job-exp-info .dqname-wrap"] = [node("上海、杭州")];
  scene.map["#resume-detail-job-exp-info .salary"] = [node("15.5-20k×14薪")];
  scene.map["#resume-detail-basic-info .user-status-tag"] = [node("在职，考虑机会")];
  scene.map[".user-status-tag"] = scene.map["#resume-detail-basic-info .user-status-tag"];
  const result = extraction.extractCurrentCandidate(scene.doc, "https://h.liepin.com/resume/showresumedetail?res_id_encode=synthetic-hunter");
  assert.equal(result.candidate_name, "合成猎头候选人"); assert.equal(result.gender, "female"); assert.equal(result.age, 35);
  assert.equal(result.work_region, "上海"); assert.equal(result.current_position, "当前合成岗位"); assert.equal(result.work_years, "12");
  assert.equal(result.expected_position, "合成期望职务"); assert.equal(result.expected_region, "上海、杭州"); assert.equal(result.expected_salary_text, "15.5-20k×14薪");
  assert.equal(result.work_status, "在职，考虑机会"); assert.equal(result.platform_candidate_id, "synthetic-hunter");
});

test("智联保留多个职位城市、技能等级、证书和教育年月，不把生日当工作经验", () => {
  const scene = fixture("zhaopin", "13800000003");
  scene.map[".resume-basic-new__meta-item"] = [node("女"), node("31岁（1995年6月）"), node("8年"), node("硕士"), node("现居【上海】")];
  scene.map[".new-resume-purposes__item"] = [
    node("", { ".new-resume-purposes__item-type": [titledNode("开发…", "合成开发工程师")], ".new-resume-purposes__item-city": [node("[上海]")], ".new-resume-purposes__item-salary": [node("15000-20000元/月")] }),
    node("", { ".new-resume-purposes__item-type": [node("合成架构师")], ".new-resume-purposes__item-city": [node("杭州")], ".new-resume-purposes__item-salary": [node("20000-25000元/月")] }),
    node("", { ".new-resume-purposes__item-type": [node("不应读取的职位")] }, true)
  ];
  scene.map[".new-professional-skills__item"] = [node("", { ".new-professional-skills__item-name": [titledNode("合成…", "合成技能")], ".new-professional-skills__item-tag": [node("精通")] })];
  scene.map[".new-resume-detail-skill-tag"] = [node("合成经历标签")];
  scene.map[".new-resume-certificates__item-name"] = [titledNode("合成证…", "合成证书")];
  scene.map[".new-education-experiences__item"] = [node("", { ".new-education-experiences__item-name": [titledNode("合成学…", "合成大学")], ".new-education-experiences__item-time": [node("2010.09 - 2014.06")], ".new-education-experiences__item-bottom > span": [node("合成工程"), node("本科")] })];
  const result = extraction.extractCurrentCandidate(scene.doc, "https://rd6.zhaopin.com/app/resume?resumeId=synthetic-zhaopin");
  assert.equal(result.expected_position, "合成开发工程师、合成架构师"); assert.equal(result.expected_region, "上海、杭州"); assert.equal(result.expected_salary_text, "15000-20000元/月");
  assert.equal(result.work_region, "上海"); assert.equal(result.work_years, "8"); assert.equal(result.birth_ym, "1995-06");
  assert.deepEqual(result.tags, ["合成经历标签", "合成技能-精通"]); assert.equal(result.certificates[0].certificate_name, "合成证书");
  assert.equal(result.educations[0].school_name, "合成大学"); assert.equal(result.educations[0].major_name, "合成工程"); assert.equal(result.educations[0].start_ym, "2010-09"); assert.equal(result.educations[0].end_ym, "2014-06");
  scene.map[".resume-basic-new__meta-item"] = [node("31岁（1995年6月）"), node("无经验")];
  assert.equal(extraction.extractCurrentCandidate(scene.doc, "https://rd6.zhaopin.com/app").work_years, "0");
  scene.map[".resume-basic-new__meta-item"] = [node("31岁（1995年6月）")];
  assert.equal(extraction.extractCurrentCandidate(scene.doc, "https://rd6.zhaopin.com/app").work_years, "");
});

test("前程无忧按模块读取简介及混合教育节点，保留完整经历和可见邮箱", () => {
  const scene = fixture("51job", "13800000004"), personal = node("", { ".module-title-text": [node("自我评价")], ".IM-info-desc": [node("这条才是合成个人简介")] });
  scene.map[".baseinfo-container .center-top-item"] = [node("女"), node("29岁"), node("5年"), node("现居·深圳")];
  scene.map[".IM-resume-item, .IM-base-module"] = [node("", { ".module-title-text": [node("工作经历")], ".IM-info-desc": [node("这条是工作职责")] }), personal];
  scene.map[".work-container-item"] = [node("", { ".info-company": [node("合成公司")], ".info-position": [node("合成工程师")], ".time-detail": [node("2020/03 - 至今")], ".item-bottom.IM-info-desc": [node("合成职责")] })];
  scene.map[".school-container-item, .edu-container-item"] = [node("本科 / 合成专业", { ".detail-name": [node("合成学院")], ".detail-time": [node("2015/09 - 2019/06")] }), node("硕士", { ".school-name": [node("合成研究学院")], ".item-time": [node("2019/09 - 2021/06")] })];
  scene.map[".IM-base-module .IM-info-desc"] = [node("这条是工作职责")];
  const baseResume = node("合成可见联系邮箱 fixture@example.test", scene.map), doc = node("", { ".con-container .resume": [baseResume] });
  const result = extraction.extractCurrentCandidate(doc, "https://ehire.51job.com/Revision/Resume");
  assert.equal(result.personal_description, "这条才是合成个人简介"); assert.equal(result.email, "fixture@example.test"); assert.equal(result.work_years, "5"); assert.equal(result.work_region, "深圳");
  assert.equal(result.work_experiences[0].company_name, "合成公司"); assert.equal(result.work_experiences[0].content, "合成职责"); assert.equal(result.work_experiences[0].start_ym, "2020-03");
  assert.equal(result.educations.length, 2); assert.equal(result.educations[0].major_name, "合成专业"); assert.equal(result.educations[1].school_name, "合成研究学院");
});

test("同一联系区域出现两个不同可见号码时要求补填，不猜属于谁", () => {
  const scene = fixture("hliepin"); scene.map["#resume-detail-basic-info .connect-box dd"] = [node("13800000005"), node("13800000006")];
  assert.equal(extraction.extractCurrentCandidate(scene.doc, "https://h.liepin.com/resume/showresumedetail").phone, "");
});

test("同一弹框容器内两份姓名同时可见时不把两人的资料混为一份", () => {
  const scene = fixture("liepin"); scene.map[".nest-resume-personal-name"] = [node("合成候选人甲"), node("合成候选人乙")];
  assert.equal(extraction.extractCurrentCandidate(scene.doc, "https://lpt.liepin.com/chat/im#preview"), null);
  scene.map[".nest-resume-personal-name"] = [node("合成候选人甲"), node("合成候选人乙", {}, true)];
  assert.equal(extraction.extractCurrentCandidate(scene.doc, "https://lpt.liepin.com/chat/im#preview").candidate_name, "合成候选人甲");
});
