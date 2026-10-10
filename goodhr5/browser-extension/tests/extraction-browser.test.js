/* 文件作用：在真实 Chrome 的合成 DOM 中验证参考平台字段与隐藏节点隔离，不连接招聘网站。 */
"use strict";
const test = require("node:test"), assert = require("node:assert/strict"), path = require("node:path");
const ROOT = path.resolve(__dirname, ".."), { chromium } = require(path.join(ROOT, "..", "cloud", "frontend-next", "node_modules", "@playwright", "test"));

/** readScene 在本地合成页面加载只读采集器，使用平台 URL 仅判断适配器，不发起外网请求。 */
async function readScene(page, html, platformURL) {
  await page.setContent(html);
  await page.addScriptTag({ path: path.join(ROOT, "src", "shared.js") });
  await page.addScriptTag({ path: path.join(ROOT, "src", "extraction.js") });
  return page.evaluate((url) => GoodHRExtraction.extractCurrentCandidate(document, url), platformURL);
}

test("真实DOM保留分隔资料与智联多期望，排除隐藏子节点并按51job模块分类", async () => {
  const browser = await chromium.launch({ channel: "chrome", headless: true });
  try {
    const page = await browser.newPage(); await page.route(/^https?:/, (route) => route.abort());
    const hunter = await readScene(page, `<div id="resume-detail">
      <div id="resume-detail-basic-info"><b class="name" title="合成猎头候选人">合成…</b><div class="basic-cont">
        <div class="sep-info">女<i>|</i>35岁<i>|</i>工作12年<i>|</i>硕士<i>|</i><span>上海</span><i>|</i><span style="display:none">隐藏城市</span></div>
        <div class="sep-info">合成当前岗位<i>|</i>合成公司</div></div><div class="connect-box"><dd>手机：13800000101</dd><dd>fixture@example.test</dd><dd hidden>13800000102</dd></div></div>
      <div id="resume-detail-job-exp-info"><div class="tabs"><span class="title">合成期望岗位</span></div><span class="dqname-wrap">杭州</span><span class="salary">15.5-20k×14薪</span></div></div>`, "https://h.liepin.com/resume/showresumedetail");
    assert.equal(hunter.candidate_name, "合成猎头候选人"); assert.equal(hunter.gender, "female"); assert.equal(hunter.work_region, "上海"); assert.equal(hunter.current_position, "合成当前岗位"); assert.equal(hunter.phone, "13800000101");
    assert.equal(JSON.stringify(hunter).includes("隐藏城市"), false);

    const zhaopin = await readScene(page, `<div class="new-resume-detail--inner"><div class="resume-basic-new">
      <span class="resume-basic-new__name">合成智联候选人</span><span class="resume-basic-new__meta-item">31岁（1995年6月）</span><span class="resume-basic-new__meta-item">8年</span><span class="resume-basic-new__meta-item">现居【上海】</span><span class="resume-basic-new__contacts--phone">13800000103</span></div>
      <div class="new-resume-purposes__item"><span class="new-resume-purposes__item-type" title="合成开发工程师">合成…</span><span class="new-resume-purposes__item-city">[上海]</span><span class="new-resume-purposes__item-salary">15-20k</span></div>
      <div class="new-resume-purposes__item"><span class="new-resume-purposes__item-type">合成架构师</span><span class="new-resume-purposes__item-city">杭州</span></div>
      <div class="new-professional-skills__item"><span class="new-professional-skills__item-name">合成技能</span><span class="new-professional-skills__item-tag">熟练</span></div>
      <div class="new-education-experiences__item"><span class="new-education-experiences__item-name">合成学院</span><span class="new-education-experiences__item-time">2010.09 - 2014.06</span><div class="new-education-experiences__item-bottom"><span>合成专业</span><span>本科</span></div></div></div>`, "https://rd6.zhaopin.com/app");
    assert.equal(zhaopin.expected_position, "合成开发工程师、合成架构师"); assert.equal(zhaopin.expected_region, "上海、杭州"); assert.equal(zhaopin.work_years, "8"); assert.deepEqual(zhaopin.tags, ["合成技能-熟练"]); assert.equal(zhaopin.educations[0].start_ym, "2010-09");

    const job = await readScene(page, `<div class="con-container"><div class="resume"><div class="baseinfo-container"><span class="name">合成无忧候选人</span><span class="center-top-item">女</span><span class="center-top-item">29岁</span><span class="detail-item">5年</span><span class="detail-item">现居·深圳</span></div>
      <div class="IM-base-module"><h3 class="module-title-text">工作经历</h3><div class="IM-info-desc">不能当个人简介的合成职责</div></div>
      <div class="IM-base-module"><h3 class="module-title-text">自我评价</h3><div class="IM-info-desc">合成个人简介</div></div>
      <div class="school-container-item"><span class="detail-name">合成学院一</span><span class="detail-time">2015/09 - 2019/06</span><span>本科</span></div><div class="edu-container-item"><span class="school-name">合成学院二</span><span class="item-time">2019/09 - 2021/06</span><span>硕士</span></div></div></div>
      <div class="eh_take_phone_popper"><div class="eh_im_take_phone_wrap"><span class="action">13800000104</span></div></div>`, "https://ehire.51job.com/Revision/Resume");
    assert.equal(job.personal_description, "合成个人简介"); assert.equal(job.work_region, "深圳"); assert.equal(job.phone, "13800000104"); assert.equal(job.educations.length, 2);
  } finally { await browser.close(); }
});

test("猎聘联系方式逐行提取带复制文案的号码，拒绝掩码、隐藏、多号和整页业务编号", async () => {
  const browser = await chromium.launch({ channel: "chrome", headless: true });
  try {
    const page = await browser.newPage(); await page.route(/^https?:/, (route) => route.abort());
    /** hunterPhone 在合成猎聘详情中读取单个联系方式，不访问真实网站或保存真实候选人。 */
    async function hunterPhone(contact) {
      const result = await readScene(page, `<div>业务编号13800000999 工作中提及13800000998</div><div id="resume-detail"><div id="resume-detail-basic-info"><b class="name">合成电话候选人</b><div class="connect-box">${contact}</div></div><div id="resume-detail-work-info"><p>职责中的13800000997不作为联系方式</p></div></div>`, "https://h.liepin.com/resume/showresumedetail");
      return result.phone;
    }
    assert.equal(await hunterPhone('<dd><span>手机号：</span><span>13800000131</span><button>复制</button></dd><dd>fixture2020@example.test</dd><dd hidden>13800000132</dd>'), "13800000131");
    assert.equal(await hunterPhone('<dd>+86 138-0000-0131 复制号码</dd><dd>2020年工作经历</dd>'), "13800000131");
    assert.equal(await hunterPhone('<dd><a href="tel:+8613800000131">拨打</a></dd>'), "13800000131");
    assert.equal(await hunterPhone('<dd><a href="tel:13800000131">138****0131</a></dd>'), "");
    assert.equal(await hunterPhone('<dd>13800000131 复制</dd><dd>13900000132</dd>'), "");
    assert.equal(await hunterPhone('<dd>13800000131@example.test</dd><dd>1995年 2020年</dd><dd>编号13800000131</dd>'), "");
    assert.equal(await hunterPhone('<dd style="display:none">13800000131</dd><dd>未显示手机号</dd>'), "");
  } finally { await browser.close(); }
});
