/** 文件作用说明：完全模拟云端和本地接口，验证团队招聘设置、面试动作、抄送权限与候选人邀请闭环。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const previewOrigin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
test.use({

  storageState: {

    cookies: [],
    origins: [
      {

        origin: previewOrigin,
        localStorage: [{ name: "goodhr5_access_token", value: "recruitment-test-only" }]
      }
    ]
  }
});
type Call = { path: string; method: string; body: any; query: URLSearchParams };
/** mockRecruitment 将所有业务请求截到合成数据，禁止发送真实邮件或改动真实候选人。 */
async function mockRecruitment(page: Page, readOnly = false) {

  const node = {

    id: "node-interview",
    name: "初面",
    type: "interview",
    owner_email: "hr@example.invalid",
    interviewer_emails: ["hr@example.invalid"],
    approver_emails: ["reviewer@example.invalid"],
    cc_emails: ["cc@example.invalid"],
    approval_mode: "all",
    requires_feedback: true,
    mail_template_id: "mail-first",
    status: "active",
    attempt: 1,
    approvals: []
  };

  const state = {

    calls: [] as Call[],
    errors: [] as string[],
    canManage: !readOnly,
    canSend: true,
    uncertain: false,
    failed: false,
    previewVersion: 1,
    recipient: "candidate@example.invalid",
    company: {

      company_name: "合成公司",
      contact_name: "测试 HR",
      contact_email: "hr@example.invalid",
      contact_phone: "",
      company_address: ""
    },
    workflows: [{

      id: "flow-default",
      name: "团队默认流程",
      version: 1,
      revision: 1,
      is_default: true,
      nodes: [node]
    }] as any[],
    templates: [
      {

        id: "mail-first",
        name: "初面邀请",
        category: "interview_first",
        subject: "{公司名称} · {岗位名称}面试邀请",
        body: "{简历姓名}，你好：\n面试时间：{面试时间}\n联系人：{招聘联系人}",
        is_builtin: true
      }
    ] as any[],
    deliveries: [] as any[],
    app: {

      id: "application-one",
      candidate_id: "candidate-one",
      candidate_name: "合成候选人",
      candidate_email: "candidate@example.invalid",
      position_id: "position-one",
      position_name: "产品经理",
      workflow_id: "flow-default",
      workflow_name: "团队默认流程",
      workflow_version: 1,
      owner_email: "hr@example.invalid",
      status: "active",
      stage: "初面",
      current_node: 0,
      revision: 1,
      nodes: [node],
      interviews: [] as any[],
      events: [
        {

          id: "event-start",
          type: "started",
          note: "初面待安排，已抄送",
          notify_emails: [readOnly ? "cc@example.invalid" : "hr@example.invalid"],
          read_by: [] as string[],
          author_email: "hr@example.invalid",
          created_at: "2026-10-10T08:00:00Z"
        }
      ],
      permissions: {

        can_manage: !readOnly,
        can_assign: !readOnly,
        can_view_resume: !readOnly,
        can_advance: !readOnly,
        can_schedule: !readOnly,
        can_feedback: !readOnly,
        can_approve: !readOnly
      }
    },
    confirmation: "pending"
  };

  page.on("pageerror", (error) => state.errors.push(error.message));

  await page.context().route("**/*", async (route) => {

    const request = route.request();
    const url = new URL(request.url());
    const method = request.method();

    if (url.origin === previewOrigin && !url.pathname.startsWith("/api/")) return route.continue();

    if (!url.pathname.startsWith("/api/") && url.pathname !== "/health") return route.abort();

    const headers = {

      "access-control-allow-origin": "*",
      "access-control-allow-headers": "*",
      "access-control-allow-methods": "*"
    };

    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });

    let body: any;
    try {

      body = request.postDataJSON();
    } catch {
      /* GET 没有正文。 */
    }

    state.calls.push({ path: url.pathname, method, body, query: url.searchParams });

    let data: any = { ok: true };

    if (url.pathname === "/health") data = { ok: true, version: "6.2.10.1" };
    else if (url.pathname === "/api/auth/me") data = {

      user: {

        id: "hr-test",
        email: readOnly ? "cc@example.invalid" : "hr@example.invalid",
        role: readOnly ? "user" : "super_admin",
        tenant_id: "test-team",
        password_set: true
      }
    };
    else if (url.pathname === "/api/subscription/status") data = {

      subscription: {

        active: true,
        member_type: "plus",
        member_name: "Plus",
        remaining_days: 30,
        allow_ai: true,
        allow_auto_reply: true
      }
    };
    else if (url.pathname === "/api/config/notification-profile") data = { profile: { completed: true } };
    else if (url.pathname === "/api/v1/runtime/status") data = {

      version: "6.2.10.1",
      node_installed: true,
      camoufox_installed: true,
      system_runtime: { ready: true, required: false }
    };
    else if (url.pathname === "/api/v1/app-update/status") data = { stage: "idle", current_version: "6.2.10.1" };
    else if (url.pathname === "/api/runtime/config" || url.pathname === "/api/system/app-config") data = { config: {} };
    else if (url.pathname === "/api/user-flow") data = { flow: { version: 2, state: "completed", stage: "completed", steps: {} } };
    else if (url.pathname === "/api/ai-wallet") data = { wallet: { balance: "100", models: [], default_model: "test" } };
    else if (url.pathname === "/api/positions") data = { positions: [{ id: "position-one", name: "产品经理" }], total: 1 };
    else if (url.pathname === "/api/candidates") data = {

      candidates: [
        {

          id: "candidate-one",
          candidate_name: "合成候选人",
          email: "candidate@example.invalid",
          phone: "13900139000"
        }
      ],
      total: 1
    };
    else if (url.pathname === "/api/recruitment/workflows") {

      if (method === "POST") state.workflows.push({ ...body, id: "flow-custom", version: 1, revision: 1 });
      data = {

        workflows: state.workflows,
        members: ["hr", "reviewer", "cc"].map((name) => ({ email: `${name}@example.invalid`, status: "active" })),
        can_manage: state.canManage
      };
    }
    else if (url.pathname.startsWith("/api/recruitment/workflows/")) {

      state.workflows[0] = { ...state.workflows[0], ...body };
      data = { workflow: state.workflows[0] };
    }
    else if (url.pathname === "/api/recruitment/company") {

      if (method === "PUT") state.company = body;
      data = { company: state.company, can_manage: state.canManage };
    }
    else if (url.pathname === "/api/recruitment/mail-templates") {

      if (method === "POST") state.templates.push({ ...body, id: "mail-custom" });
      data = { templates: state.templates, can_manage: state.canManage };
    }
    else if (url.pathname.startsWith("/api/recruitment/mail-templates/")) {

      state.templates[0] = { ...state.templates[0], ...body };
      data = { template: state.templates[0] };
    }
    else if (url.pathname === "/api/recruitment/placeholders") data = {

      placeholders: ["公司名称", "岗位名称", "简历姓名", "面试时间", "招聘联系人", "联系邮箱"].map((key) => ({ key, label: key, example: `示例${key}`, group: "基础" }))
    };
    else if (url.pathname === "/api/recruitment/mail-preview") {

      const render = (value: string) => String(value || "").replace(/\{([^}]+)\}/g, (_, key) => ({

        公司名称: "合成公司",
        岗位名称: "产品经理",
        简历姓名: "合成候选人",
        招聘联系人: "测试 HR",
        面试时间: "2026-10-11 10:00",
        联系邮箱: "hr@example.invalid"
      } as Record<string, string>)[key] || `示例${key}`);
      data = {

        preview: {

          subject: `[合成公司] ${render(body.subject)}`,
          body: render(body.body),
          recipient: state.recipient,
          reply_to: "hr@example.invalid",
          context_fingerprint: body.application_id ? `context-${state.previewVersion}` : undefined,
          is_example: !body.application_id,
          can_send: Boolean(body.application_id && state.canSend),
          missing_placeholders: state.canSend ? [] : ["公司名称"],
          unknown_placeholders: []
        }
      };
    }
    else if (url.pathname === "/api/recruitment/mail-history") data = { deliveries: state.deliveries };
    else if (url.pathname === "/api/recruitment/mail-send") {
      if (body.context_fingerprint !== `context-${state.previewVersion}`) {
        return route.fulfill({ status: 409, headers, json: { ok: false, code: "recruitment_preview_changed", error: "收件人已变化，请重新预览后再确认。" } });
      }
      const delivery = {

        id: "delivery-one",
        application_id: state.app.id,
        interview_id: body.interview_id,
        template_id: body.template_id,
        recipient: state.recipient,
        subject: body.subject,
        status: state.uncertain ? "uncertain" : state.failed ? "failed" : "sent",
        created_at: "2026-10-10T08:00:00Z"
      };
      state.deliveries.push(delivery);
      data = { delivery };
    }
    else if (url.pathname === "/api/recruitment/applications") data = method === "POST" ? { application: state.app } : { applications: [state.app] };
    else if (url.pathname.endsWith("/actions")) {

      state.app.revision++;
      if (body.action === "schedule") state.app.interviews.push({

        ...body,
        id: "interview-one",
        node_id: node.id,
        attempt: 1,
        status: "scheduled",
        confirmation: "pending",
        feedback: [],
        revision: 1
      });
      if (body.action === "feedback") state.app.interviews[0].feedback.push({ ...body, author_email: "hr@example.invalid" });
      if (body.action === "read_notifications") state.app.events[0].read_by.push(readOnly ? "cc@example.invalid" : "hr@example.invalid");
      data = {

        application: state.app,
        candidate: {

          id: "candidate-one",
          candidate_name: "合成候选人",
          basic_info: "五年产品经验",
          work_experiences: [{ company_name: "合成原公司", position_name: "产品经理", content: "负责需求与交付" }]
        }
      };
    }
    else if (url.pathname === "/api/recruitment/applications/application-one") data = {

      application: state.app,
      candidate: {

        id: "candidate-one",
        candidate_name: "合成候选人",
        basic_info: "五年产品经验",
        work_experiences: [{ company_name: "合成原公司", position_name: "产品经理", content: "负责需求与交付" }]
      }
    };
    else if (url.pathname.startsWith("/api/public/recruitment/invitations/")) {

      if (method === "POST") state.confirmation = body.action === "confirm" ? "confirmed" : body.action === "reschedule" ? "reschedule_requested" : "declined";
      data = method === "POST" ? { confirmation: state.confirmation, message: "你的答复已送到招聘联系人。" } : {

        invitation: {

          candidate_name: "合成候选人",
          position_name: "产品经理",
          starts_at: "2026-10-11T02:00:00Z",
          duration_minutes: 60,
          mode: "video",
          meeting_url: "https://example.invalid/meeting",
          confirmation: state.confirmation
        }
      };
    }
    else if (url.pathname.includes("/invitations")) data = { invitations: [] };

    return route.fulfill({ status: 200, headers, json: data });

  });
  return state;
}

test("企业招聘单行工具栏设置弹框共用旧内容，保存公司流程邮件后关闭仍留在列表", async ({ page }) => {
  const state = await mockRecruitment(page);
  await page.setViewportSize({ width: 1920, height: 1000 });
  await page.goto("/admin/recruitment");
  await expect(page.getByRole("menuitem", { name: "招聘设置", exact: true })).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "企业招聘", exact: true })).toHaveCount(0);
  const toolbar = page.getByTestId("recruitment-toolbar");
  const create = toolbar.getByRole("button", { name: "开启招聘", exact: true });
  const refresh = toolbar.getByRole("button", { name: /刷新$/ });
  const settingsButton = toolbar.getByRole("button", { name: "招聘设置", exact: true });
  const createBox = await create.boundingBox();
  for (const control of [refresh, settingsButton, toolbar.getByLabel("搜索招聘进度", { exact: true }), toolbar.getByLabel("筛选招聘岗位", { exact: true }), toolbar.getByRole("tablist")]) {
    const controlBox = await control.boundingBox();
    expect(controlBox).not.toBeNull();
    expect(Math.abs(controlBox!.y + controlBox!.height / 2 - createBox!.y - createBox!.height / 2)).toBeLessThan(12);
  }
  await refresh.click();
  const screenshotDirectory = resolve("../../../output/resume-folders");
  mkdirSync(screenshotDirectory, { recursive: true });
  await page.screenshot({ path: resolve(screenshotDirectory, "recruitment-toolbar.png"), fullPage: true, animations: "disabled" });
  await settingsButton.click();
  const settings = page.getByRole("dialog", { name: "招聘设置", exact: true });
  await expect(settings).toBeVisible();
  await expect(page).toHaveURL(/\/admin\/recruitment$/);
  await settings.getByLabel("公司名称", { exact: true }).fill("弹框合成公司");
  await settings.getByRole("button", { name: "保存公司信息", exact: true }).click();
  await expect.poll(() => state.company.company_name).toBe("弹框合成公司");
  await expect(page.locator(".ant-notification-notice")).toHaveCount(0);
  await page.screenshot({ path: resolve(screenshotDirectory, "recruitment-settings-dialog.png"), fullPage: true, animations: "disabled" });
  await settings.getByRole("tab", { name: "流程模板", exact: true }).click();
  await settings.getByRole("button", { name: "新建流程", exact: true }).click();
  const workflow = page.getByRole("dialog", { name: "新建招聘流程", exact: true });
  await workflow.getByLabel("流程名称", { exact: true }).fill("弹框两轮面试");
  await workflow.getByRole("button", { name: "保存", exact: true }).click();
  await expect(workflow).toBeHidden();
  await expect.poll(() => state.workflows.length).toBe(2);
  await settings.getByRole("tab", { name: "邮件模板", exact: true }).click();
  await settings.getByRole("button", { name: "编辑", exact: true }).click();
  const mail = page.getByRole("dialog", { name: "编辑邮件模板", exact: true });
  await mail.getByLabel("邮件模板正文", { exact: true }).fill("弹框邀请：{简历姓名}");
  await mail.getByRole("button", { name: "保存", exact: true }).click();
  await expect(mail).toBeHidden();
  await expect.poll(() => state.templates[0].body).toBe("弹框邀请：{简历姓名}");
  await settings.locator(".ant-modal-footer").getByRole("button", { name: "关闭", exact: true }).click();
  await expect(settings).toBeHidden();
  await expect(page).toHaveURL(/\/admin\/recruitment$/);
  await create.click();
  const start = page.getByRole("dialog", { name: "开启企业招聘", exact: true });
  await start.getByLabel("招聘流程模板", { exact: true }).click();
  await expect(page.locator(".ant-select-item-option").filter({ hasText: "弹框两轮面试" })).toBeVisible();
  await start.getByRole("button", { name: "取消", exact: true }).click();
  await expect(start).toBeHidden();
  await page.goto("/admin/recruitment/settings");
  await expect(page.getByRole("heading", { name: "招聘设置", exact: true })).toBeVisible();
  await expect(page.getByLabel("公司名称", { exact: true })).toHaveValue("弹框合成公司");
  expect(state.errors).toEqual([]);
});

test("团队可另存多套流程，并修改预置邮件、插入中文变量、实时预览", async ({ page }) => {

  const state = await mockRecruitment(page);
  await page.goto("/admin/recruitment/settings");

  await page.getByRole("tab", { name: "流程模板", exact: true }).click();
  await page.getByRole("button", { name: "新建流程", exact: true }).click();

  const workflow = page.getByRole("dialog", { name: "新建招聘流程", exact: true });
  await workflow.getByLabel("流程名称", { exact: true }).fill("技术岗位两轮面试");
  await workflow.getByRole("button", { name: /添加节点$/ }).click();
  await workflow.getByRole("button", { name: "保存", exact: true }).click();

  await expect.poll(() => state.workflows.length).toBe(2);
  expect(state.calls.find((call) => call.path === "/api/recruitment/workflows" && call.method === "POST")?.body.nodes).toHaveLength(6);

  await page.getByRole("tab", { name: "邮件模板", exact: true }).click();
  await page.getByRole("button", { name: "编辑", exact: true }).click();
  const mail = page.getByRole("dialog", { name: "编辑邮件模板", exact: true });
  await mail.getByLabel("邮件模板正文", { exact: true }).fill("你好，");
  await mail.getByRole("button", { name: "{简历姓名}", exact: true }).click();
  await expect(mail.getByLabel("邮件模板正文", { exact: true })).toHaveValue("你好，{简历姓名}");
  await expect(mail.getByTestId("mail-template-live-preview")).toHaveText("你好，合成候选人");
  await mail.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => state.templates[0].body).toBe("你好，{简历姓名}");
  await page.getByRole("button", { name: "新建邮件模板", exact: true }).click();
  const custom = page.getByRole("dialog", { name: "新建邮件模板", exact: true });
  await expect(custom.getByLabel("邮件模板正文", { exact: true })).toHaveValue(/[{]简历姓名[}]/);
  await custom.getByLabel("邮件模板名称", { exact: true }).fill("公司自定义邀请");
  await custom.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => state.templates.length).toBe(2);
  expect(state.errors).toEqual([]);
});

test("面试排期和独立面评提交准确字段，邮件缺资料不能发送", async ({ page }) => {

  const state = await mockRecruitment(page);
  await page.goto("/admin/recruitment");
  await page.getByRole("button", { name: "查看进度", exact: true }).click();
  const detail = page.getByRole("dialog", { name: "合成候选人 · 产品经理", exact: true });
  await detail.getByRole("button", { name: "安排面试", exact: true }).click();
  const schedule = page.getByRole("dialog", { name: "安排面试", exact: true });
  await schedule.getByLabel("面试时间", { exact: true }).fill("2026-10-11T10:00");
  await schedule.getByLabel("会议链接", { exact: true }).fill("https://example.invalid/meeting");
  await schedule.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => state.app.interviews.length).toBe(1);
  const call = state.calls.find((item) => item.body?.action === "schedule")!;
  expect(call.body).toMatchObject({

    revision: 1,
    mode: "video",
    duration_minutes: 60,
    interviewer_emails: ["hr@example.invalid"]
  });

  state.app.interviews[0].starts_at = new Date(Date.now() - 60000).toISOString();
  await detail.getByRole("button", { name: "刷新进度", exact: true }).click();
  await detail.getByRole("button", { name: "写面试评价", exact: true }).click();
  const feedback = page.getByRole("dialog", { name: "面试评价", exact: true });
  await feedback.getByLabel("招聘操作说明", { exact: true }).fill("需求表达清晰，建议业务复试。");
  await feedback.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => state.app.interviews[0].feedback.length).toBe(1);
  expect(state.calls.find((item) => item.body?.action === "feedback")?.body).toMatchObject({

    revision: 2,
    interview_id: "interview-one",
    conclusion: "pass",
    content: "需求表达清晰，建议业务复试。"
  });

  state.canSend = false;
  await detail.getByRole("button", { name: "预览并发送邮件", exact: true }).click();
  const mail = page.getByRole("dialog", { name: "预览并发送招聘邮件", exact: true });
  await expect(mail.getByText("先补上：公司名称", { exact: true })).toBeVisible();
  await expect(mail.getByRole("button", { name: "确认发送", exact: true })).toBeDisabled();
  expect(state.calls.filter((item) => item.path.endsWith("/mail-send"))).toHaveLength(0);
  expect(state.errors).toEqual([]);
});

test("抄送成员只读查看并标记通知已读，不能推进或发信", async ({ page }) => {

  const state = await mockRecruitment(page, true);
  await page.goto("/admin/recruitment");
  await page.getByRole("tab", { name: "我的待办与抄送", exact: true }).click();
  await expect.poll(() => state.calls.some((call) => call.path === "/api/recruitment/applications" && call.query.get("mine") === "true")).toBe(true);
  await page.getByRole("button", { name: "查看进度", exact: true }).click();
  const detail = page.getByRole("dialog", { name: "合成候选人 · 产品经理", exact: true });
  await expect(detail.getByRole("button", { name: "推进下一步", exact: true })).toHaveCount(0);
  await expect(detail.getByRole("button", { name: "预览并发送邮件", exact: true })).toHaveCount(0);
  await detail.getByRole("button", { name: "标记通知已读", exact: true }).click();
  await expect.poll(() => state.app.events[0].read_by.includes("cc@example.invalid")).toBe(true);

  expect(state.calls.filter((call) => call.path === "/api/recruitment/mail-history")).toHaveLength(0);
  await detail.getByRole("button", { name: "招聘资料", exact: true }).click();
  await expect(detail.getByText("合成原公司 · 产品经理", { exact: true })).toBeVisible();
  await expect(detail.getByRole("link", { name: "查看完整简历", exact: true })).toHaveCount(0);
  await detail.getByRole("button", { name: "关闭", exact: true }).last().click();
  await page.goto("/admin/recruitment/settings");
  await expect(page.getByLabel("公司名称", { exact: true })).toBeDisabled();
  await page.getByRole("tab", { name: "流程模板", exact: true }).click();
  await expect(page.getByRole("button", { name: "新建流程", exact: true })).toHaveCount(0);
  expect(state.errors).toEqual([]);
});

test("邮件发送结果不明确时停止重复发送", async ({ page }) => {

  const state = await mockRecruitment(page);
  state.uncertain = true;
  await page.goto("/admin/recruitment");
  await page.getByRole("button", { name: "查看进度", exact: true }).click();
  await page.getByRole("dialog", { name: "合成候选人 · 产品经理", exact: true }).getByRole("button", { name: "预览并发送邮件", exact: true }).click();
  const mail = page.getByRole("dialog", { name: "预览并发送招聘邮件", exact: true });
  await expect(mail.getByRole("button", { name: "确认发送", exact: true })).toBeEnabled();
  await mail.getByRole("button", { name: "确认发送", exact: true }).click();
  await expect(mail.getByRole("button", { name: "确认发送", exact: true })).toBeDisabled();
  await expect.poll(() => state.calls.filter((item) => item.path.endsWith("/mail-send")).length).toBe(1);
  expect(state.calls.find((item) => item.path.endsWith("/mail-send"))?.body.idempotency_key).toBeTruthy();
  expect(state.errors).toEqual([]);
});

test("候选人确认页面保护邀请令牌，改期申请保留候选人的说明", async ({ page }) => {

  const state = await mockRecruitment(page);
  const thirdPartyRequests: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.origin !== previewOrigin && !["127.0.0.1", "localhost"].includes(url.hostname)) thirdPartyRequests.push(request.url());
  });
  await page.goto("/interview-invitation/test-only-token");
  await expect(page.getByText("产品经理", { exact: true })).toBeVisible();
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute("content", /noindex.*nofollow/);
  await expect(page.locator('meta[name="googlebot"]')).toHaveAttribute("content", /noindex.*nofollow/);
  await expect(page.locator('meta[name="referrer"]')).toHaveAttribute("content", "no-referrer");
  await expect(page.locator('#baidu-analytics, script[src*="hm.baidu.com"]')).toHaveCount(0);
  await page.getByRole("button", { name: "申请改期", exact: true }).click();
  expect(state.calls.filter((call) => call.path.startsWith("/api/public/") && call.method === "POST")).toHaveLength(0);
  await page.getByLabel("面试答复说明", { exact: true }).fill("周一下午方便，谢谢。");
  await page.getByRole("button", { name: "申请改期", exact: true }).click();
  await expect(page.getByText("你的答复已送到招聘联系人。", { exact: true })).toBeVisible();
  expect(state.calls.find((call) => call.path.startsWith("/api/public/") && call.method === "POST")?.body).toEqual({ action: "reschedule", note: "周一下午方便，谢谢。" });
  expect(state.calls.some((call) => call.path === "/api/recruitment/applications")).toBe(false);
  expect(thirdPartyRequests).toEqual([]);
  expect(state.errors).toEqual([]);
});

test("已结束流程仍可发结果通知，明确失败后的人工重发使用新编号", async ({ page }) => {

  const state = await mockRecruitment(page);
  state.app.status = "completed";
  state.failed = true;

  await page.goto("/admin/recruitment");
  await page.getByRole("button", { name: "查看进度", exact: true }).click();

  const detail = page.getByRole("dialog", { name: "合成候选人 · 产品经理", exact: true });
  await expect(detail.getByRole("button", { name: "推进下一步", exact: true })).toHaveCount(0);

  await detail.getByRole("button", { name: "预览并发送邮件", exact: true }).click();
  const mail = page.getByRole("dialog", { name: "预览并发送招聘邮件", exact: true });
  await expect(mail.getByRole("button", { name: "确认发送", exact: true })).toBeEnabled();

  await mail.getByRole("button", { name: "确认发送", exact: true }).click();
  await expect(mail.getByRole("button", { name: "重新发送", exact: true })).toBeVisible();
  const original = state.calls.find((item) => item.path.endsWith("/mail-send"))?.body.idempotency_key;

  state.failed = false;
  await mail.getByRole("button", { name: "重新发送", exact: true }).click();
  await expect.poll(() => state.calls.filter((item) => item.path.endsWith("/mail-send")).length).toBe(2);
  const retry = state.calls.filter((item) => item.path.endsWith("/mail-send"))[1].body.idempotency_key;

  expect(retry).not.toBe(original);
  expect(state.errors).toEqual([]);
});

test("面试默认选择邀请模板，预览后收件人变化需重新核对并再次确认", async ({ page }) => {
  const state = await mockRecruitment(page);
  state.app.nodes[0].mail_template_id = "";
  state.templates.unshift({ id: "mail-offer", name: "Offer 通知", category: "offer", subject: "录用通知", body: "Offer 正文" });
  await page.goto("/admin/recruitment");
  await page.getByRole("button", { name: "查看进度", exact: true }).click();
  const detail = page.getByRole("dialog", { name: "合成候选人 · 产品经理", exact: true });
  await detail.getByRole("button", { name: "预览并发送邮件", exact: true }).click();
  const mail = page.getByRole("dialog", { name: "预览并发送招聘邮件", exact: true });
  await expect(mail.getByLabel("本次邮件标题", { exact: true })).toHaveValue("{公司名称} · {岗位名称}面试邀请");
  await expect(mail.getByRole("button", { name: "确认发送", exact: true })).toBeEnabled();
  await expect(mail.getByText(/收件人：candidate@example.invalid/)).toBeVisible();

  state.previewVersion = 2;
  state.recipient = "updated@example.invalid";
  await mail.getByRole("button", { name: "确认发送", exact: true }).click();
  await expect(mail.getByText(/收件人：updated@example.invalid/)).toBeVisible();
  await expect(mail.getByRole("button", { name: "确认发送", exact: true })).toBeEnabled();
  expect(state.calls.filter((item) => item.path.endsWith("/mail-send"))).toHaveLength(1);
  expect(state.deliveries).toHaveLength(0);

  await mail.getByRole("button", { name: "确认发送", exact: true }).click();
  await expect.poll(() => state.deliveries.length).toBe(1);
  const sends = state.calls.filter((item) => item.path.endsWith("/mail-send"));
  expect(sends[0].body.template_id).toBe("mail-first");
  expect(sends[0].body.context_fingerprint).toBe("context-1");
  expect(sends[1].body.context_fingerprint).toBe("context-2");
  expect(sends[1].body.idempotency_key).toBe(sends[0].body.idempotency_key);
  expect(state.deliveries[0].recipient).toBe("updated@example.invalid");
  expect(state.errors).toEqual([]);
});
