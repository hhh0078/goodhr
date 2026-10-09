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
