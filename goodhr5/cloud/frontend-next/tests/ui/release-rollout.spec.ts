/** 文件作用说明：用合成接口验证自动回复灰度入口、既有配置维护，以及顶部余额精度与版本展示。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";
import { AUTO_REPLY_ROLLOUT_MESSAGE } from "../../lib/auto-reply-rollout";

const output=resolve(process.env.UI_TEST_SCREENSHOT_DIR||"../../../output/release-rollout-20261109");
test.use({storageState:{cookies:[],origins:[{origin:uniformOrigin,localStorage:[{name:"goodhr5_access_token",value:"rollout-synthetic-token"}]}]}});

/** rolloutFixture 按角色、会员与连接状态模拟灰度管理，不向真实账号、Agent或云端发送业务请求。 */
async function rolloutFixture(page:Page,options:{role?:string;member?:boolean;online?:boolean;enabled?:boolean}={}){
  const base=await mockUniformAdmin(page);const online=options.online!==false;const member=options.member!==false;
  const state={balance:"100.9999" as string|null,running:false,writes:[] as Array<{path:string;body:Record<string,unknown>}>,config:{position_id:"uniform-position-1",company_profile_id:"company-one",enabled:Boolean(options.enabled),position_description:"原岗位说明",resume_request_message:"请发一份简历。",conditions:[],condition_options:[],default_phrases:[],phrase_overrides:[],phrases_version:"rollout-1",poll_interval_seconds:5,max_threads_per_checkpoint:3,version:1}};
  const headers={"access-control-allow-origin":"*","access-control-allow-methods":"*","access-control-allow-headers":"*"};
  await page.addLocatorHandler(page.getByRole("dialog",{name:"请先安装本地程序",exact:true}),async()=>{await page.keyboard.press("Escape");});
  await page.addInitScript(({port,online})=>{
    if(online)localStorage.setItem("goodhr5_local_agent_detect_cache",JSON.stringify({baseURL:`http://127.0.0.1:${port}`,checkedAt:Date.now()}));
    else localStorage.removeItem("goodhr5_local_agent_detect_cache");
  },{port:process.env.UI_UNIFORM_AGENT_PORT||"43129",online});
  await page.route(/\/api\/(?:auth\/me|subscription\/status|ai-wallet$|auto-reply\/|v1\/local\/auto-reply\/)/,async route=>{
    const request=route.request(),path=new URL(request.url()).pathname,method=request.method();
    if(method==="OPTIONS")return route.fulfill({status:204,headers});
    let data:unknown;
    if(path==="/api/auth/me")data={user:{id:"uniform-user",email:"uniform@example.invalid",role:options.role||"super_admin",tenant_id:"uniform-team",has_openid:true}};
    else if(path==="/api/subscription/status")data={subscription:{active:member,member_type:member?"max":"free",member_name:"合成Max会员",remaining_days:30,allow_auto_reply:member,allow_ai:member}};
    else if(path==="/api/ai-wallet")data={balance:state.balance,balance_units:1009999};
    else if(path==="/api/auto-reply/questions/summary")data={pending_count:0,positions:[]};
    else if(path.endsWith("/auto-reply/status"))data={ok:true,data:{status:state.running?"running":"stopped"}};
    else if(path.endsWith("/auto-reply/stop")){state.running=false;state.writes.push({path,body:{}});data={ok:true,data:{status:"stopped"}};}
    else if(path.endsWith("/status"))data={configured_enabled:state.config.enabled,enabled:state.config.enabled&&member,version:state.config.version,subscription:{allow_auto_reply:member}};
    else if(path.endsWith("/config")){
      if(method==="PUT"){const body=request.postDataJSON();state.writes.push({path,body});state.config={...state.config,...body};}
      data={config:state.config};
    }else if(path.endsWith("/company-profiles"))data={company_profiles:[{id:"company-one",name:"合成公司",address:"",contact:"",overview:"合成公司介绍",extra_info:"",updated_at:"2026-11-09T00:00:00Z"}]};
    else{if(method!=="GET")state.writes.push({path,body:request.postDataJSON()||{}});throw new Error(`灰度界面不应请求 ${method} ${path}`);}
    await route.fulfill({headers,json:data});
  });
  if(!online)await page.route(new RegExp(`^http://127\\.0\\.0\\.1:${process.env.UI_UNIFORM_AGENT_PORT||"43129"}/`),route=>route.abort());
  return{state,base};
}

for(const [role,member,online,width] of [["user",false,false,390],["admin",true,true,1440],["super_admin",true,true,1440]] as const){
  test(`灰度 ${role} ${width}px 在会员和连接检查前拦截开启，仍可查看配置`,async({page})=>{
    const{state}=await rolloutFixture(page,{role,member,online});await page.setViewportSize({width,height:960});await page.goto("/admin/positions");
    await page.getByTestId("auto-reply-global").click();const workspace=page.getByRole("dialog",{name:"自动回复",exact:true});await expect(workspace).toBeVisible();
    const start=workspace.getByRole("button",{name:"开始自动回复",exact:true});await expect(start).toBeEnabled();await start.click();
    await expect(page.locator(".ant-notification").getByText(AUTO_REPLY_ROLLOUT_MESSAGE,{exact:true}).first()).toBeVisible();
    await expect(page.locator(".ant-notification").getByText(AUTO_REPLY_ROLLOUT_MESSAGE,{exact:true})).toHaveCount(1);
    await expect(page.getByRole("dialog",{name:"确认当前招聘账号",exact:true})).toHaveCount(0);await expect(page.getByRole("dialog",{name:"自动回复需要 Max",exact:true})).toHaveCount(0);
    await workspace.getByRole("button",{name:"关闭",exact:true}).last().click();
    const card=page.locator('.ant-card[data-position-id="uniform-position-1"]');const toggle=card.getByRole("switch");await expect(toggle).not.toBeDisabled();await toggle.click();await expect(toggle).not.toBeChecked();
    await card.getByRole("button",{name:/自动回复设置$/}).click();const settings=page.getByRole("dialog",{name:/^自动回复 · /});
    await expect(settings.getByRole("textbox",{name:"自动回复岗位描述",exact:true})).toHaveValue("原岗位说明");await settings.getByRole("switch",{name:"开启岗位自动回复",exact:true}).click();
    await expect(settings.getByRole("switch",{name:"开启岗位自动回复",exact:true})).not.toBeChecked();await expect(settings.getByText(AUTO_REPLY_ROLLOUT_MESSAGE,{exact:true})).toBeVisible();
    await expect(page.getByRole("dialog",{name:"自动回复需要 Max",exact:true})).toHaveCount(0);expect(state.writes).toEqual([]);
    mkdirSync(output,{recursive:true});await page.screenshot({path:resolve(output,`rollout-${role}-${width}.png`),fullPage:true,animations:"disabled"});
  });
}

test("灰度状态读取失败仍可点击提示，不查配置、不启用也不弹付费",async({page})=>{
  const{state}=await rolloutFixture(page,{member:false});let statusReads=0;let configReads=0;
  await page.route("**/api/auto-reply/positions/*/status",async route=>{statusReads++;await route.fulfill({status:503,headers:{"access-control-allow-origin":"*"},json:{message:"合成状态暂不可用"}});});
  await page.route("**/api/auto-reply/positions/*/config",async route=>{configReads++;await route.fallback();});
  await page.goto("/admin/positions");await expect.poll(()=>statusReads).toBeGreaterThan(0);
  const toggle=page.locator('.ant-card[data-position-id="uniform-position-1"]').getByRole("switch");await expect(toggle).toBeEnabled();await toggle.click();await expect(toggle).not.toBeChecked();
  await expect(page.locator(".ant-notification").getByText(AUTO_REPLY_ROLLOUT_MESSAGE,{exact:true})).toBeVisible();
  await expect(page.getByRole("dialog",{name:"自动回复需要 Max",exact:true})).toHaveCount(0);expect(state.writes).toEqual([]);expect(configReads).toBe(0);
});

test("灰度不阻挡Max已有开启配置改正文、关闭岗位和停止既有运行",async({page})=>{
  const{state}=await rolloutFixture(page,{enabled:true});state.running=true;await page.goto("/admin/positions");
  const card=page.locator('.ant-card[data-position-id="uniform-position-1"]');await card.getByRole("button",{name:/自动回复设置$/}).click();
  const settings=page.getByRole("dialog",{name:/^自动回复 · /});await settings.getByRole("textbox",{name:"自动回复岗位描述",exact:true}).fill("灰度期间修改的真实岗位正文");
  await settings.getByRole("button",{name:"保存配置",exact:true}).click();await expect(settings).toBeHidden();expect(state.config.enabled).toBe(true);expect(state.config.position_description).toContain("灰度期间修改");
  await card.getByRole("switch").click();await expect(card.getByRole("switch")).not.toBeChecked();expect(state.config.enabled).toBe(false);
  await card.getByRole("switch").click();await expect(card.getByRole("switch")).not.toBeChecked();expect(state.writes).toHaveLength(2);
  await page.getByTestId("auto-reply-global").click();const workspace=page.getByRole("dialog",{name:"自动回复",exact:true});await workspace.getByRole("button",{name:"停止自动回复",exact:true}).click();
  await expect(workspace.getByRole("button",{name:"开始自动回复",exact:true})).toBeVisible();expect(state.running).toBe(false);
  expect(state.writes.map(item=>item.path)).toEqual(["/api/auto-reply/positions/uniform-position-1/config","/api/auto-reply/positions/uniform-position-1/config","/api/v1/local/auto-reply/stop"]);
});

test("顶部只显示剩余天数、展示余额与版本，边界精度和实际余额保持正确",async({page})=>{
  const{state}=await rolloutFixture(page);await page.setViewportSize({width:1440,height:960});
  for(const [balance,display] of [["100.9999","￥100"],["1.0001","￥1"],["1.0000","￥1.0000"],["0.1234","￥0.1234"],["0.0000","￥0.0000"],[null,"￥0.00"]] as const){
    state.balance=balance;await page.goto("/admin/positions");const capsule=page.locator(".goodhr-admin-toolbar").getByRole("button",{name:/AI余额/});
    await expect(capsule).toHaveText(`剩30天 · AI余额 ${display}`);await expect(capsule.locator("svg")).toHaveCount(0);await expect(capsule).not.toContainText("Max");
    await expect(page.getByTestId("local-agent-status")).toHaveText("6.2.11.0");await expect(page.getByTestId("local-agent-status")).not.toContainText("端口");expect(state.balance).toBe(balance);
    if(balance==="100.9999"){mkdirSync(output,{recursive:true});await page.screenshot({path:resolve(output,"header-balance-1440.png"),fullPage:true,animations:"disabled"});}
  }
});

for(const width of [1366,390])test(`会员页 ${width}px 展示灰度限制且保留套餐价格、余额原精度和权益`,async({page})=>{
  const{state}=await rolloutFixture(page);state.balance="0.1234";
  const plan={id:"synthetic-max",name:"合成Max月卡",member_type:"max",duration_days:30,original_price:299,discount_amount:10,allow_auto_reply:true,description:"完整解锁 AI 筛选和自动回复",features:["AI 筛选","包含自动回复"]};
  await page.route("**/api/subscription/plans",route=>route.fulfill({headers:{"access-control-allow-origin":"*"},json:{plans:[plan]}}));
  await page.setViewportSize({width,height:960});await page.goto("/admin/subscription");
  await expect(page.getByText(`30 天 · ${AUTO_REPLY_ROLLOUT_MESSAGE}`,{exact:true})).toBeVisible();await expect(page.getByText(/自动回复可用/)).toHaveCount(0);
  await expect(page.getByText("自动回复灰度中，暂不开放。",{exact:true})).toBeVisible();await expect(page.getByText("￥289",{exact:true})).toBeVisible();
  await expect(page.getByText("￥0.1234",{exact:true}).first()).toBeVisible();expect(plan.allow_auto_reply).toBe(true);expect(plan.original_price).toBe(299);expect(state.writes).toEqual([]);
  mkdirSync(output,{recursive:true});await page.screenshot({path:resolve(output,`subscription-rollout-${width}.png`),fullPage:true,animations:"disabled"});
});
