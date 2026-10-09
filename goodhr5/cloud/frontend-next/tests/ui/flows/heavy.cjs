/* 本文件只在本地浏览器中拦截全部业务接口，验证 Ant Design 岗位与会员页面；不会请求真实业务数据。 */
const { chromium } = require("@playwright/test");
const fs = require("node:fs");
const path = require("node:path");
const assert = require("node:assert/strict");
/** runHeavyChecks 通过全部拦截的本地接口验证岗位、会员、支付和个人配置的 59 项交互。 */
async function runHeavyChecks(baseURL = "http://127.0.0.1:5190") {
  const origin = new URL(baseURL).origin;
  const output = path.resolve(__dirname, "../../../artifacts/antd-heavy-qa");
  fs.mkdirSync(output, { recursive: true });
  const email = "qa@example.test";
  const subscription = {
    active: true,
    member_type: "max",
    member_name: "Max 全能版",
    remaining_days: 30,
    remaining_seconds: 2592000,
    allow_ai: true,
    allow_auto_reply: true,
    expires_at: "2026-11-08T00:00:00+08:00",
  };
  const wallet = {
    balance: "18.0000",
    balance_units: 180000,
    default_model: "qwen-test",
    models: [
      { id: "qwen-test", name: "千问测试" },
      { id: "vision-test", name: "视觉测试" },
    ],
  };
  const positions = [
    {
      id: "position-test",
      name: "测试招聘岗位",
      platform_id: "boss",
      creator_email: email,
      created_at: "2026-10-09T10:00:00+08:00",
      status: "stopped",
      keywords: ["工程师"],
      exclude_keywords: [],
      match_limit: 50,
      common_config: { mode_default: "ai", detail_mode: "ai" },
      ai_config: {
        position_requirement: "测试岗位条件",
        filter_prompt: "系统默认打招呼提示词",
        open_detail_prompt: "系统默认看详情提示词",
        detail_score_threshold: 60,
        greet_score_threshold: 70,
        request_score_threshold: 70,
      },
    },
  ];
  const requests = [],
    errors = [],
    checks = [];
  let activePage;
  let saveDelay = 0,
    paymentDelay = 0,
    refundDelay = 0,
    modelDelay = 0;
  /** pause 等待原生图片预览入场动画完成，避免测试按键与动画切换竞争。 */
  async function pause(ms) {
    return new Promise((r) => setTimeout(r, ms));
  }
  /** expectVisible 校验目标已显示，并记录已验证的交互。 */
  async function expectVisible(locator, label) {
    await locator.waitFor({ state: "visible", timeout: 20000 });
    checks.push(label);
  }
  /** expectHidden 校验目标已关闭，并记录已验证的交互。 */
  async function expectHidden(locator, label) {
    await locator.waitFor({ state: "hidden", timeout: 10000 });
    checks.push(label);
  }
  /** fillNumber 按用户输入和失焦步骤填写数字，触发原生组件的范围与精度处理。 */
  async function fillNumber(page, label, value) {
    const field = page.getByLabel(label, { exact: true });
    await field.fill(String(value));
    await field.press("Tab");
  }
  const browser = await chromium.launch({ channel: "chrome", headless: true });
  try {
    const context = await browser.newContext({
      viewport: { width: 1440, height: 1050 },
      storageState: {
        cookies: [],
        origins: [
          {
            origin,
            localStorage: [
              { name: "goodhr5_access_token", value: "qa-test-token" },
            ],
          },
        ],
      },
    });
    /** 接口拦截覆盖云端与 Agent，其他外部请求直接终止；测试期间不会发起真实业务请求。 */
    await context.route("**/*", async (route) => {
      const url = new URL(route.request().url()),
        method = route.request().method();
      if (url.origin === origin) return route.continue();
      if (
        !["127.0.0.1:8084", "127.0.0.1:43129", "goodhr.58it.cn"].includes(
          url.host,
        )
      )
        return route.abort();
      const headers = {
        "access-control-allow-origin": "*",
        "access-control-allow-methods": "GET,POST,PUT,DELETE,OPTIONS",
        "access-control-allow-headers": "content-type,authorization",
        "content-type": "application/json",
      };
      if (method === "OPTIONS")
        return route.fulfill({ status: 204, headers, body: "" });
      let body;
      try {
        body = route.request().postDataJSON();
      } catch {}
      requests.push({ path: url.pathname, method, body });
      let data = {};
      if (url.port === "43129") {
        if (url.pathname === "/health")
          data = { ok: true, version: "6.2.10.1" };
        else if (url.pathname === "/api/v1/runtime/status")
          data = {
            node_installed: true,
            camoufox_installed: true,
            version: "6.2.10.1",
            platform: "win-x64",
          };
        else if (url.pathname.endsWith("/status"))
          data = {
            status: "stopped",
            scanned_count: 3,
            greeted_count: 1,
            skipped_count: 2,
          };
        else if (url.pathname.endsWith("/logs"))
          data = {
            logs: [
              {
                id: "log-1",
                created_at: "2026-10-09T10:00:00.111+08:00",
                message: "本地测试日志",
                level: "info",
              },
            ],
          };
      } else {
        if (url.pathname === "/api/auth/me")
          data = {
            user: {
              id: "qa-user",
              email,
              name: "测试用户",
              tenant_id: "qa-tenant",
              is_super_admin: true,
            },
          };
        else if (url.pathname === "/api/subscription/status")
          data = { subscription };
        else if (url.pathname === "/api/ai-wallet") data = { wallet };
        else if (url.pathname === "/api/runtime/config")
          data = {
            config: {
              local_agent: [
                {
                  version: "6.2.10.1",
                  url_win: "https://example.test/agent.exe",
                  sha256: "qa",
                },
              ],
            },
          };
        else if (url.pathname === "/api/config/notification-profile")
          data = { profile: { completed: true } };
        else if (url.pathname === "/api/positions" && method === "GET")
          data = { positions };
        else if (url.pathname === "/api/positions" && method === "POST") {
          await pause(saveDelay);
          data = { ok: true };
        } else if (url.pathname === "/api/system/default-prompts")
          data = {
            prompts: {
              open_detail_prompt: "系统默认看详情提示词",
              filter_prompt: "系统默认打招呼提示词",
            },
          };
        else if (url.pathname === "/api/platforms/config/")
          data = {
            platforms: ["boss", "zhaopin", "hliepin", "liepin"].map(
              (platform_id) => ({ platform_id, open: true }),
            ),
          };
        else if (url.pathname === "/api/subscription/plans")
          data = {
            plans: [
              {
                id: "free",
                member_type: "free",
                name: "免费版",
                original_price: 0,
                duration_days: 0,
                features: ["关键词筛选"],
              },
              {
                id: "plus",
                member_type: "plus",
                name: "Plus 基础版",
                original_price: 30,
                duration_days: 30,
                features: ["AI筛选"],
              },
              {
                id: "max",
                member_type: "max",
                name: "Max 全能版",
                original_price: 60,
                duration_days: 30,
                features: ["自动回复"],
                recommended: true,
              },
            ],
          };
        else if (url.pathname === "/api/payment/orders")
          data = {
            orders: [
              {
                order_no: "qa-paid",
                plan_name: "测试订单",
                amount_cents: 3000,
                status: "paid",
                created_at: "2026-10-09T10:00:00+08:00",
              },
            ],
          };
        else if (url.pathname.startsWith("/api/payment/orders/"))
          data = { order: { status: "pending" } };
        else if (url.pathname === "/api/ai-wallet/records")
          data = {
            records: [
              {
                id: "qa-record",
                category: "ai_usage",
                change_units: -10,
                change: "-0.0010",
                balance_after: "18.0000",
                model_id: "qwen-test",
                reason: "测试AI调用",
                created_at: "2026-10-09T10:00:00+08:00",
              },
            ],
            total: 1,
          };
        else if (url.pathname === "/api/config/user-ai") {
          if (method === "PUT") await pause(modelDelay);
          data = {
            config: {
              base_url: "https://example.test/v1/chat/completions",
              api_key: "qa-secret",
              api_key_set: true,
              model: body?.model || "qwen-test",
              enabled: true,
            },
          };
        } else if (url.pathname === "/api/payment/refund") {
          if (method === "POST") {
            await pause(refundDelay);
            data = { message: "退款已提交" };
          } else
            data = {
              membership: {
                available: true,
                max_amount: "12.34",
                max_amount_cents: 1234,
              },
              ai_balance: {
                available: true,
                max_amount: "8.88",
                max_amount_cents: 888,
              },
            };
        } else if (
          url.pathname === "/api/payment/create" ||
          url.pathname === "/api/payment/ai-balance"
        ) {
          await pause(paymentDelay);
          data = {
            order: {
              order_no: "qa-order",
              plan_name: "测试支付订单",
              amount: "10.00",
            },
            payment: { code_url: "weixin://wxpay/test-only" },
          };
        } else if (url.pathname === "/api/user-flow/state")
          data = { state: "completed", steps: {} };
      }
      return route.fulfill({
        status: 200,
        headers,
        body: JSON.stringify(data),
      });
    });
    const page = await context.newPage();
    activePage = page;
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(origin + "/admin/positions", {
      waitUntil: "domcontentloaded",
    });
    await expectVisible(
      page.getByRole("button", { name: "新建岗位", exact: true }),
      "岗位页面加载",
    );
    await page.getByRole("button", { name: "编辑", exact: true }).click();
    const edit = page.getByRole("dialog", {
      name: "编辑岗位模板",
      exact: true,
    });
    await expectVisible(edit, "打开编辑岗位弹框");
    await expectVisible(
      edit.getByLabel("岗位名称", { exact: true }),
      "岗位名称与标签关联",
    );
    await edit.getByLabel("岗位名称", { exact: true }).fill("岗位编辑草稿");
    await edit.getByRole("button", { name: "高级设置", exact: false }).click();
    await expectVisible(
      edit.getByLabel("看详情阈值分", { exact: true }),
      "高级设置展开",
    );
    await fillNumber(edit, "看详情阈值分", 0);
    await fillNumber(edit, "打招呼阈值分", 0);
    assert.equal(
      await edit.getByLabel("索要分数", { exact: true }).inputValue(),
      "0",
    );
    checks.push("打招呼分数调整时索要分数随原值同步");
    saveDelay = 900;
    await edit.getByRole("button", { name: "保存修改", exact: true }).click();
    await expectVisible(edit, "保存中弹框保持可见");
    await page.keyboard.press("Escape");
    await expectVisible(edit, "保存中 Escape 不误关");
    await expectHidden(edit, "保存成功关闭岗位弹框");
    await page.screenshot({
      path: output + "/positions-desktop.png",
      fullPage: true,
      animations: "disabled",
    });
    const save = requests.findLast(
      (x) => x.path === "/api/positions" && x.method === "POST",
    );
    assert.equal(save.body.ai_config.detail_score_threshold, 0);
    assert.equal(save.body.ai_config.greet_score_threshold, 0);
    assert.equal(save.body.ai_config.request_score_threshold, 0);
    checks.push("0 分阈值准确提交");
    await page.getByRole("button", { name: "新建岗位", exact: true }).click();
    const create = page.getByRole("dialog", {
      name: "新建岗位模板",
      exact: true,
    });
    await expectVisible(create, "打开新增岗位弹框");
    await create.getByLabel("岗位名称", { exact: true }).fill("待取消的草稿");
    await page.keyboard.press("Escape");
    await expectHidden(create, "Escape 关闭新增弹框");
    await page.getByRole("button", { name: "新建岗位", exact: true }).click();
    await expectVisible(create, "重新打开新增弹框");
    assert.equal(
      await create.getByLabel("岗位名称", { exact: true }).inputValue(),
      "",
    );
    checks.push("新建岗位再次打开清空上次草稿");
    await create
      .getByLabel("岗位名称", { exact: true })
      .fill("嵌套预览保留草稿");
    await create.getByRole("radio", { name: /猎聘猎头端/ }).check();
    await expectVisible(
      create.getByLabel("猎聘快捷搜索名", { exact: true }),
      "切换猎聘显示快捷搜索字段",
    );
    await create
      .getByLabel("猎聘快捷搜索名", { exact: true })
      .fill("测试快捷搜索");
    await create
      .getByRole("img", {
        name: "猎聘保存搜索条件并创建快捷搜索教程",
        exact: true,
      })
      .click();
    await expectVisible(
      page.locator(".ant-image-preview").first(),
      "岗位表单内教程图片预览打开",
    );
    await page.getByRole("button", { name: "close", exact: true }).click();
    await expectHidden(
      page.locator(".ant-image-preview").first(),
      "close 只关闭图片预览",
    );
    await expectVisible(create, "关闭图片预览后岗位表单仍打开");
    assert.equal(
      await create.getByLabel("岗位名称", { exact: true }).inputValue(),
      "嵌套预览保留草稿",
    );
    checks.push("嵌套图片预览保留表单草稿");
    await create
      .getByLabel("猎聘快捷搜索名", { exact: true })
      .fill("关闭预览后可编辑");
    checks.push("关闭图片预览后父表单可继续编辑");
    await create
      .getByRole("img", {
        name: "猎聘保存搜索条件并创建快捷搜索教程",
        exact: true,
      })
      .click();
    await expectVisible(
      page.locator(".ant-image-preview").first(),
      "再次打开嵌套教程预览",
    );
    await pause(400);
    await page.keyboard.press("Escape");
    await expectHidden(
      page.locator(".ant-image-preview").first(),
      "Escape 只关闭图片预览",
    );
    await page.setViewportSize({ width: 390, height: 844 });
    await expectVisible(create, "手机视口岗位弹框保持打开");
    await page.screenshot({
      path: output + "/positions-create-mobile.png",
      fullPage: true,
      animations: "disabled",
    });
    await page.keyboard.press("Escape");
    await page.setViewportSize({ width: 1440, height: 1050 });
    await page.getByRole("button", { name: "开始", exact: true }).click();
    await expectVisible(
      page.getByRole("dialog", { name: "开始招聘岗位", exact: true }),
      "岗位启动确认打开",
    );
    await page.keyboard.press("Escape");
    await expectHidden(
      page.getByRole("dialog", { name: "开始招聘岗位", exact: true }),
      "岗位启动确认取消",
    );
    if (await page.getByRole("button", { name: "日志", exact: true }).count())
      await page.getByRole("button", { name: "日志", exact: true }).click();
    await expectVisible(
      page.getByText("本地测试日志", { exact: true }),
      "岗位日志可展开",
    );
    await page
      .getByRole("button", { name: "查看全部日志", exact: true })
      .click();
    await expectVisible(
      page.getByRole("dialog", { name: "查看全部岗位日志", exact: true }),
      "全部日志弹框打开",
    );
    await page.keyboard.press("Escape");
    await expectHidden(
      page.getByRole("dialog", { name: "查看全部岗位日志", exact: true }),
      "全部日志弹框取消",
    );
    subscription.active = false;
    subscription.allow_ai = false;
    await page.goto(origin + "/admin/positions", {
      waitUntil: "domcontentloaded",
    });
    await page.getByRole("button", { name: "新建岗位", exact: true }).click();
    await expectVisible(create, "免费会员新增岗位弹框");
    await create.getByLabel("岗位名称", { exact: true }).fill("免费会员草稿");
    await create.getByRole("radio", { name: /AI 筛选（会员功能）/ }).click();
    const memberReminder = page.getByRole("dialog", {
      name: "该功能需要订阅会员",
      exact: true,
    });
    await expectVisible(memberReminder, "岗位表单内会员提醒弹框打开");
    await memberReminder.getByRole("button", { name: /^取\s*消$/ }).click();
    await expectHidden(memberReminder, "会员提醒取消");
    await expectVisible(create, "取消会员提醒后岗位草稿仍打开");
    assert.equal(
      await create.getByLabel("岗位名称", { exact: true }).inputValue(),
      "免费会员草稿",
    );
    await expectVisible(
      create.getByLabel("关键词", { exact: true }),
      "会员提醒取消保留关键词模式",
    );
    await page.keyboard.press("Escape");
    subscription.active = true;
    subscription.allow_ai = true;
    await page.goto(origin + "/admin/subscription", {
      waitUntil: "domcontentloaded",
    });
    await expectVisible(
      page.getByRole("heading", { name: "选择会员套餐", exact: true }),
      "会员页面加载",
    );
    await page
      .getByRole("dialog", { name: "准备必要组件", exact: true })
      .waitFor({ state: "hidden", timeout: 10000 });
    await page.screenshot({
      path: output + "/subscription-desktop.png",
      fullPage: true,
      animations: "disabled",
    });
    await page.getByRole("button", { name: /当前模型/ }).click();
    const model = page.getByRole("dialog", {
      name: "选择当前模型",
      exact: true,
    });
    await expectVisible(model, "模型弹框打开");
    await model.getByRole("combobox").click();
    await page.getByText("视觉测试 · vision-test", { exact: true }).click();
    modelDelay = 900;
    await model.getByRole("button", { name: "保存模型", exact: true }).click();
    await page.keyboard.press("Escape");
    await expectVisible(model, "模型保存中禁止误关");
    await expectHidden(model, "模型保存成功关闭");
    const modelSave = requests.findLast(
      (x) => x.path === "/api/config/user-ai" && x.method === "PUT",
    );
    assert.equal(modelSave.body.model, "vision-test");
    checks.push("模型选择正确提交");
    await page.getByRole("button", { name: /^充\s*值$/ }).click();
    const recharge = page.getByRole("dialog", {
      name: "充值 AI 余额",
      exact: true,
    });
    await expectVisible(recharge, "充值弹框打开");
    await fillNumber(recharge, "充值金额（元）", 12.34);
    await page.keyboard.press("Escape");
    await expectHidden(recharge, "取消充值弹框");
    await page.getByRole("button", { name: /^充\s*值$/ }).click();
    await expectVisible(recharge, "重新打开充值弹框");
    assert.equal(
      await recharge.getByLabel("充值金额（元）", { exact: true }).inputValue(),
      "12.34",
    );
    checks.push("充值取消后草稿金额保留");
    await recharge.getByRole("button", { name: "去支付", exact: true }).click();
    await expectHidden(recharge, "支付理解步骤关闭金额弹框");
    const understanding = page.getByRole("dialog", {
      name: "支付前请确认",
      exact: true,
    });
    await expectVisible(understanding, "付款前理解弹框打开");
    paymentDelay = 900;
    await understanding
      .getByRole("button", { name: "我已理解，继续支付", exact: true })
      .click();
    await page.keyboard.press("Escape");
    await expectVisible(understanding, "创建支付订单时禁止误关");
    const qr = page.getByRole("dialog", { name: "微信扫码支付", exact: true });
    await expectVisible(qr, "二维码支付弹框打开");
    await qr.locator(".ant-qrcode svg").waitFor({ state: "visible" });
    assert.ok(await qr.locator(".ant-qrcode svg path").count());
    const logo = qr.locator(".ant-qrcode svg image");
    assert.equal(
      (await logo.getAttribute("href")) ||
        (await logo.getAttribute("xlink:href")),
      "/assets/payment/wechat-pay.png",
    );
    await page.setViewportSize({ width: 390, height: 844 });
    const qrBox = await qr.locator(".ant-qrcode").boundingBox();
    assert.ok(qrBox.x >= 0 && qrBox.x + qrBox.width <= 390);
    await page.locator(".ant-notification-notice").last().waitFor({
      state: "hidden",
      timeout: 10000,
    });
    await page.screenshot({
      path: output + "/wechat-payment-mobile.png",
      animations: "disabled",
    });
    checks.push("Ant Design 原生二维码保留微信图标且手机显示不溢出");
    await expectHidden(understanding, "订单创建后关闭理解弹框");
    await qr.getByRole("button", { name: "稍后再付", exact: true }).click();
    await expectHidden(qr, "二维码支付弹框关闭");
    await page.setViewportSize({ width: 1440, height: 1050 });
    const rechargeRequest = requests.findLast(
      (x) => x.path === "/api/payment/ai-balance" && x.method === "POST",
    );
    assert.equal(rechargeRequest.body.amount_yuan, "12.34");
    checks.push("充值小数金额准确提交");
    await page.getByRole("button", { name: "退款", exact: true }).click();
    const refund = page.getByRole("dialog", { name: "申请退款", exact: true });
    await expectVisible(refund, "退款弹框打开");
    await expectVisible(refund.getByText(/当前最多可退/), "退款额度显示");
    assert.equal(
      await refund.getByLabel("退款金额（元）", { exact: true }).inputValue(),
      "12.34",
    );
    await refund.getByRole("combobox").click();
    await page.getByText("AI 余额退款", { exact: true }).last().click();
    assert.equal(
      await refund.getByLabel("退款金额（元）", { exact: true }).inputValue(),
      "8.88",
    );
    checks.push("退款类型切换同步金额");
    refundDelay = 900;
    await refund.getByRole("button", { name: "确认退款", exact: true }).click();
    await page.keyboard.press("Escape");
    await expectVisible(refund, "退款处理中禁止误关");
    await expectHidden(refund, "退款完成关闭");
    await page.getByRole("tab", { name: "AI 使用记录", exact: true }).click();
    await expectVisible(
      page.getByText("测试AI调用", { exact: true }),
      "原生 AI 记录表显示",
    );
    await page.setViewportSize({ width: 390, height: 844 });
    await page.screenshot({
      path: output + "/subscription-mobile.png",
      fullPage: true,
    });
    await page.goto(origin + "/admin/personal-config", {
      waitUntil: "domcontentloaded",
    });
    await expectVisible(
      page.getByRole("tab", { name: "账号安全", exact: true }),
      "个人配置页面加载",
    );
    await page.getByRole("tab", { name: "账号安全", exact: true }).click();
    await expectVisible(
      page.getByLabel("当前密码", { exact: true }),
      "原生密码输入关联",
    );
    await page.getByRole("tab", { name: "随机时间", exact: true }).click();
    await expectVisible(
      page.getByRole("spinbutton", { name: "点击频率", exact: true }),
      "操作节奏原生数字输入标签",
    );
    await page.screenshot({
      path: output + "/personal-mobile.png",
      fullPage: true,
    });
    assert.deepEqual(errors, []);
    checks.push("浏览器无页面异常");
    fs.writeFileSync(
      path.join(output, "result.json"),
      JSON.stringify({ checks, requests, errors }, null, 2),
    );
    return checks;
  } catch (error) {
    if (activePage && !activePage.isClosed()) {
      try {
        await activePage.screenshot({
          path: path.join(output, "failure.png"),
          fullPage: true,
        });
        fs.writeFileSync(
          path.join(output, "failure-body.txt"),
          await activePage.locator("body").innerText(),
        );
      } catch {
        // 截图失败时保留原始测试错误，避免诊断步骤遮盖实际失败原因。
      }
    }
    fs.writeFileSync(
      path.join(output, "failure.json"),
      JSON.stringify(
        {
          message: error.message,
          stack: error.stack,
          checks,
          requests,
          errors,
        },
        null,
        2,
      ),
    );
    throw error;
  } finally {
    await browser.close();
  }
}

module.exports = { runHeavyChecks };
