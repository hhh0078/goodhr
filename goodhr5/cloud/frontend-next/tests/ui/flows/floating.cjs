/* 本文件通过全部拦截的本地接口验证真实 Chrome 置顶窗口、独立 Ant Design 样式和关闭行为，不会操作真实招聘平台。 */
const { chromium, expect } = require("@playwright/test");
const fs = require("node:fs");
const path = require("node:path");
const assert = require("node:assert/strict");

/** runFloatingChecks 在真实用户点击创建的独立窗口中验证紧凑布局、分析结果与任务状态。 */
async function runFloatingChecks(baseURL = "http://127.0.0.1:5190") {
  const origin = new URL(baseURL).origin;
  const output = path.resolve(__dirname, "../../../artifacts/antd-floating-qa");
  fs.mkdirSync(output, { recursive: true });
  const checks = [],
    calls = [],
    errors = [];
  const email = "floating@example.test";
  const position = {
    id: "floating-position",
    name: "置顶窗口测试岗位",
    platform_id: "boss",
    creator_email: email,
    status: "stopped",
    created_at: "2026-10-09T00:00:00Z",
    keywords: ["工程师"],
    match_limit: 50,
    common_config: { mode_default: "keyword", detail_mode: "ocr" },
    ai_config: {},
  };
  let task = {
    status: "stopped",
    current_step: "",
    scanned_count: 0,
    greeted_count: 0,
    skipped_count: 0,
    analysis: null,
  };
  let page, pip;
  const browser = await chromium.launch({ channel: "chrome", headless: true });
  try {
    const context = await browser.newContext({
      viewport: null,
      storageState: {
        cookies: [],
        origins: [
          {
            origin,
            localStorage: [
              { name: "goodhr5_access_token", value: "floating-test-token" },
            ],
          },
        ],
      },
    });
    context.on("page", (opened) =>
      opened.on("pageerror", (error) => errors.push(error.message)),
    );
    /** 全部业务请求使用内存响应，外部地址直接终止，保持测试与真实账号完全隔离。 */
    await context.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      const method = route.request().method();
      if (url.origin === origin) return route.continue();
      if (
        !["127.0.0.1:8084", "127.0.0.1:43129", "goodhr.58it.cn"].includes(
          url.host,
        )
      )
        return route.abort();
      const headers = {
        "access-control-allow-origin": "*",
        "access-control-allow-methods": "*",
        "access-control-allow-headers": "*",
        "content-type": "application/json",
      };
      if (method === "OPTIONS")
        return route.fulfill({ status: 204, headers, body: "" });
      calls.push({ path: url.pathname, method });
      let data = { ok: true };
      if (url.port === "43129") {
        if (url.pathname === "/health")
          data = { ok: true, version: "6.2.10.1" };
        else if (url.pathname === "/api/v1/runtime/status")
          data = {
            version: "6.2.10.1",
            node_installed: true,
            camoufox_installed: true,
            system_runtime: { ready: true, required: false },
          };
        else if (url.pathname === "/api/v1/page/url")
          data = { url: "https://mock.boss.local/chat" };
        else if (url.pathname.endsWith("/run")) {
          position.status = "running";
          task.status = "running";
        } else if (url.pathname.endsWith("/stop")) {
          position.status = "stopped";
          task.status = "stopped";
        } else if (url.pathname.endsWith("/status")) data = task;
        else if (url.pathname.endsWith("/logs")) data = { logs: [], task };
      } else {
        if (url.pathname === "/api/auth/me")
          data = {
            user: {
              id: "floating-user",
              email,
              name: "测试用户",
              tenant_id: "floating-tenant",
              is_super_admin: true,
            },
          };
        else if (url.pathname === "/api/subscription/status")
          data = {
            subscription: {
              active: true,
              member_type: "max",
              remaining_days: 30,
              remaining_seconds: 2592000,
              allow_ai: true,
              allow_auto_reply: true,
            },
          };
        else if (url.pathname === "/api/runtime/config")
          data = {
            config: {
              local_agent: [
                {
                  version: "6.2.10.1",
                  url: "https://example.test/agent.exe",
                  sha256: "floating-qa",
                },
              ],
            },
          };
        else if (url.pathname === "/api/config/notification-profile")
          data = { profile: { completed: true } };
        else if (url.pathname === "/api/user-flow/state")
          data = { state: "completed", steps: {} };
        else if (url.pathname === "/api/positions")
          data = { positions: [position] };
        else if (url.pathname === "/api/system/default-prompts")
          data = { prompts: {} };
        else if (url.pathname === "/api/platforms/config/")
          data = {
            platforms: [
              {
                platform_id: "boss",
                open: true,
                auth: {
                  pages: [{ url: "https://mock.boss.local/chat", entry: true }],
                },
                public: { pages: [] },
              },
            ],
          };
      }
      return route.fulfill({
        status: 200,
        headers,
        body: JSON.stringify(data),
      });
    });
    page = await context.newPage();
    await page.goto(origin + "/admin/positions", {
      waitUntil: "domcontentloaded",
    });

    /** startWindow 通过真实点击启动岗位并获得浏览器创建的独立置顶窗口。 */
    async function startWindow() {
      await page.getByRole("button", { name: "开始", exact: true }).click();
      const dialog = page.getByRole("dialog", {
        name: "开始招聘岗位",
        exact: true,
      });
      await expect(dialog).toBeVisible();
      await expect(
        dialog.getByRole("button", {
          name: "我已筛选好，立即开始",
          exact: true,
        }),
      ).toBeEnabled();
      const popup = page.waitForEvent("popup");
      await dialog
        .getByRole("button", { name: "我已筛选好，立即开始", exact: true })
        .click();
      pip = await popup;
      await expect(pip).toHaveTitle("GoodHR 任务状态");
      await expect(pip.getByRole("main")).toBeVisible();
      await expect(dialog).toBeHidden({ timeout: 20000 });
      await expect(
        page.getByRole("button", { name: "停止", exact: true }),
      ).toBeVisible();
      return pip;
    }
    /** verifyIndependentStyles 确认组件样式和主题变量注入独立 head，并校验真实布局与紧凑尺寸。 */
    async function verifyIndependentStyles(target) {
      const styles = (
        await target.locator("head style[data-css-hash]").allTextContents()
      ).join("\n");
      assert.match(styles, /\.ant-pip-flex/);
      assert.match(styles, /\.ant-pip-typography/);
      assert.match(styles, /\.ant-pip-tag/);
      assert.match(styles, /--ant-pip-color-text:\s*#ffffff/);
      assert.doesNotMatch(styles, /--goodhr-/);
      const mainBox = await target.getByRole("main").boundingBox();
      assert.ok(
        mainBox.width >= 260 &&
          mainBox.width <= 360 &&
          mainBox.height >= 180 &&
          mainBox.height <= 270,
        JSON.stringify(mainBox),
      );
      const headerBox = await target
        .getByText("GoodHR · 招聘小助手", { exact: true })
        .boundingBox();
      const statsBox = await target.getByLabel("本次任务统计").boundingBox();
      assert.ok(statsBox.y > headerBox.y + headerBox.height);
      checks.push(
        "独立 document 注入 Ant Design 组件和白字主题，340×250 紧凑布局",
      );
      return await target.locator("head style[data-css-hash]").count();
    }

    task.analysis = {
      kind: "ai",
      phase: "loading",
      terminal: false,
      candidate_name: "正在分析的小李",
      reason: "",
      updated_at: "2026-10-01T00:00:00Z",
    };
    task.current_step = "读取候选人信息";
    await startWindow();
    await expect(pip.getByLabel("AI 正在分析候选人")).toBeVisible();
    await expect(pip.locator(".ant-pip-spin-spinning")).toBeVisible();
    const loadingStyles = (
      await pip.locator("head style[data-css-hash]").allTextContents()
    ).join("\n");
    assert.match(loadingStyles, /@keyframes[^{}]*antSpin/);
    checks.push("真实置顶窗口展示候选人姓名和 Ant Design 转圈动画");
    await verifyIndependentStyles(pip);
    await expect(pip.getByText("扫描 0", { exact: true })).toBeVisible();
    await expect(pip.getByText("打招呼 0", { exact: true })).toBeVisible();
    await expect(pip.getByText("跳过 0", { exact: true })).toBeVisible();
    checks.push("三项统计零值正常显示");

    const longName = "超长候选人姓名".repeat(16);
    const longReason =
      "这里是完整判断原因，长文本应在分析区域滚动且不能挤掉底部统计。".repeat(
        35,
      ) + "原因结束标记";
    task.analysis = {
      kind: "ai",
      phase: "result",
      terminal: false,
      candidate_name: longName,
      score: 0,
      threshold: 0,
      accepted: false,
      reason: longReason,
      updated_at: "2026-10-01T00:00:01Z",
    };
    await expect(pip.getByLabel("AI 分数")).toHaveText("0.0", {
      timeout: 10000,
    });
    await expect(pip.getByText("分 · 阈值 0.0", { exact: true })).toBeVisible();
    await expect(pip.getByText("运行中 · 跳过", { exact: true })).toBeVisible();
    assert.equal(
      await pip.getByTitle(longName, { exact: true }).textContent(),
      longName,
    );
    assert.equal(
      await pip.getByTitle(longReason, { exact: true }).textContent(),
      longReason,
    );
    await expect(pip.getByLabel("本次任务统计")).toBeVisible();
    await expect(
      pip.getByText("当前：读取候选人信息", { exact: true }),
    ).toBeVisible();
    const nameBox = await pip
      .getByTitle(longName, { exact: true })
      .boundingBox();
    const scoreBox = await pip.getByLabel("AI 分数").boundingBox();
    assert.ok(nameBox.y + nameBox.height <= scoreBox.y + 1);
    checks.push("AI 零分、零阈值、跳过状态和完整长文本保留，姓名位于分数上方");
    await pip.screenshot({ path: path.join(output, "ai-zero-long.png") });
    const stableStyleCount = await verifyIndependentStyles(pip);
    task.current_step = "进行下一步普通浏览器操作";
    await expect(
      pip.getByText("当前：进行下一步普通浏览器操作", { exact: true }),
    ).toBeVisible({ timeout: 10000 });
    assert.equal(
      await pip.locator("head style[data-css-hash]").count(),
      stableStyleCount,
    );
    await expect(pip.getByLabel("AI 分数")).toHaveText("0.0");
    checks.push("普通步骤更新保留分析结果，重复渲染不重复注入样式");

    task.analysis = {
      kind: "keyword",
      phase: "result",
      terminal: false,
      candidate_name: "关键词候选人",
      reason: "匹配规则结果",
      keywords: ["工程师", "React"],
      matched_keywords: [],
      exclude_keywords: ["实习"],
      matched_excludes: ["实习"],
      updated_at: "2026-10-01T00:00:02Z",
    };
    await expect(
      pip.getByText("需要：工程师、React", { exact: true }),
    ).toBeVisible({ timeout: 10000 });
    await expect(pip.getByText("排除：实习", { exact: true })).toBeVisible();
    await expect(pip.getByText("命中：暂无", { exact: true })).toBeVisible();
    await expect(
      pip.getByText("命中排除：实习", { exact: true }),
    ).toBeVisible();
    checks.push("关键词、排除词和空命中结果显示完整");
    task.analysis = {
      kind: "ai",
      phase: "error",
      terminal: false,
      candidate_name: "分析失败候选人",
      reason: "测试连接失败原因",
      updated_at: "2026-10-01T00:00:03Z",
    };
    await expect(
      pip.getByText("测试连接失败原因", { exact: true }),
    ).toBeVisible({ timeout: 10000 });
    await expect(pip.locator(".ant-pip-spin-spinning")).toBeHidden();
    checks.push("分析失败保留原因并结束转圈");
    await page.getByRole("button", { name: "停止", exact: true }).click();
    await expect(pip.getByText("已停止", { exact: true })).toBeVisible();
    assert.match(
      await pip.getByRole("main").getAttribute("style"),
      /background:\s*rgb\(179, 67, 67\)/,
    );
    await expect(
      pip.getByText("当前：任务已按你的要求停止", { exact: true }),
    ).toBeVisible();
    checks.push("停止任务后小窗转红，分析原因继续保留");
    await pip.screenshot({ path: path.join(output, "stopped.png") });
    await pip.close();

    task.analysis = null;
    await startWindow();
    await expect(pip.getByText("等待候选人", { exact: true })).toBeVisible();
    await verifyIndependentStyles(pip);
    assert.match(
      await pip.getByRole("main").getAttribute("style"),
      /background:\s*rgb\(47, 125, 84\)/,
    );
    checks.push("再次创建独立窗口恢复样式和绿色运行状态，空分析正常显示");
    const stopCalls = calls.filter((call) =>
      call.path.endsWith("/stop"),
    ).length;
    await pip.close();
    await expect(
      page.getByRole("button", { name: "停止", exact: true }),
    ).toBeVisible();
    assert.equal(
      calls.filter((call) => call.path.endsWith("/stop")).length,
      stopCalls,
    );
    checks.push("关闭置顶窗口不调用停止接口，主页面任务继续运行");
    await page.getByRole("button", { name: "停止", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "开始", exact: true }),
    ).toBeVisible();

    await startWindow();
    task.status = "completed";
    await expect(pip.getByText("已完成", { exact: true })).toBeVisible({
      timeout: 10000,
    });
    await expect(
      pip.getByText("当前：这轮候选人已经处理完成", { exact: true }),
    ).toBeVisible();
    checks.push("完成状态和完成说明显示正确");
    await pip.close();
    await page.getByRole("button", { name: "停止", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "开始", exact: true }),
    ).toBeVisible();

    await startWindow();
    task.status = "failed";
    await expect(pip.getByText("运行失败", { exact: true })).toBeVisible({
      timeout: 10000,
    });
    assert.match(
      await pip.getByRole("main").getAttribute("style"),
      /background:\s*rgb\(143, 47, 47\)/,
    );
    await expect(
      pip.getByText("当前：这次没跑顺，请回岗位日志看看原因", { exact: true }),
    ).toBeVisible();
    checks.push("运行失败显示红色和下一步说明");
    assert.deepEqual(errors, []);
    checks.push("所有窗口与主页面均无运行时错误");
    fs.writeFileSync(
      path.join(output, "result.json"),
      JSON.stringify({ checks, calls, errors }, null, 2),
    );
    return checks;
  } catch (error) {
    fs.writeFileSync(
      path.join(output, "result.json"),
      JSON.stringify({ error: String(error), checks, calls, errors }, null, 2),
    );
    if (page && !page.isClosed())
      await page
        .screenshot({ path: path.join(output, "failure-main.png") })
        .catch(() => undefined);
    if (pip && !pip.isClosed()) {
      await pip
        .screenshot({ path: path.join(output, "failure-pip.png") })
        .catch(() => undefined);
      fs.writeFileSync(
        path.join(output, "failure-pip-styles.txt"),
        (await pip.locator("head style").allTextContents()).join("\n"),
      );
    }
    throw error;
  } finally {
    await browser.close();
  }
}

module.exports = { runFloatingChecks };
