/** 文件作用说明：用全拦截的合成接口验证自动回复配置、全局启停、刷新恢复与桌面手机布局。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import type { ReplyPhrase, ReplyPhraseOverride } from "../../lib/auto-reply-phrases";
import type { PositionReplyCondition } from "../../lib/auto-reply";
import type { ReplyQuestion } from "../../lib/auto-reply-inbox";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const output = resolve(process.env.UI_TEST_SCREENSHOT_DIR || "output/auto-reply-20261030");
test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "auto-reply-synthetic-token" }] }] } });

/** fixture 在内存中模拟岗位配置和本地任务，未列入的写操作不会触达任何服务。 */
async function fixture(page: Page, agentOnline = true) {
  const base = await mockUniformAdmin(page);
  await page.addLocatorHandler(page.getByRole("dialog", {name:"请先安装本地程序"}), async () => { await page.keyboard.press("Escape"); });
  // 本组验证已连接设备的业务界面；端口探测本身另有单测，避免450ms探测被模拟路由调度耗时干扰。
  await page.addInitScript(({port,online}) => {
    if(online) localStorage.setItem("goodhr5_local_agent_detect_cache",JSON.stringify({baseURL:`http://127.0.0.1:${port}`,checkedAt:Date.now()}));
    else localStorage.removeItem("goodhr5_local_agent_detect_cache");
  }, {port:process.env.UI_UNIFORM_AGENT_PORT || "43129",online:agentOnline});
  const reports = { blocked: 0, resumeBlocked: 0, retries: 0, failure: "" };
  const defaults: ReplyPhrase[] = [
    { id: "receive_resume", title: "收到简历", category: "简历", scene: "候选人刚刚发来简历", content: "简历收到了，谢谢你发过来。" },
    { id: "receive_phone", title: "收到手机号", category: "联系方式", scene: "收到候选人的号码", content: "号码收到了，谢谢。" },
  ];
  const state = { running: false, starts: 0, stops: 0, config: { position_id: "uniform-position-1", company_profile_id: "company-one", enabled: false, position_description: "合成岗位说明", resume_request_message: "你好，方便发一份简历吗？", conditions: [] as PositionReplyCondition[], condition_options: ["能够到成都办公"], phrases_version: "view-version-1", default_phrases: defaults, phrase_overrides: [] as ReplyPhraseOverride[], poll_interval_seconds: 5, max_threads_per_checkpoint: 3, version: 1 }, profiles: [{ id: "company-one", name: "合成公司", address: "合成测试地址", contact: "", overview: "合成公司介绍", extra_info: "", updated_at: "2026-10-30T08:00:00Z" }] };
  await page.route(/\/api\/(?:auto-reply\/|v1\/local\/auto-reply\/)/, async (route) => {
    const request = route.request(); const path = new URL(request.url()).pathname; const method = request.method();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    let data: unknown;
    if (path.endsWith("/auto-reply/start")) { expect(request.postDataJSON().platform_account_id).toBe("合成招聘账号"); state.starts++; state.running = true; data = { ok: true, data: { status: "running", task_id: "synthetic-auto-reply" } }; }
    else if (path === "/api/auto-reply/questions/summary") data = { pending_count: 0, positions: [] };
    else if (path === "/api/auto-reply/questions") data = { questions: [], next_cursor: "", pending_count: 0 };
    else if (path.endsWith("/questions") && method === "GET") data = { questions: [] };
    else if (path.endsWith("/auto-reply/stop")) { state.stops++; state.running = false; data = { ok: true, data: { status: "stopped" } }; }
    else if (path.endsWith("/auto-reply/status")) data = { ok: true, data: { status: state.running ? "running" : reports.failure ? "failed" : "stopped", current_step: reports.failure ? "finished" : "合成任务状态", error_code: reports.failure ? "TASK_FLOW_FAILED" : "", error_message: reports.failure, blocked_report_count: reports.blocked, blocked_resume_count: reports.resumeBlocked } };
    else if (path.endsWith("/auto-reply/retry-reports")) {
      expect(request.postDataJSON().token).toBe("auto-reply-synthetic-token");
      const count = reports.blocked, resumeCount = reports.resumeBlocked; reports.retries++; reports.blocked = 0; reports.resumeBlocked = 0;
      data = { ok: true, data: { retried_count: count + resumeCount, report_count: count, resume_count: resumeCount } };
    }
    else if (/\/positions\/[^/]+\/status$/.test(path)) data = { configured_enabled: state.config.enabled, enabled: state.config.enabled, version: 1, subscription: { allow_auto_reply: true } };
    else if (path.endsWith("/config")) {
      if (method === "PUT") state.config = { ...state.config, ...request.postDataJSON() };
      data = { config: state.config };
    } else if (path.endsWith("/company-profiles")) data = { company_profiles: state.profiles };
    else if (path.endsWith("/company-profiles/company-one") && method === "PUT") data = { company_profile: { ...state.profiles[0], ...request.postDataJSON() } };
    else throw new Error(`自动回复测试遗漏接口 ${method} ${path}`);
    return route.fulfill({ headers, json: data });
  });
  return { state, base, reports };
}

/** openReplyWorkspace 只打开统一面板，不能因为查看待办而顺带启动浏览器任务。 */
async function openReplyWorkspace(page: Page) {
  await page.getByTestId("auto-reply-global").click();
  const dialog = page.getByRole("dialog", { name: "自动回复", exact: true });
  await expect(dialog).toBeVisible();
  return dialog;
}

/** inboxFixture 模拟服务器汇总、当前页原话和版本化答复，不访问真实招聘账号。 */
async function inboxFixture(page: Page, agentOnline = true) {
  const base = await fixture(page, agentOnline);
  const rows: ReplyQuestion[] = Array.from({length:13},(_,i)=>({ id:`inbox-${i+1}`,position_id:i===12?"closed-position":"uniform-position-1",question:`可以确认事项${i+1}吗？`,answer:"",status:"pending",publish_target:"",error:"",version:1,source_messages:[{message_id:`source-${i+1}`,text:`候选人的合成原话${i+1}`,candidate_name:`合成候选人${i+1}`}] }));
  const inbox = { rows, summaryFailure:false, invalidCursor:false, invalidateOnReply:false, replies:0, reads:0 };
  const headers={"access-control-allow-origin":"*","access-control-allow-methods":"*","access-control-allow-headers":"*"};
  await page.route(/\/api\/auto-reply\/(?:questions(?:\/summary|\?|$)|positions\/[^/]+\/questions\/reply$)/,async route=>{
    const req=route.request(),url=new URL(req.url());
    if(req.method()==="OPTIONS")return route.fulfill({status:204,headers});
    const pending=inbox.rows.filter(row=>row.status==="pending"||row.status==="failed");
    if(url.pathname.endsWith("/summary")){
      if(inbox.summaryFailure)return route.fulfill({status:503,headers,json:{message:"汇总读取测试失败"}});
      return route.fulfill({headers,json:{pending_count:pending.length,positions:[{position_id:"uniform-position-1",position_name:"合成待确认岗位",platform_id:"boss",is_open:true,pending_count:pending.filter(row=>row.position_id==="uniform-position-1").length},{position_id:"closed-position",position_name:"已关闭的合成岗位",platform_id:"liepin",is_open:false,pending_count:pending.filter(row=>row.position_id==="closed-position").length}]}});
    }
    if(url.pathname.endsWith("/reply")){
      const body=req.postDataJSON();const row=inbox.rows.find(item=>item.id===body.id);expect(row).toBeTruthy();expect(body.version).toBe(row!.version);
      row!.answer=body.answer;row!.status="resolved";row!.publish_target="position";row!.version++;inbox.replies++;
      if(inbox.invalidateOnReply){inbox.invalidCursor=true;inbox.invalidateOnReply=false;}
      return route.fulfill({headers,json:{question:row}});
    }
    inbox.reads++;
    if(inbox.invalidCursor&&url.searchParams.get("cursor"))return route.fulfill({status:400,headers,json:{code:"AUTO_REPLY_VALIDATION_FAILED",message:"列表刚有更新，请重新打开"}});
    const filtered=(url.searchParams.get("view")==="all"?inbox.rows:pending).filter(row=>row.position_id===url.searchParams.get("position_id"));
    const offset=url.searchParams.get("cursor")?10:0;
    return route.fulfill({headers,json:{questions:filtered.slice(offset,offset+10),next_cursor:filtered.length>offset+10?"page-two-cursor":"",pending_count:pending.length}});
  });
  return { ...base,inbox };
}

for (const width of [1440,390]) test(`待确认统一入口 ${width}px 标题原话、分页草稿与游标失效恢复`,async({page})=>{
  const {inbox,state,base}=await inboxFixture(page);await page.setViewportSize({width,height:960});await page.goto("/admin/positions");
  await expect(page.getByTitle("13 项待确认补充",{exact:true})).toBeVisible();
  const badgeBounds=await page.getByTitle("13 项待确认补充",{exact:true}).boundingBox();
  const toolbarBounds=await page.getByTestId("positions-toolbar").boundingBox();
  expect(badgeBounds!.y).toBeGreaterThanOrEqual(toolbarBounds!.y);
  expect(badgeBounds!.y+badgeBounds!.height).toBeLessThanOrEqual(toolbarBounds!.y+toolbarBounds!.height);
  expect((await page.getByTestId("auto-reply-global").boundingBox())!.height).toBe(32);
  mkdirSync(output,{recursive:true});await page.screenshot({path:resolve(output,`inbox-entry-${width}.png`),fullPage:true,animations:"disabled"});
  const dialog=await openReplyWorkspace(page);
  await expect(dialog.getByRole("columnheader",{name:"候选人原话",exact:true})).toHaveCount(0);
  const first=dialog.getByRole("textbox",{name:"回复可以确认事项1吗？",exact:true});await first.fill("第一页仍未提交的答复");
  await dialog.getByRole("button",{name:/可以确认事项1吗？/}).click();
  await expect(dialog.getByText("候选人的合成原话1",{exact:true})).toBeVisible();
  await dialog.getByRole("button",{name:"下一页",exact:true}).click();
  const second=dialog.getByRole("textbox",{name:"回复可以确认事项11吗？",exact:true});await second.fill("第二页确认后的答复");
  inbox.invalidateOnReply=true;await dialog.getByRole("button",{name:"保存回复",exact:true}).first().click();
  await expect(dialog.getByText("列表刚有更新，已回到第一页，填写的草稿还在。",{exact:true})).toBeVisible();
  await expect(first).toHaveValue("第一页仍未提交的答复");
  await expect(page.getByTitle("12 项待确认补充",{exact:true})).toBeVisible();
  const select=dialog.getByRole("combobox",{name:"待确认所属岗位",exact:true});await select.click();await page.locator(".ant-select-item-option").filter({hasText:/已关闭的合成岗位/}).click();
  await expect(dialog.getByRole("textbox",{name:"回复可以确认事项13吗？",exact:true})).toBeVisible();
  await select.click();await page.locator(".ant-select-item-option").filter({hasText:/合成待确认岗位/}).click();await expect(first).toHaveValue("第一页仍未提交的答复");
  await dialog.getByText("全部记录",{exact:true}).click();
  await expect(first).toHaveValue("第一页仍未提交的答复");
  await dialog.getByRole("button",{name:/可以确认事项1吗？/}).click();
  await expect(dialog.getByText("候选人的合成原话1",{exact:true})).toBeVisible();
  await dialog.getByText("消息接待",{exact:true}).scrollIntoViewIfNeeded();
  await expect(page.getByText("已补入对应资料。这条已处理，可在全部记录里查看。",{exact:true})).toHaveCount(0);
  mkdirSync(output,{recursive:true});await page.screenshot({path:resolve(output,`inbox-${width}.png`),fullPage:true,animations:"disabled"});
  const bounds=await dialog.boundingBox();expect(bounds!.x+bounds!.width).toBeLessThanOrEqual(width);
  expect(state.starts).toBe(0);expect(inbox.replies).toBe(1);expect(base.unknown).toEqual([]);
});

test("待确认汇总失败显示未知，离线仍可回复，处理最后一题不跳到其他岗位",async({page})=>{
  const {inbox,state}=await inboxFixture(page,false);inbox.rows=inbox.rows.filter(row=>row.id==="inbox-1"||row.id==="inbox-13");inbox.summaryFailure=true;
  await page.route(/http:\/\/127\.0\.0\.1:43999\//,route=>route.abort());
  await page.goto("/admin/positions");await page.addLocatorHandler(page.getByRole("dialog",{name:"请先安装本地程序"}),async()=>{await page.keyboard.press("Escape");});
  const dialog=await openReplyWorkspace(page);await expect(dialog.getByText("数量暂未读到",{exact:true})).toBeVisible();await expect(dialog.getByText("0 项待处理",{exact:true})).toHaveCount(0);
  await expect(dialog.getByRole("button",{name:"开始自动回复",exact:true})).toBeDisabled();
  inbox.summaryFailure=false;await dialog.getByRole("button",{name:"重试",exact:true}).click();
  await dialog.getByRole("textbox",{name:"回复可以确认事项1吗？",exact:true}).fill("已核实的信息");await dialog.getByRole("button",{name:"保存回复",exact:true}).click();
  await expect(dialog.getByText("这个岗位暂时没有需要补充的问题。",{exact:true})).toBeVisible();
  await expect(dialog.getByRole("textbox",{name:"回复可以确认事项13吗？",exact:true})).toHaveCount(0);
  await expect(dialog.getByText("合成待确认岗位",{exact:true})).toBeVisible();
  await expect(page.getByTitle("1 项待确认补充",{exact:true})).toBeVisible();expect(state.starts).toBe(0);expect(inbox.replies).toBe(1);
});

test("保存答复响应省略来源时，不把仍在读取的原话显示成已清理",async({page})=>{
  const {inbox}=await inboxFixture(page);inbox.rows=inbox.rows.slice(0,1);
  let hold=false;let release=()=>{};const gate=new Promise<void>(resolve=>{release=resolve;});
  await page.route(/\/api\/auto-reply\/questions\?/,async route=>{if(hold)await gate;await route.fallback();});
  await page.route("**/api/auto-reply/positions/*/questions/reply",async route=>{
    const headers={"access-control-allow-origin":"*","access-control-allow-methods":"*","access-control-allow-headers":"*"};
    if(route.request().method()==="OPTIONS")return route.fulfill({status:204,headers});
    const row=inbox.rows[0];row.answer=route.request().postDataJSON().answer;row.status="resolved";row.publish_target="position";row.version++;hold=true;
    const {source_messages:omitted,...saved}=row;void omitted;await route.fulfill({headers,json:{question:saved}});
  });
  try{
    await page.goto("/admin/positions");const dialog=await openReplyWorkspace(page);await dialog.getByText("全部记录",{exact:true}).click();
    await dialog.getByRole("button",{name:/可以确认事项1吗？/}).click();await dialog.getByRole("textbox",{name:"回复可以确认事项1吗？",exact:true}).fill("已确认答复");
    await dialog.getByRole("button",{name:"保存回复",exact:true}).click();await expect(dialog.getByRole("table").getByText("已补充",{exact:true})).toBeVisible();
    await expect(dialog.getByText("候选人的合成原话1",{exact:true})).toBeVisible();await expect(dialog.getByText("原话已清理",{exact:true})).toHaveCount(0);
  }finally{release();}
});

test("答复草稿固定首次版本，每次并发更新都需核对，自己的归档失败仍可直接重试",async({page})=>{
  test.setTimeout(60000);
  const {inbox}=await inboxFixture(page);inbox.rows=inbox.rows.slice(0,1);
  const question=inbox.rows[0];question.answer="上次中断时保存的原答复";question.status="failed";
  let firstAttempt=true;
  await page.route("**/api/auto-reply/positions/*/questions/reply",async route=>{
    if(route.request().method()==="OPTIONS"||!firstAttempt)return route.fallback();
    const body=route.request().postDataJSON();expect(body.version).toBe(3);firstAttempt=false;
    question.answer=body.answer;question.version++;question.status="failed";question.error="整理没完成，可以重新保存。";inbox.replies++;
    await route.fulfill({headers:{"access-control-allow-origin":"*"},json:{question}});
  });
  await page.goto("/admin/positions");const dialog=await openReplyWorkspace(page);
  const row=dialog.getByRole("table").getByRole("row").filter({hasText:"可以确认事项1吗？"});
  const input=row.getByRole("textbox",{name:"回复可以确认事项1吗？",exact:true});
  const save=row.getByRole("button",{name:"保存回复",exact:true});
  await expect(input).toHaveValue("上次中断时保存的原答复");await expect(save).toBeEnabled();
  await input.fill("本页仍未提交的人工草稿");
  question.version=2;question.answer="另一个页面刚保存的答复";
  await expect(row.getByText("最新已保存答复：另一个页面刚保存的答复",{exact:true})).toBeVisible({timeout:15000});
  await expect(input).toHaveValue("本页仍未提交的人工草稿");await expect(save).toBeDisabled();expect(inbox.replies).toBe(0);
  await row.getByRole("button",{name:"已核对，继续用我的草稿",exact:true}).click();await expect(save).toBeEnabled();
  question.version=3;question.answer="核对后另一个页面再次保存的答复";
  await expect(row.getByText("最新已保存答复：核对后另一个页面再次保存的答复",{exact:true})).toBeVisible({timeout:15000});
  await expect(save).toBeDisabled();await expect(input).toHaveValue("本页仍未提交的人工草稿");expect(inbox.replies).toBe(0);
  await page.setViewportSize({width:390,height:960});
  const reviewBounds=await dialog.getByRole("button",{name:"已核对，继续用我的草稿",exact:true}).boundingBox();const dialogBounds=await dialog.boundingBox();
  expect(reviewBounds!.x+reviewBounds!.width).toBeLessThanOrEqual(dialogBounds!.x+dialogBounds!.width);expect(reviewBounds!.height).toBe(32);
  mkdirSync(output,{recursive:true});await page.screenshot({path:resolve(output,"inbox-draft-conflict-390.png"),fullPage:true,animations:"disabled"});
  await page.setViewportSize({width:1440,height:960});
  await row.getByRole("button",{name:"已核对，继续用我的草稿",exact:true}).click();await save.click();
  await expect(row.getByText("整理没完成，可以重新保存。",{exact:true})).toBeVisible();
  await expect(save).toBeEnabled();await expect(row.getByRole("button",{name:"已核对，继续用我的草稿",exact:true})).toHaveCount(0);
  await save.click();await expect(dialog.getByText("这个岗位暂时没有需要补充的问题。",{exact:true})).toBeVisible();
  expect(inbox.replies).toBe(2);expect(question.answer).toBe("本页仍未提交的人工草稿");
});

test("慢轮询不重叠，读取失败重试后保留岗位待确认问题的草稿", async ({ page }) => {
  test.setTimeout(60000);
  await fixture(page);
  let reads = 0;
  let holdNext = false;
  let readAtHold = 0;
  let release = () => {};
  let releaseFirst = () => {};
  // 开发模式会试挂载组件，所有初始读取共用闸门，避免已取消的首请求抢先放行。
  const firstResponse = new Promise<void>(resolve => { releaseFirst = resolve; });
  const question = { id:"slow-question", question:"能否远程办公？", source_messages:[], status:"pending", version: 1 };
  await page.route("**/api/auto-reply/questions/summary", async route => route.fulfill({ headers: { "access-control-allow-origin":"*" }, json: { pending_count: 1, positions:[{position_id:"uniform-position-1",position_name:"合成岗位",platform_id:"boss",is_open:true,pending_count:1}] } }));
  await page.route(/\/api\/auto-reply\/questions(?:\?|$)/, async route => {
    const headers = { "access-control-allow-origin":"*", "access-control-allow-headers":"*" };
    if (route.request().method() === "OPTIONS") return route.fulfill({status:204,headers});
    reads++;
    await firstResponse;
    if (holdNext) {
      holdNext = false;
      readAtHold = reads;
      await new Promise<void>(resolve => { release=resolve; });
      return route.fulfill({status:503,headers,json:{message:"问题列表暂时没读出来"}});
    }
    await route.fulfill({headers,json:{questions:[question],next_cursor:"",pending_count:1}});
  });
  await page.goto("/admin/positions");
  await openReplyWorkspace(page);
  await expect(page.getByRole("status").filter({hasText:"正在读取待确认问题"})).toBeVisible();
  await expect(page.getByText("暂时没有需要你补充的问题。")).toHaveCount(0);
  await expect.poll(() => reads).toBeGreaterThan(0);
  releaseFirst();
  const questionInput=page.getByRole("textbox",{name:"回复能否远程办公？"});
  await questionInput.fill("HR正在填写的答案");
  await page.getByRole("button", {name:/能否远程办公？/}).click();
  await expect(page.getByText("原话已清理", {exact:true})).toBeVisible();
  await expect(page.getByText("0 条原话", {exact:true})).toHaveCount(0);
  holdNext = true;
  await expect.poll(() => readAtHold,{timeout:15000}).toBeGreaterThan(0);
  // 让已挂起的第二次读取横跨下一轮 10 秒的真实轮询。
  await new Promise(resolve => setTimeout(resolve,11000));
  expect(reads).toBe(readAtHold);
  await expect(questionInput).toHaveValue("HR正在填写的答案");
  release();
  const retry=page.getByRole("button",{name:"重试",exact:true});
  await expect(retry).toBeVisible();
  await expect(questionInput).toHaveValue("HR正在填写的答案");
  await retry.click();
  await expect(questionInput).toHaveValue("HR正在填写的答案");
  expect(reads).toBe(readAtHold+1);
});

test("条件建议失败可以重试，旧请求晚到时保留新草稿", async ({ page }) => {
  const { state } = await fixture(page);
  let calls = 0;
  let release = () => {};
  await page.route("**/api/positions/*/auto-reply/condition-suggestions", async route => {
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    calls++;
    if (calls === 1) return route.fulfill({ headers, status: 503, json: { message: "模型暂时繁忙" } });
    await new Promise<void>(resolve => { release = resolve; });
    await route.fulfill({ headers, json: { suggestions: [{ type: "required", content: "上一份岗位说明里的条件" }] } });
  });
  await page.goto("/admin/positions");
  const open = page.locator('.ant-card[data-position-id="uniform-position-1"]').getByRole("button", { name: /自动回复设置$/ });
  await open.click();
  const dialog = page.getByRole("dialog", { name: /^自动回复 · / });
  const generate = dialog.getByRole("button", { name: "根据 AI 生成" });
  await generate.click();
  await expect(dialog.getByText("模型暂时繁忙", { exact: true })).toBeVisible();
  await generate.click();
  await expect.poll(() => calls).toBe(2);
  await dialog.getByRole("textbox", { name: "自动回复岗位描述" }).fill("下一份岗位的资料");
  await expect(generate).toHaveAttribute("aria-busy", "false");
  release();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await open.click();
  await dialog.getByRole("button", { name: "添加条件" }).click();
  await expect(dialog.getByRole("checkbox", { name: "上一份岗位说明里的条件" })).toHaveCount(0);
  await expect(dialog.getByRole("textbox", { name: "自动回复岗位描述" })).toHaveValue(state.config.position_description);
  expect(state.config.conditions).toEqual([]);
});

for (const width of [1366, 390]) {
  test(`自动回复 ${width}px 勾选AI建议、管理岗位话术并保持公司绑定`, async ({ page }) => {
    const { state } = await fixture(page);
    const secondCompany = { ...state.profiles[0], id: "company-two", name: "第二家合成公司" };
    state.profiles.push(secondCompany);
    state.config.company_profile_id = "";
    state.config.conditions.push({ id: "old", type: "required", content: "有数据分析经验", enabled: true, sort_order: 0 });
    let release = () => {};
    let calls = 0;
    await page.route("**/api/positions/*/auto-reply/condition-suggestions", async route => {
      const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
      if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
      calls++;
      expect(route.request().postDataJSON().position_description).toBe("合成岗位说明");
      await new Promise<void>(resolve => { release = resolve; });
      await route.fulfill({ json: { suggestions: [{ content: "可以接受排班", type: "confirm" }, { content: "有数据分析经验", type: "required" }] }, headers });
    });
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/admin/positions");
    await page.locator('.ant-card[data-position-id="uniform-position-1"]').getByRole("button", { name: /自动回复设置$/ }).click();
    const dialog = page.getByRole("dialog", { name: /^自动回复 · / });
    await expect(dialog.getByRole("radio", { name: "合成公司", exact: true })).not.toBeChecked();
    await expect(dialog.getByText("简历收到了，谢谢你发过来。")).toBeHidden();
    await dialog.getByRole("radio", { name: secondCompany.name }).check();
    mkdirSync(output, { recursive: true });
    await page.screenshot({ path: resolve(output, `settings-${width}.png`), animations: "disabled" });
    const generate = dialog.getByRole("button", { name: "根据 AI 生成" });
    const add = dialog.getByRole("button", { name: "添加条件" });
    const addBox = await add.boundingBox(); const aiBox = await generate.boundingBox();
    expect(aiBox!.x).toBeGreaterThan(addBox!.x);
    await generate.click();
    await expect(generate).toHaveAttribute("aria-busy", "true");
    await expect.poll(() => calls).toBe(1);
    release();
    const offered = dialog.getByRole("checkbox", { name: "可以接受排班" });
    await expect(offered).not.toBeChecked();
    await expect(generate).toHaveAttribute("aria-busy", "false");
    await offered.check();
    await add.click();
    await dialog.locator("summary").filter({ hasText: "沟通话术" }).click();
    await dialog.getByRole("button", { name: "编辑话术：收到简历" }).click();
    const editor = page.getByRole("dialog", { name: "编辑话术", exact: true });
    await editor.getByLabel("回复内容").fill("收到简历，谢谢您。");
    await editor.getByRole("button", { name: "确定", exact: true }).click();
    await expect(dialog.getByText("收到简历，谢谢您。", { exact: true })).toBeVisible();
    await dialog.getByRole("button", { name: "移除话术：收到手机号" }).click();
    await dialog.locator("summary").filter({ hasText: "已移除的默认话术" }).click();
    await dialog.getByRole("button", { name: "恢复话术：收到手机号" }).click();
    await expect(dialog.getByText("号码收到了，谢谢。", { exact: true })).toBeVisible();
    await dialog.getByRole("button", { name: "新增话术" }).click();
    const newPhrase = page.getByRole("dialog", { name: "新增话术", exact: true });
    await newPhrase.getByLabel("标题").fill("收到作品");
    await newPhrase.getByLabel("分类").fill("作品资料");
    await newPhrase.getByLabel("使用场景").fill("候选人刚发来作品集");
    await newPhrase.getByLabel("回复内容").fill("作品集收到了，谢谢。");
    await newPhrase.getByRole("button", { name: "确定", exact: true }).click();
    await page.screenshot({ path: resolve(output, `phrases-${width}.png`), animations: "disabled" });
    await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
    await expect(dialog).toBeHidden();
    expect(state.config.company_profile_id).toBe(secondCompany.id);
    expect(state.config.conditions).toHaveLength(2);
    expect(state.config.conditions[0]).toMatchObject({ id: "old", type: "required", enabled: true });
    expect(state.config.conditions[1]).toMatchObject({ content: "可以接受排班", enabled: true });
    expect(state.config.phrase_overrides).toHaveLength(2);
    expect(state.config.phrase_overrides).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: "receive_resume", content: "收到简历，谢谢您。" }),
      expect.objectContaining({ id: expect.stringMatching(/^job_/), title: "收到作品" }),
    ]));
  });

  test(`自动回复失败 ${width}px 持续显示原因并可重新开始`, async ({ page }) => {
    const { reports, state } = await fixture(page);
    reports.failure = "当前平台还不能核对单会话稳定身份，本次限制任务没有启动";
    await page.setViewportSize({ width, height: 960 });
    await page.goto("/admin/positions");
    const alert = page.getByTestId("auto-reply-failure");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText(reports.failure);
    await expect(alert).not.toContainText("finished");
    await expect(alert).toContainText("处理后点击“开始自动回复”重试");
    await page.reload();
    await expect(alert).toBeVisible();
    const bounds = await alert.boundingBox();
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width);
    mkdirSync(output, { recursive: true });
    await page.screenshot({ path: resolve(output, `failure-${width}.png`), fullPage: true, animations: "disabled" });
    const workspace = await openReplyWorkspace(page);
    await workspace.getByRole("button", { name: "开始自动回复", exact: true }).click();
    const account = page.getByRole("dialog", { name: "确认当前招聘账号", exact: true });
    await account.getByRole("textbox", { name: "当前招聘账号", exact: true }).fill("合成招聘账号");
    await account.getByRole("button", { name: "确认账号并开始", exact: true }).click();
    await expect(page.getByRole("button", { name: "停止自动回复", exact: true })).toBeVisible();
    await expect(alert).not.toBeVisible();
    expect(state.starts).toBe(1);
  });

  test(`自动回复 ${width}px 岗位配置全局启停与刷新恢复`, async ({ page }) => {
    const { state, base } = await fixture(page);
    await page.setViewportSize({ width, height: 960 });
    await page.goto("/admin/positions");
    const card = page.locator('.ant-card[data-position-id="uniform-position-1"]');
    await card.getByRole("button", { name: /自动回复设置$/ }).click();
    const dialog = page.getByRole("dialog", { name: /^自动回复 · / });
    await expect(dialog.getByRole("textbox", { name: "自动回复岗位描述", exact: true })).toHaveValue("合成岗位说明");
    await dialog.getByRole("textbox", { name: "自动回复岗位描述", exact: true }).fill("合成新说明，绝不提交真实资料");
    await dialog.getByRole("switch", { name: "开启岗位自动回复", exact: true }).click();
    await expect(dialog.getByText("岗位里将“是否生成简历”选为“需要”，资料和推荐报告才会进简历库。")).toBeVisible();
    mkdirSync(output, { recursive: true });
    await page.screenshot({ path: resolve(output, `config-${width}.png`), fullPage: true, animations: "disabled" });
    await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(state.config.position_description).toContain("合成新说明"); expect(state.config.enabled).toBe(true);
    const workspace = await openReplyWorkspace(page);
    await workspace.getByRole("button", { name: "开始自动回复", exact: true }).click();
    const account = page.getByRole("dialog", { name: "确认当前招聘账号", exact: true });
    await expect(account.getByRole("button", { name: "确认账号并开始", exact: true })).toBeDisabled();
    await account.getByRole("textbox", { name: "当前招聘账号", exact: true }).fill("合成招聘账号");
    await expect(account).not.toHaveClass(/zoom.*(?:appear|enter)/);
    await expect(account).toHaveCSS("opacity", "1");
    await expect(page.getByText("自动回复配置保存好了，我会按这份内容老实回答。", { exact: true })).not.toBeVisible();
    await page.screenshot({ path: resolve(output, `account-${width}.png`), fullPage: true, animations: "disabled" });
    await account.getByRole("button", { name: "确认账号并开始", exact: true }).click();
    await expect(page.getByRole("button", { name: "停止自动回复", exact: true })).toBeVisible(); expect(state.starts).toBe(1);
    await page.reload();
    const restoredWorkspace = await openReplyWorkspace(page);
    await restoredWorkspace.getByRole("button", { name: "停止自动回复", exact: true }).click();
    await expect(page.getByRole("button", { name: "开始自动回复", exact: true })).toBeVisible(); expect(state.stops).toBe(1);
    await restoredWorkspace.getByRole("button", { name: "关闭", exact: true }).last().click();
    await page.screenshot({ path: resolve(output, `positions-${width}.png`), fullPage: true, animations: "disabled" });
    const create = await page.getByRole("button", { name: "新建岗位", exact: true }).boundingBox();
    expect(create!.x + create!.width).toBeLessThanOrEqual(width);
    expect(base.errors).toEqual([]); expect(base.writes).toEqual([]); expect(base.unknown).toEqual([]);
  });

  test(`失败待办 ${width}px 确认后才重试且不启动消息发送`, async ({ page }) => {
    const { reports, state } = await fixture(page);
    reports.blocked = 1; reports.resumeBlocked = 1;
    await page.setViewportSize({ width, height: 960 });
    await page.goto("/admin/positions");
    await openReplyWorkspace(page);
    await page.getByRole("button", { name: "重试待办（2）", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "重试待办", exact: true });
    await expect(dialog).toContainText("使用 AI 余额");
    await expect(dialog).toContainText("不会重发已回复的消息");
    expect(reports.retries).toBe(0);
    await expect(dialog).not.toHaveClass(/zoom.*(?:appear|enter)/);
    mkdirSync(output, { recursive: true });
    await page.screenshot({ path: resolve(output, `retry-report-${width}.png`), fullPage: true, animations: "disabled" });
    await dialog.getByRole("button", { name: "确认", exact: true }).click();
    await expect(page.getByRole("button", { name: "重试待办（2）", exact: true })).not.toBeVisible();
    expect(reports.retries).toBe(1); expect(state.starts).toBe(0);
  });
}

test("报告默认未公开，授权的聊天记录与公开报告分别读取", async ({ page }) => {
  const base = await mockUniformAdmin(page);
  const report = { public_id: "synthetic-public-report", share_enabled: false, generated_at: "2026-10-30T08:00:00Z", summary: "合成候选人符合已确认条件", report: { candidate: { name: "合成候选人1" }, position: { name: "合成岗位1" } } };
  const actions: string[] = [];
  await page.route(/\/api\/(?:candidates\/[^/]+\/auto-reply|auto-reply\/recommendations\/)/, async (route) => {
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname;
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (path.startsWith("/api/candidates/")) {
      if (url.searchParams.get("section") === "history") {
        return route.fulfill({ headers, json: { auto_reply: { threads: [], thread_id: "", messages: [], events: [], goals: [], next_message_cursor: "", next_event_cursor: "" } } });
      }
      expect(url.searchParams.get("section")).toBe("recommendations");
      return route.fulfill({ headers, json: { auto_reply: { recommendations: [report] } } });
    }
    actions.push(path); report.share_enabled = path.endsWith("/share");
    return route.fulfill({ headers, json: { ok: true, recommendation: report } });
  });
  await page.goto("/admin/resumes/detail?candidate_id=uniform-candidate-1");
  await page.getByRole("tab", { name: "自动回复", exact: true }).click();
  await expect(page.getByText("合成候选人符合已确认条件", { exact: true })).toBeVisible();
  expect(actions).toEqual([]);
  await page.getByRole("button", { name: "查看报告", exact: true }).click();
  const preview = page.getByRole("dialog", { name: "推荐报告 · 私有预览", exact: true });
  await expect(preview.getByRole("heading", { name: "合成候选人1", exact: true })).toBeVisible();
  await expect(preview.getByRole("heading", { name: "联系方式", exact: true })).toBeVisible();
  for (const width of [1366, 390]) {
    await page.setViewportSize({ width, height: 960 });
    await page.screenshot({ path: resolve(output, `private-report-${width}.png`), fullPage: true, animations: "disabled" });
    const box = await preview.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
  }
  expect(actions).toEqual([]);
  await preview.getByRole("button", { name: "关闭", exact: true }).last().click();
  await expect(preview).not.toBeVisible();
  await page.getByRole("button", { name: "开启分享", exact: true }).click();
  const sharing = page.getByRole("dialog", { name: "开启报告分享", exact: true });
  await expect(sharing).toContainText("完整简历、联系方式和推荐结论");
  await sharing.getByRole("button", { name: "确认", exact: true }).click();
  await expect(page.getByRole("button", { name: "撤销分享", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "撤销分享", exact: true }).click();
  await page.getByRole("dialog", { name: "撤销报告分享", exact: true }).getByRole("button", { name: "确认", exact: true }).click();
  await expect(page.getByRole("button", { name: "开启分享", exact: true })).toBeEnabled();
  expect(actions).toEqual(["/api/auto-reply/recommendations/synthetic-public-report/share", "/api/auto-reply/recommendations/synthetic-public-report/revoke"]);
  expect(base.calls.some((call) => /conversations|attachments|ai_records/.test(call.query))).toBe(false);
  expect(base.errors).toEqual([]); expect(base.writes).toEqual([]); expect(base.unknown).toEqual([]);
});


test("简历详情区分自动发送和未知招聘方，可分页追溯操作与目标证据", async ({ page }) => {
  await mockUniformAdmin(page);
  const threads = ["成都", "重庆"].map((city, index) => ({ id: "thread-" + index, platform: "liepin", position_name: city + "区域经理", candidate_name: "合成候选人", status: "active", updated_at: "2026-11-07T08:30:00+08:00" }));
  const messages = [
    { id: "ask", side: "recruiter", sender_type: "unknown", text: "有跨城工作计划吗？", sent_at: "2026-11-07T08:00:00+08:00" },
    { id: "answer", side: "candidate", sender_type: "human", text: "我可以在成都工作。", sent_at: "2026-11-07T08:01:00+08:00", attachments: [
      {id:"one",file_name:"合成简历.pdf",source_message_id:"answer"},
      {id:"wrong",file_name:"错误来源.pdf",source_message_id:"different-message"},
    ] },
    { id: "ai", side: "recruiter", sender_type: "ai", text: "收到，感谢确认。", sent_at: "2026-11-07T08:02:00+08:00" },
  ];
  const events = [{id: "sent", title: "发送平台消息", status: "sent", occurred_at: messages[2].sent_at, duration_ms: 1320, description: "平台已确认发出。"}];
  const goals = [{id:"place", title:"能在成都工作", status:"matched", evidence:"候选人明确表示接受成都办公。", evidence_message_ids:["answer"], updated_at:messages[1].sent_at}];
  await page.route(/\/api\/candidates\/[^/]+\/auto-reply/, async route => {
    const u = new URL(route.request().url());
    const headers = { "access-control-allow-origin":"*", "access-control-allow-headers":"*" };
    if (route.request().method() === "OPTIONS") return route.fulfill({status:204,headers});
    if (u.searchParams.get("section") === "recommendations") return route.fulfill({headers,json:{auto_reply:{recommendations:[]}}});
    expect(u.searchParams.get("section")).toBe("history");
    const id = u.searchParams.get("thread_id") || threads[0].id;
    const oldMessages = u.searchParams.has("message_cursor"), oldEvents = u.searchParams.has("event_cursor");
    const alt = id === threads[1].id;
    await route.fulfill({headers,json:{auto_reply:{threads,thread_id:id,
      messages:alt ? [] : oldMessages ? messages.slice(0,2) : messages.slice(1),
      events:alt ? [] : oldEvents ? [{id:"read",title:"读取聊天历史",status:"success",occurred_at:messages[0].sent_at}] : u.searchParams.get("event_group")==="all" ? [...events,{id:"verify",title:"核验聊天对象",status:"success",group:"verification",occurred_at:messages[0].sent_at}] : events,
      goals:alt ? [] : goals,next_message_cursor:alt || oldMessages ? "" : "prior-messages",next_event_cursor:alt || oldEvents ? "" : "prior-events"}}});
  });
  await page.goto("/admin/resumes/detail?candidate_id=uniform-candidate-1");
  await page.getByRole("tab",{name:"自动回复",exact:true}).click();
  const section = page.getByRole("region",{name:"自动回复沟通记录"});
  const chat = section.getByRole("list",{name:/双方聊天记录/});
  await expect(chat.getByText("AI 自动发送",{exact:true})).toHaveCount(1);
  await expect(chat.getByRole("button",{name:"打开附件"})).toHaveCount(1);
  await expect(chat.getByText("错误来源.pdf")).toHaveCount(0);
  let downloads=0;
  await page.route("**/api/candidates/*/auto-reply/attachments/one?**",async route=>{
    const headers={"access-control-allow-origin":"*","access-control-allow-headers":"*"};
    if(route.request().method()==="OPTIONS")return route.fulfill({status:204,headers});
    expect(new URL(route.request().url()).searchParams.get("thread_id")).toBe(threads[0].id);
    expect(route.request().headers().authorization).toBe("Bearer auto-reply-synthetic-token");
    downloads++;
    if(downloads===1)return route.fulfill({status:404,headers,json:{message:"这份附件已清理"}});
    return route.fulfill({headers,contentType:"application/pdf",body:"%PDF-1.4\nsynthetic fixture\n%%EOF"});
  });
  await chat.getByRole("button",{name:"下载附件"}).click();
  await expect(chat.getByRole("alert")).toHaveText("这份附件已清理");
  const file=page.waitForEvent("download");
  await chat.getByRole("button",{name:"下载附件"}).click();
  expect((await file).suggestedFilename()).toBe("合成简历.pdf");
  await section.getByRole("button",{name:"查看更早消息"}).click();
  await expect(chat.getByRole("listitem")).toHaveCount(3);
  await expect(chat.getByRole("listitem").first()).toContainText("来源未记录");
  await expect(chat.getByRole("listitem").last()).toContainText("AI 自动发送");
  mkdirSync(output,{recursive:true});
  for (const width of [1366,390]) {
    await page.setViewportSize({width,height:900});
    await section.scrollIntoViewIfNeeded();
    await page.screenshot({path:resolve(output,`candidate-history-${width}.png`),animations:"disabled"});
    const box = await section.boundingBox();
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
  }
  await section.getByRole("tab",{name:"操作时间线"}).click();
  await expect(section.getByText("耗时 1.32 秒")).toBeVisible();
  await section.getByRole("button",{name:"查看更多操作"}).click();
  await expect(section.getByText("读取聊天历史")).toBeVisible();
  await section.getByRole("checkbox",{name:"查看核验操作"}).check();
  await expect(section.getByText("核验聊天对象")).toBeVisible();
  await section.getByRole("checkbox",{name:"查看核验操作"}).uncheck();
  await expect(section.getByText("核验聊天对象")).toHaveCount(0);
  await section.getByRole("tab",{name:"沟通目标"}).click();
  await expect(section.getByText("已满足",{exact:true})).toBeVisible();
  await expect(section.getByText("候选人明确表示接受成都办公。")).toBeVisible();
  await section.getByText("查看关联原话（1）").click();
  await expect(section.getByRole("tabpanel",{name:"沟通目标"}).getByText("我可以在成都工作。",{exact:false})).toBeVisible();
  await section.getByRole("combobox",{name:"选择沟通会话"}).click();
  await page.getByTitle(/重庆区域经理/).click();
  await expect(section.getByText("这段沟通还没有聊天记录。")).toBeVisible();
  await expect(section.getByText("收到，感谢确认。")).toHaveCount(0);
});

test("主动退出先撤销本机内存凭证再返回登录页", async ({ page }) => {
  await fixture(page);
  let unbound = false;
  await page.route("**/api/v1/session/unbind", async (route) => {
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    expect(route.request().postDataJSON().token).toBe("auto-reply-synthetic-token");
    unbound = true;
    await route.fulfill({ headers, json: { ok: true, data: { unbound: true } } });
  });
  await page.goto("/admin/positions");
  const workspace = await openReplyWorkspace(page);
  await expect(workspace.getByRole("button", { name:"开始自动回复", exact:true })).toBeEnabled();
  await workspace.getByRole("button", {name:"关闭",exact:true}).last().click();
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  expect(unbound).toBe(true);
});

test("本地程序同端口重启后，开始自动回复重新绑定并等待启动检查", async ({ page }) => {
  const { state } = await fixture(page);
  await page.goto("/admin/positions");
  const workspace = await openReplyWorkspace(page);
  await expect(workspace.getByRole("button", {name:"开始自动回复",exact:true})).toBeEnabled();
  let bound = false;
  let checks = 0;
  let ready = false;
  await page.route(/\/api\/v1\/(?:session\/bind|runtime\/status)$/, async (route) => {
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (route.request().url().endsWith("/session/bind")) {
      expect(route.request().postDataJSON().token).toBe("auto-reply-synthetic-token");
      bound = true;
      return route.fulfill({ headers, json: { ok: true, data: {} } });
    }
    if (bound) checks++;
    return route.fulfill({ headers, json: { ok: true, data: { camoufox_ready: false, worker_ready: false, camoufox_installed: ready, camoufox_version: "", install_progress: { running: !ready, stage: ready ? "installed" : "queued" }, native_input_safety: { ready: true } } } });
  });
  await workspace.getByRole("button", { name: "开始自动回复", exact: true }).click();
  const account = page.getByRole("dialog", { name: "确认当前招聘账号", exact: true });
  await account.getByRole("textbox", { name: "当前招聘账号", exact: true }).fill("合成招聘账号");
  await account.getByRole("button", { name: "确认账号并开始", exact: true }).click();
  await expect.poll(() => checks).toBeGreaterThan(0);
  expect(bound).toBe(true);
  expect(state.starts).toBe(0);
  ready = true;
  await expect(page.getByRole("button", { name: "停止自动回复", exact: true })).toBeVisible();
  expect(bound).toBe(true);
  expect(checks).toBeGreaterThanOrEqual(2);
  expect(state.starts).toBe(1);
});
