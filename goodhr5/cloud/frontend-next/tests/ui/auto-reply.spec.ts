/** 文件作用说明：用全拦截的合成接口验证自动回复配置、全局启停、刷新恢复与桌面手机布局。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const output = resolve(process.env.UI_TEST_SCREENSHOT_DIR || "output/auto-reply-20261030");
test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "auto-reply-synthetic-token" }] }] } });

/** fixture 在内存中模拟岗位配置和本地任务，未列入的写操作不会触达任何服务。 */
async function fixture(page: Page) {
  const base = await mockUniformAdmin(page);
  const state = { running: false, starts: 0, stops: 0, config: { position_id: "uniform-position-1", company_profile_id: "company-one", enabled: false, position_description: "合成岗位说明", resume_request_message: "你好，方便发一份简历吗？", conditions: [], poll_interval_seconds: 5, max_threads_per_checkpoint: 3, version: 1 }, profiles: [{ id: "company-one", name: "合成公司", address: "合成测试地址", contact: "", overview: "合成公司介绍", extra_info: "", updated_at: "2026-10-30T08:00:00Z" }] };
  await page.route(/\/api\/(?:auto-reply\/|v1\/local\/auto-reply\/)/, async (route) => {
    const request = route.request(); const path = new URL(request.url()).pathname; const method = request.method();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    let data: unknown;
    if (path.endsWith("/auto-reply/start")) { state.starts++; state.running = true; data = { ok: true, data: { status: "running", task_id: "synthetic-auto-reply" } }; }
    else if (path.endsWith("/auto-reply/stop")) { state.stops++; state.running = false; data = { ok: true, data: { status: "stopped" } }; }
    else if (path.endsWith("/auto-reply/status")) data = { ok: true, data: { status: state.running ? "running" : "stopped", current_step: "合成任务状态" } };
    else if (/\/positions\/[^/]+\/status$/.test(path)) data = { configured_enabled: state.config.enabled, enabled: state.config.enabled, version: 1, subscription: { allow_auto_reply: true } };
    else if (path.endsWith("/config")) {
      if (method === "PUT") state.config = { ...state.config, ...request.postDataJSON() };
      data = { config: state.config };
    } else if (path.endsWith("/company-profiles")) data = { company_profiles: state.profiles };
    else if (path.endsWith("/company-profiles/company-one") && method === "PUT") data = { company_profile: { ...state.profiles[0], ...request.postDataJSON() } };
    else throw new Error(`自动回复测试遗漏接口 ${method} ${path}`);
    return route.fulfill({ headers, json: data });
  });
  return { state, base };
}

for (const width of [1366, 390]) {
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
    await expect(dialog.getByText("聊天和附件留本机；岗位里将“是否生成简历”选为“需要”，资料和推荐报告才会进简历库。")).toBeVisible();
    mkdirSync(output, { recursive: true });
    await page.screenshot({ path: resolve(output, `config-${width}.png`), fullPage: true, animations: "disabled" });
    await dialog.getByRole("button", { name: "保存配置", exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(state.config.position_description).toContain("合成新说明"); expect(state.config.enabled).toBe(true);
    await page.getByRole("button", { name: "开始自动回复", exact: true }).click();
    await expect(page.getByRole("button", { name: "停止自动回复", exact: true })).toBeVisible(); expect(state.starts).toBe(1);
    await page.reload();
    await page.getByRole("button", { name: "停止自动回复", exact: true }).click();
    await expect(page.getByRole("button", { name: "开始自动回复", exact: true })).toBeVisible(); expect(state.stops).toBe(1);
    await page.screenshot({ path: resolve(output, `positions-${width}.png`), fullPage: true, animations: "disabled" });
    const create = await page.getByRole("button", { name: "新建岗位", exact: true }).boundingBox();
    expect(create!.x + create!.width).toBeLessThanOrEqual(width);
    expect(base.errors).toEqual([]); expect(base.writes).toEqual([]); expect(base.unknown).toEqual([]);
  });
}

test("授权报告默认未公开，HR明确确认后可分享并撤销，页面不请求聊天全文", async ({ page }) => {
  const base = await mockUniformAdmin(page);
  const report = { public_id: "synthetic-public-report", share_enabled: false, generated_at: "2026-10-30T08:00:00Z", summary: "合成候选人符合已确认条件", report: { candidate: { name: "合成候选人1" }, position: { name: "合成岗位1" } } };
  const actions: string[] = [];
  await page.route(/\/api\/(?:candidates\/[^/]+\/auto-reply|auto-reply\/recommendations\/)/, async (route) => {
    const request = route.request(); const url = new URL(request.url()); const path = url.pathname;
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (path.startsWith("/api/candidates/")) {
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
