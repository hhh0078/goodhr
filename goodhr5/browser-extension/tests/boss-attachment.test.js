/* 文件作用：验证 BOSS 附件的纯文本排版、头像图片范围、最早经历与人工核对合并；所有资料均为合成内容。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict");
const reader = require("../src/boss-attachment.js"), shared = require("../src/shared.js"), { manifest } = require("../scripts/build.js");

/** piece 创建不含个人资料的排版片段，便于独立测试视觉顺序。 */
function piece(text, x, y, extra = {}) { return { text, x, y, width: 100, height: 12, rotation: 0, order: 0, ...extra }; }

/** glyphs 模拟PDF将同一行拆成逐字span，间距与字宽独立，避免大块字符串掩盖跨页中线断词。 */
function glyphs(text, x, y, step = 12) { return Array.from(text, (char, index) => piece(char, x + index * step, y, { width: step - 1, height: 14, order: index })); }

/** imageNode 构造只读附件图片替身，用自然尺寸区分头像、装饰和整页图片。 */
function imageNode(source, options = {}) {
  const attributes = { src: options.svg ? "" : source, href: options.svg ? source : "", alt: options.alt || "", class: options.className || "" }, page = { getBoundingClientRect: () => ({ width: 750, height: 1000 }) };
  return Object.freeze({ naturalWidth: options.svg ? 0 : options.width ?? 120, naturalHeight: options.svg ? 0 : options.height ?? 160, hidden: options.hidden === true, getClientRects: () => [{}], closest: (selector) => selector === ".page[data-page-number]" ? page : null, getAttribute: (name) => attributes[name] || "", getBoundingClientRect: () => ({ width: options.displayWidth ?? 120, height: options.displayHeight ?? 160 }) });
}

/** pdfFixture 创建纯合成的唯一附件与文字层，画布只作不可读取的占位对象。 */
function pdfFixture(images = [], loadedPages = [true], total = 0) {
  const pages = loadedPages.map((loaded, index) => {
    const span = { childNodes: [{ nodeType: 3, nodeValue: "合成附件第" + (index + 1) + "页" }], closest: () => null, getClientRects: () => [{}], getAttribute: () => "", getBoundingClientRect: () => ({ left: 10, top: 20, width: 120, height: 12 }) }, layer = { closest: () => null, getClientRects: () => [{}], querySelectorAll: () => [span] };
    return { closest: () => null, getClientRects: () => [{}], getAttribute: (name) => name === "data-page-number" ? String(index + 1) : "", getBoundingClientRect: () => ({ left: 0, top: 0, width: 750, height: 1000 }), querySelectorAll: () => loaded ? [layer] : [] };
  });
  const canvases = [Object.freeze({}), Object.freeze({})], viewer = { closest: () => null, getClientRects: () => [{}], getAttribute: (name) => name === "data-page-count" && total ? String(total) : "", querySelectorAll: (selector) => ({ ".page[data-page-number]": pages, "img, image": images, canvas: canvases }[selector] || []) };
  return { viewer, doc: { querySelectorAll: (selector) => selector === ".bzl-office-pdf" ? [viewer] : [] } };
}

test("BOSS 只开放精确 HTTPS 域名，保持顶层脚本和无网络拦截权限", () => {
  assert.equal(shared.platformForURL("https://www.zhipin.com/web/chat/index?token=secret"), "boss");
  for (const url of ["http://www.zhipin.com/", "https://zhipin.com/", "https://www.zhipin.com.evil.test/", "https://www.zhipin.com:444/"]) assert.equal(shared.platformForURL(url), "");
  for (const browser of ["chrome", "firefox"]) { const result = manifest(browser); assert.equal(result.content_scripts[0].all_frames, false); assert.ok(result.content_scripts[0].js.includes("src/boss-attachment.js")); assert.ok(result.content_scripts[0].matches.includes("https://www.zhipin.com/*")); assert.ok(result.permissions.includes("scripting")); assert.ok(result.host_permissions.includes("https://img.bosszhipin.com/*")); assert.equal(result.permissions.some((item) => /webRequest|debugger|cookies/.test(item)), false); }
});

test("视觉顺序恢复标题、连续双栏和公司日期，不能采用原 DOM 顺序", () => {
  const pieces = [piece("右栏二", 410, 44), piece("正文标题", 70, 10, { width: 500 }), piece("左栏二", 70, 44), piece("右栏一", 410, 28), piece("左栏一", 70, 28), piece("合成公司", 70, 90), piece("2020-01 — 2022-01", 490, 90), piece("合成学校", 70, 105), piece("2016-09 — 2020-06", 490, 105)];
  assert.equal(reader.layoutText(pieces, 747), "正文标题\n左栏一\n左栏二\n右栏一\n右栏二\n合成公司 2020-01 — 2022-01\n合成学校 2016-09 — 2020-06");
});

test("重复长串并有旋转才去水印，普通旋转文字与单个长编号仍保留", () => {
  const mark = "SYNTHETIC_WATERMARK_01234567890123456789", pieces = [piece(mark, 0, 0), piece(mark, 200, 200, { rotation: -40 }), piece(mark, 500, 500, { rotation: -90 }), piece("S", 0, 0, { rotation: -40 }), piece("合法旋转正文", 0, 20, { rotation: -40 }), piece("ANOTHER_SINGLE_IDENTIFIER_01234567890", 0, 40)];
  assert.deepEqual(reader.cleanPieces(pieces).map((item) => item.text), ["合法旋转正文", "ANOTHER_SINGLE_IDENTIFIER_01234567890"]);
  assert.equal(reader.cleanPieces([piece(mark, 0, 0), piece(mark, 0, 1), piece(mark, 0, 2)]).length, 3);
});

test("逐字专业和邮箱横跨48%页宽仍保持完整，字段之间的空隙不把词语拆到后文", () => {
  const pieces = [...glyphs("姓名：合成甲", 70, 20), ...glyphs("政治面貌：共青团员", 300, 20), ...glyphs("学历：本科在读", 70, 40), ...glyphs("专业：信息管理与信息系统", 300, 40), ...glyphs("电话：13800000991", 70, 60, 8), ...glyphs("邮箱：synthetic@example.test", 300, 60, 7)];
  const lines = reader.layoutText(pieces.reverse(), 747).split("\n");
  for (const field of ["专业：信息管理与信息系统", "邮箱：synthetic@example.test", "电话：13800000991", "政治面貌：共青团员"]) assert.ok(lines.some((line) => line.replace(/\s/g, "").includes(field)), field);
  assert.equal(lines.filter((line) => line.includes("信息管理与信息系统")).length, 1);
});

test("偏离页中线的真双栏使用稳定空隙，局部缺左栏与跨栏标题仍按完整段落输出", () => {
  const pieces = [piece("跨栏标题", 40, 0, { width: 600 }), piece("左栏一", 40, 30, { width: 120 }), piece("右栏一整段横跨页中线", 270, 30, { width: 300 }), piece("右栏中间独立行", 270, 46, { width: 300 }), piece("左栏二", 40, 62, { width: 120 }), piece("右栏二", 270, 62, { width: 300 }), piece("跨栏结尾", 40, 90, { width: 600 })];
  assert.equal(reader.layoutText(pieces, 747), "跨栏标题\n左栏一\n左栏二\n右栏一整段横跨页中线\n右栏中间独立行\n右栏二\n跨栏结尾");
});

test("字号不同但基线相同的标题合为一行，轻微基线偏移不打散专业正文", () => {
  const pieces = [piece("个人简历", 40, 0, { width: 150, height: 26 }), piece("PERSONAL RESUME", 230, 12, { width: 250, height: 14 }), ...glyphs("专业：信息管理与信息系统", 300, 55).map((item, index) => ({ ...item, y: item.y + (index % 3 - 1) * 2 }))];
  assert.equal(reader.layoutText(pieces, 747), "个人简历 PERSONAL RESUME\n专业：信息管理与信息系统");
});

test("三列日期公司岗位保持同一视觉行关联，不把日期和雇主分别抽成纵列", () => {
  const pieces = [piece("2024.01-2024.03", 60, 20, { width: 150 }), piece("合成甲公司", 270, 20, { width: 170 }), piece("运营", 600, 20, { width: 60 }), piece("2025.01-2025.03", 60, 45, { width: 150 }), piece("合成乙公司", 270, 45, { width: 170 }), piece("工程师", 600, 45, { width: 60 })];
  assert.equal(reader.layoutText(pieces, 747), "2024.01-2024.03 合成甲公司 运营\n2025.01-2025.03 合成乙公司 工程师");
  const years = [piece("EmployerA", 70, 10, { width: 150 }), piece("2018", 450, 10, { width: 40 }), piece("2020", 540, 10, { width: 40 }), piece("EmployerB", 70, 25, { width: 150 }), piece("2019", 450, 25, { width: 40 }), piece("2021", 540, 25, { width: 40 })];
  assert.equal(reader.layoutText(years, 747), "EmployerA 2018 2020\nEmployerB 2019 2021");
});

test("带波浪号的重复旋转水印去掉，兼容汉字恢复且普通编号和旋转正文保留", () => {
  const mark = "SYNTHETIC_WATERMARK_01234567890123456789~~", pieces = [piece(mark, 0, 0), piece(mark, 200, 200, { rotation: -40 }), piece(mark, 500, 500, { rotation: -90 }), piece("~", 0, 300, { rotation: -40 }), piece("合法旋转正文", 0, 350, { rotation: -40 }), piece("SINGLE_VALID_IDENTIFIER_0123456789~~", 0, 400)];
  assert.deepEqual(reader.cleanPieces(pieces).map((item) => item.text), ["合法旋转正文", "SINGLE_VALID_IDENTIFIER_0123456789~~"]);
  assert.equal(reader.ownText({ childNodes: [{ nodeType: 3, nodeValue: "⼯作：信息管理与信息系统" }] }), "工作:信息管理与信息系统");
});

test("最早经历按开始年月而非数组顺序，完整解析保留其它经历和人工清空", () => {
  const full = { candidate_name: "完整识别姓名", phone: "13800000991", email: "synthetic@example.test", gender: "male", ai_summary: "忠实于合成履历的摘要", work_experiences: [{ company_name: "较晚公司", start_ym: "2022-01", content: "较晚完整内容" }, { company_name: "最早公司", start_ym: "2017-01", content: "最早完整内容" }], educations: [{ school_name: "较晚学校", start_ym: "2014-09" }, { school_name: "最早学校", start_ym: "2010-09" }] };
  assert.equal(shared.attachmentReviewFields(full)["first-company"], "最早公司"); assert.equal(shared.attachmentReviewFields(full)["first-school"], "最早学校");
  const reviewed = { candidate_name: "人工核对姓名", phone: "13800000992", gender: "female" }, fields = { "first-company": "人工核对首份公司", "first-work-start": "2017-02", "first-work-end": "", "first-school": "人工核对首所学校", "first-school-start": "2010-09", "first-school-end": "2014-06" };
  const result = shared.mergeAttachmentReview(full, reviewed, fields, ["name", "first-work-end"], true);
  assert.equal(result.candidate_name, reviewed.candidate_name); assert.equal(result.phone, reviewed.phone); assert.equal(result.gender, "female"); assert.equal(result.email, full.email); assert.equal(result.ai_summary, full.ai_summary);
  assert.equal(result.work_experiences.length, 2); assert.equal(result.work_experiences[0].content, "较晚完整内容"); assert.equal(result.work_experiences[1].content, "最早完整内容"); assert.equal(result.work_experiences[1].company_name, fields["first-company"]); assert.equal(result.work_experiences[1].end_ym, "");
  assert.equal(result.educations[1].school_name, fields["first-school"]); assert.equal(full.work_experiences[1].company_name, "最早公司");
});

test("附件正文不会混入候选人白名单或查重，身份不依赖 AI 姓名和人工改号", async () => {
  const text = "仅用于本机合成测试的附件正文", text_hash = await shared.hashText(text), envelope = { text, text_hash, page_count: 1, total_pages: 0, complete: false };
  assert.equal(text_hash.length, 64); assert.equal(shared.sanitizeAttachment(envelope).text, text);
  assert.equal(shared.sanitizeAttachment({ ...envelope, text_hash: "fake" }), null); assert.equal(shared.sanitizeAttachment({ ...envelope, text: "字".repeat(200001) }), null);
  const resume = { platform_id: "boss", source_url: "https://www.zhipin.com/web/chat/index", attachment: envelope };
  assert.equal(shared.resumeIdentity(resume), shared.resumeIdentity({ ...resume, candidate_name: "手改", phone: "13800000993" }));
  assert.equal(JSON.stringify(shared.sanitizeResume(resume)).includes(text), false); assert.equal(JSON.stringify(shared.duplicateInput(resume)).includes(text), false);
});

test("附件完整结果保留长文与100条经历，只有出生年份时写入基本资料且不编月份，查重仍只发最早线索", () => {
  const full = { platform_id: "boss", candidate_name: "合成完整候选人", phone: "13800000991", gender: "女", birth_ym: "1990", basic_info: "基".repeat(900), personal_description: "介".repeat(7000), ai_summary: "摘".repeat(13000), work_experiences: Array.from({ length: 100 }, (_, index) => ({ company_name: "合成公司" + index, start_ym: index === 99 ? "2010-01" : "2020-01", content: index === 99 ? "经".repeat(7000) : "完整内容" })) };
  const sanitized = shared.sanitizeResume(full, true), fields = shared.attachmentReviewFields(sanitized), merged = shared.mergeAttachmentReview(sanitized, { ...sanitized, gender: "female" }, fields, [], true);
  for (const updating of [false, true]) { const saved = shared.candidateWriteResume(merged, updating, true); assert.equal(saved.birth_ym || "", ""); assert.equal(saved.basic_info, "出生年份：1990\n" + full.basic_info); assert.equal(saved.personal_description.length, 7000); assert.equal(saved.ai_summary.length, 13000); assert.equal(saved.work_experiences.length, 100); assert.equal(saved.work_experiences[99].content.length, 7000); }
  const query = shared.duplicateInput(merged); assert.equal(query.work_experiences.length, 1); assert.equal(query.work_experiences[0].company_name, "合成公司99"); assert.equal(JSON.stringify(query).includes("完整内容"), false);
  assert.equal(shared.sanitizeResume(full).work_experiences.length, 40, "普通四平台保留原有上限");
});

test("附件图片只从唯一附件容器读取，排除明确装饰、隐藏、小图、横图及整页预览", () => {
  const href = "https://www.zhipin.com/web/chat/index", photo = imageNode("/synthetic-photo.png"), scene = pdfFixture([photo, photo, imageNode("/company-logo.png"), imageNode("/picture.png", { alt: "二维码" }), imageNode("/hidden.png", { hidden: true }), imageNode("/small.png", { width: 32, height: 32 }), imageNode("/landscape.png", { width: 500, height: 120 }), imageNode("/page.png", { width: 750, height: 1000, displayWidth: 750, displayHeight: 1000 })]);
  const originalQuery = scene.doc.querySelectorAll; scene.doc.querySelectorAll = (selector) => selector === "img, image" ? [imageNode("/wrong-chat-photo.png")] : originalQuery(selector);
  assert.deepEqual(reader.extract(scene.doc, href).avatar_candidates, [{ source_url: "https://www.zhipin.com/synthetic-photo.png", width: 120, height: 160 }]);
});

test("SVG image仅保留显式图片地址，未知自然尺寸交后台核实，未核验CDN不扩大权限", () => {
  const scene = pdfFixture([imageNode("data:image/png;base64,AAAA", { svg: true }), imageNode("https://img.bosszhipin.com.evil.test/unverified.png"), imageNode("https://www.zhipin.com.evil.test/photo.png")]);
  assert.deepEqual(reader.avatarCandidates(scene.viewer, "https://www.zhipin.com/web/chat/index"), [{ source_url: "data:image/png;base64,AAAA", width: 0, height: 0 }]);
  assert.deepEqual(reader.avatarCandidates(pdfFixture(Array.from({ length: 7 }, (_, index) => imageNode(`/photo-${index}.png`))).viewer, "https://www.zhipin.com/web/chat/index"), []);
});

test("双canvas没有独立图片时头像留空，多份附件不猜测，未知总页数的缺页显示短提醒", () => {
  const href = "https://www.zhipin.com/web/chat/index", scene = pdfFixture(), result = reader.extract(scene.doc, href);
  assert.deepEqual(result.avatar_candidates, []); assert.equal(result.attachment.blocked, false); assert.equal(result.attachment.warning, "");
  const missing = reader.extract(pdfFixture([], [true, false]).doc, href); assert.equal(missing.attachment.blocked, true); assert.match(missing.attachment.warning, /还没加载完/);
  const known = reader.extract(pdfFixture([], [true, false], 2).doc, href); assert.match(known.attachment.warning, /1\/2/);
  const multiple = reader.extract({ querySelectorAll: (selector) => selector === ".bzl-office-pdf" ? [scene.viewer, scene.viewer] : [] }, href); assert.equal(multiple.attachment.blocked, true); assert.equal(multiple.avatar_candidates, undefined);
});

test("头像指纹隔离同文字不同照片，人工改号仍保持同一附件身份", () => {
  const resume = { platform_id: "boss", source_url: "https://www.zhipin.com/web/chat/index", attachment: { text_hash: "a".repeat(64), avatar_hash: "b".repeat(64) } };
  assert.notEqual(shared.resumeIdentity(resume), shared.resumeIdentity({ ...resume, attachment: { ...resume.attachment, avatar_hash: "c".repeat(64) } }));
  assert.equal(shared.resumeIdentity(resume), shared.resumeIdentity({ ...resume, phone: "13800000991" }));
});
