/** 文件作用说明：用合成接口验证自动回复灰度入口、既有配置维护，以及顶部余额精度与版本展示。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";
import { AUTO_REPLY_ROLLOUT_MESSAGE, AUTO_REPLY_TEST_NOTICE } from "../../lib/auto-reply-rollout";

const output=resolve(process.env.UI_TEST_SCREENSHOT_DIR||"../../../output/release-rollout-20261109");
test.use({storageState:{cookies:[],origins:[{origin:uniformOrigin,localStorage:[{name:"goodhr5_access_token",value:"rollout-synthetic-token"}]}]}});

/** rolloutFixture 按角色、会员与连接状态模拟灰度管理，不向真实账号、Agent或云端发送业务请求。 */
async function rolloutFixture(page:Page,options:{role?:string;member?:boolean;online?:boolean;enabled?:boolean;platform?:string}={}){
  const base=await mockUniformAdmin(page);const online=options.online!==false;const member=options.member!==false;
  const state={balance:"100.9999" as string|null,running:false,writes:[] as Array<{path:string;body:Record<string,unknown>}>,config:{position_id:"uniform-position-1",company_profile_id:"company-one",enabled:Boolean(options.enabled),position_description:"原岗位说明",resume_request_message:"请发一份简历。",conditions:[],condition_options:[],default_phrases:[],phrase_overrides:[],phrases_version:"rollout-1",poll_interval_seconds:5,max_threads_per_checkpoint:3,version:1}};
  const headers={"access-control-allow-origin":"*","access-control-allow-methods":"*","access-control-allow-headers":"*"};
  await page.addLocatorHandler(page.getByRole("dialog",{name:"请先安装本地程序",exact:true}),async()=>{await page.keyboard.press("Escape");});
  await page.addInitScript(({port,online})=>{
    if(online)localStorage.setItem("goodhr5_local_agent_detect_cache",JSON.stringify({baseURL:`http://127.0.0.1:${port}`,checkedAt:Date.now()}));
    else localStorage.removeItem("goodhr5_local_agent_detect_cache");
  },{port:process.env.UI_UNIFORM_AGENT_PORT||"43129",online});
  await page.route(/\/api\/(?:auth\/me|subscription\/status|ai-wallet$|positions(?:\?|$)|auto-reply\/|v1\/local\/auto-reply\/)/,async route=>{
    const request=route.request(),path=new URL(request.url()).pathname,method=request.method();
    if(method==="OPTIONS")return route.fulfill({status:204,headers});
    let data:unknown;
    if(path==="/api/positions") data={positions:[{id:"uniform-position-1",name:"测试岗位",platform_id:options.platform||"boss",creator_email:"uniform@example.invalid",is_open:true,status:"stopped",common_config:{mode_default:"ai"}}],total:1};
    else if(path==="/api/auth/me")data={user:{id:"uniform-user",email:"uniform@example.invalid",role:options.role||"super_admin",tenant_id:"uniform-team",has_openid:true}};
    else if(path==="/api/subscription/status")data={subscription:{active:member,member_type:member?"max":"free",member_name:"合成Max会员",remaining_days:30,allow_auto_reply:member,allow_ai:member}};
    else if(path==="/api/ai-wallet")data={balance:state.balance,balance_units:1009999};
    else if(path==="/api/auto-reply/questions/summary")data={pending_count:0,positions:[]};
    else if(path.endsWith("/auto-reply/status"))data={ok:true,data:{status:state.running?"running":"stopped"}};
    else if(path.endsWith("/auto-reply/start")){state.running=true;state.writes.push({path,body:request.postDataJSON()});data={ok:true,data:{status:"running"}};}
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

for(const width of [1366,390]) for(const platform of ["boss","liepin","zhaopin","hliepin","51job"]) {
 test(`自动回复 ${platform} ${width}px 仅允许测试平台开启并显示联系作者入口`,async({page})=>{
  const {state}=await rolloutFixture(page,{platform,member:false});
  await page.setViewportSize({width,height:960});await page.goto("/admin/positions");
  const card=page.locator('.ant-card[data-position-id="uniform-position-1"]');
  await card.getByRole("button",{name:/自动回复设置$/}).click();
  const settings=page.getByRole("dialog",{name:/^自动回复 · /});
  await expect(settings.getByText(AUTO_REPLY_TEST_NOTICE,{exact:true})).toBeVisible();
  await expect(settings.getByRole("link",{name:"添加作者微信",exact:true})).toHaveAttribute("href","https://goodhr.58it.cn/contact");
  const toggle=settings.getByRole("switch",{name:"开启岗位自动回复",exact:true});
  if(["boss","liepin","zhaopin"].includes(platform)) {
   await expect(toggle).toBeEnabled();await toggle.click();await expect(toggle).toBeChecked();
  } else {
   await expect(toggle).toBeDisabled();await expect(settings.getByText(AUTO_REPLY_ROLLOUT_MESSAGE,{exact:true})).toBeVisible();
  }
  mkdirSync(output,{recursive:true});await settings.screenshot({path:resolve(output,`auto-reply-${platform}-${width}.png`),animations:"disabled"});
  await settings.getByRole("button",{name:"保存配置",exact:true}).click();await expect(settings).toBeHidden();
  expect(state.config.enabled).toBe(["boss","liepin","zhaopin"].includes(platform));
  await page.getByTestId("auto-reply-global").click();
  const workspace=page.getByRole("dialog",{name:"自动回复",exact:true});
  await expect(workspace.getByText(AUTO_REPLY_TEST_NOTICE,{exact:true})).toBeVisible();
  await expect(workspace.getByRole("link",{name:"添加作者微信",exact:true})).toBeVisible();
 });
}

test("未开放平台仍可编辑和关闭已有配置，并停止既有运行",async({page})=>{
 const {state}=await rolloutFixture(page,{platform:"hliepin",enabled:true});state.running=true;
 await page.goto("/admin/positions");const card=page.locator('.ant-card[data-position-id="uniform-position-1"]');
 await card.getByRole("button",{name:/自动回复设置$/}).click();const settings=page.getByRole("dialog",{name:/^自动回复 · /});
 await settings.getByRole("textbox",{name:"自动回复岗位描述",exact:true}).fill("保留可编辑正文");
 await settings.getByRole("button",{name:"保存配置",exact:true}).click();await expect(settings).toBeHidden();expect(state.config.enabled).toBe(true);
 await card.getByRole("switch").click();await expect(card.getByRole("switch")).not.toBeChecked();await expect(card.getByRole("switch")).toBeDisabled();
 await page.getByTestId("auto-reply-global").click();const workspace=page.getByRole("dialog",{name:"自动回复",exact:true});
 await workspace.getByRole("button",{name:"停止自动回复",exact:true}).click();expect(state.running).toBe(false);
});

test("顶部只显示余额与版本，边界精度和实际余额保持正确",async({page})=>{
  const{state}=await rolloutFixture(page);await page.setViewportSize({width:1440,height:960});
  for(const [balance,display] of [["100.9999","￥100"],["1.0001","￥1"],["1.0000","￥1.0000"],["0.1234","￥0.1234"],["0.0000","￥0.0000"],[null,"￥0.00"]] as const){
    state.balance=balance;await page.goto("/admin/positions");const capsule=page.locator(".goodhr-admin-toolbar").getByRole("button",{name:/AI余额/});
    await expect(capsule).toHaveText(`AI余额 ${display}`);await expect(capsule.locator("svg")).toHaveCount(0);await expect(capsule).not.toContainText("Max");
    await expect(page.getByTestId("local-agent-status")).toHaveText("6.2.11.0");await expect(page.getByTestId("local-agent-status")).not.toContainText("端口");expect(state.balance).toBe(balance);
    if(balance==="100.9999"){mkdirSync(output,{recursive:true});await page.screenshot({path:resolve(output,"header-balance-1440.png"),fullPage:true,animations:"disabled"});}
  }
});

for(const width of [1366,390])test(`会员页 ${width}px 保留余额原精度和套餐权益`,async({page})=>{
  const{state}=await rolloutFixture(page);state.balance="0.1234";
  const plan={id:"synthetic-max",name:"合成Max月卡",member_type:"max",duration_days:30,original_price:299,discount_amount:10,allow_auto_reply:true,description:"完整解锁 AI 筛选和自动回复",features:["AI 筛选","包含自动回复"]};
  await page.route("**/api/subscription/plans",route=>route.fulfill({headers:{"access-control-allow-origin":"*"},json:{plans:[plan]}}));
  await page.setViewportSize({width,height:960});await page.goto("/admin/subscription");
  await expect(page.getByText("30 天 · 自动回复可用",{exact:true})).toBeVisible();
  await expect(page.getByText("自动回复灰度中，暂不开放。",{exact:true})).toHaveCount(0);await expect(page.getByText("￥289",{exact:true})).toBeVisible();
  await expect(page.getByText("￥0.1234",{exact:true}).first()).toBeVisible();expect(plan.allow_auto_reply).toBe(true);expect(plan.original_price).toBe(299);expect(state.writes).toEqual([]);
  mkdirSync(output,{recursive:true});await page.screenshot({path:resolve(output,`subscription-rollout-${width}.png`),fullPage:true,animations:"disabled"});
});

// 已确认的测试开放不要求先添加微信，账号核对完成后才发起一次启动。
test("测试提示不阻止确认账号后启动",async({page})=>{
 const {state}=await rolloutFixture(page);await page.goto("/admin/positions");
 await page.getByTestId("auto-reply-global").click();const workspace=page.getByRole("dialog",{name:"自动回复",exact:true});
 await workspace.getByRole("button",{name:"开始自动回复",exact:true}).click();
 const account=page.getByRole("dialog",{name:"确认当前招聘账号",exact:true});await expect(account).toBeVisible();
 await account.getByRole("textbox",{name:"当前招聘账号",exact:true}).fill("合成招聘账号");
 await account.getByRole("button",{name:"确认账号并开始",exact:true}).click();
 await expect(workspace.getByRole("button",{name:"停止自动回复",exact:true})).toBeVisible();
 expect(state.writes.filter(item=>item.path.endsWith("/auto-reply/start"))).toHaveLength(1);
});

for(const width of [1366,390]) test(`共享后台菜单 ${width}px 保持可见且没有横向溢出`,async({page})=>{
 await rolloutFixture(page);await page.setViewportSize({width,height:960});
 for(const path of ["/admin/positions","/admin/resumes","/admin/recommendations","/admin/recruitment"]) {
  await page.goto(path);await expect(page.locator(".goodhr-admin-toolbar")).toBeVisible();
  mkdirSync(output,{recursive:true});await page.screenshot({path:resolve(output,`menus-${path.split('/').at(-1)}-${width}.png`),fullPage:true,animations:"disabled"});
 }
});

// 云端执行故障显示真实原因；恢复状态后允许开启，避免要求用户反复安装客户端。
for (const width of [1366, 390]) test(`自动回复 ${width}px 显示云端错误并在恢复后允许开启`, async ({ page }) => {
 const { state } = await rolloutFixture(page);
 let unavailable = true;
 await page.route("**/api/v1/local/auto-reply/status", async route => {
  const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
  if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
  await route.fulfill({ status: unavailable ? 502 : 200, headers, json: unavailable ? { ok: false, error: { code: "CLOUD_STATUS_UNAVAILABLE", message: "云端执行程序尚未配置好" } } : { ok: true, data: { status: "stopped" } } });
 });
 await page.setViewportSize({ width, height: 960 }); await page.goto("/admin/positions");
 await page.getByTestId("auto-reply-global").click();
 const workspace = page.getByRole("dialog", { name: "自动回复", exact: true });
 await expect(workspace.getByText("云端执行程序尚未配置好", { exact: true })).toBeVisible();
 await expect(workspace.getByRole("button", { name: "开始自动回复", exact: true })).toBeDisabled();
 mkdirSync(output, { recursive: true }); await workspace.screenshot({ path: resolve(output, `auto-reply-cloud-error-${width}.png`), animations: "disabled" });
 unavailable = false;
 await expect(workspace.getByRole("button", { name: "开始自动回复", exact: true })).toBeEnabled();
 await expect(workspace.getByText("云端执行程序尚未配置好", { exact: true })).toHaveCount(0);
 await workspace.screenshot({ path: resolve(output, `auto-reply-recovered-${width}.png`), animations: "disabled" });
 expect(state.writes).toEqual([]);
});
