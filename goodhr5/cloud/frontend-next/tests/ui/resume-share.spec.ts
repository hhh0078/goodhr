/** 文件作用说明：用合成简历和完整接口拦截验证共享入口、撤销、匿名公开访问、二维码及手机布局。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const origin = new URL(process.env.UI_TEST_BASE_URL || "http://127.0.0.1:5190").origin;
const cloudOrigin = new URL(process.env.NEXT_PUBLIC_CLOUD_API_BASE || "http://127.0.0.1:8084").origin;
const agentOrigin = new URL(`http://127.0.0.1:${process.env.UI_TEST_AGENT_PORT || "43129"}`).origin;
const shareToken = "a".repeat(64);
const privateResume = {
  id: "share-resume", candidate_name: "分享合成候选人", phone: "13800138000", email: "candidate@example.invalid",
  gender: "男", education_level: "本科", work_years: "5", expected_position: "产品经理", expected_region: "广州",
  work_experiences: [{ company_name: "合成公司", position_name: "产品经理", content: "公开工作经历" }],
  raw_text: "不公开的完整原文", ai_summary: "不公开的AI摘要", user_email: "private-owner@example.invalid", creator_name: "内部入库人",
  ai: { detail: { score: 92, reason: "内部AI评分原因" } }, events: [{ event_type: "private", message_text: "内部任务记录" }],
};
const publicResume = { candidate_name: privateResume.candidate_name, phone: privateResume.phone, email: privateResume.email, gender: privateResume.gender,
  education_level: privateResume.education_level, work_years: privateResume.work_years, expected_position: privateResume.expected_position,
  expected_region: privateResume.expected_region, work_experiences: privateResume.work_experiences };
type State = { revoked: boolean; creates: number; calls: Array<{ path: string; method: string; authorization: string }>; errors: string[]; analytics: string[] };

test.use({ storageState: { cookies: [], origins: [{ origin, localStorage: [{ name: "goodhr5_access_token", value: "share-test-only" }] }] } });

/** mockShareSession 拦截全部云端、本地及第三方请求，只返回合成简历和模拟权限状态。 */
async function mockShareSession(page: Page): Promise<State> {
  const state: State = { revoked: false, creates: 0, calls: [], errors: [], analytics: [] };
  page.on("pageerror", (error) => state.errors.push(error.message));
  page.on("request", (request) => { if (/hm\.baidu\.com/.test(request.url())) state.analytics.push(request.url()); });
  await page.context().route("**/*", async (route) => {
    const request = route.request(); const url = new URL(request.url()); const method = request.method();
    if (url.origin === origin && !url.pathname.startsWith("/api/")) return route.continue();
    if (![origin, cloudOrigin, agentOrigin, "https://goodhr.58it.cn", "http://127.0.0.1:8084", "http://127.0.0.1:43129", "http://127.0.0.1:43139"].includes(url.origin)) return route.abort();
    const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    state.calls.push({ path: url.pathname, method, authorization: request.headers().authorization || "" });
    let data: any = { ok: true };
    if (url.pathname === `/api/public/resume-shares/${shareToken}`) {
      if (state.revoked) return route.fulfill({ status: 404, headers, json: { ok: false, error: { message: "这份分享已取消，或简历已删除" } } });
      data = { ok: true, candidate: publicResume };
    } else if (url.pathname === "/api/candidates/share-resume/share") {
      if (method === "DELETE") state.revoked = true;
      else { state.creates += 1; state.revoked = false; data = { ok: true, token: shareToken }; }
    } else if (["43129", "43139", new URL(agentOrigin).port].includes(url.port) && (url.pathname === "/health" || url.pathname.startsWith("/api/v1/"))) {
      if (url.pathname === "/health") data = { ok: true, version: "6.2.10.2" };
      else if (url.pathname === "/api/v1/runtime/status") data = { version: "6.2.10.2", node_installed: true, camoufox_installed: true, system_runtime: { ready: true, required: false } };
      else if (url.pathname === "/api/v1/app-update/status") data = { stage: "idle", current_version: "6.2.10.2" };
    } else if (url.pathname === "/api/auth/me") data = { user: { id: "share-user", email: "share@example.invalid", role: "super_admin", tenant_id: "share-team", password_set: true } };
    else if (url.pathname === "/api/subscription/status") data = { subscription: { active: true, member_type: "plus", member_name: "Plus", remaining_days: 30, allow_ai: true } };
    else if (url.pathname === "/api/config/notification-profile") data = { profile: { completed: true } };
    else if (url.pathname === "/api/runtime/config" || url.pathname === "/api/system/app-config") data = { config: {} };
    else if (url.pathname === "/api/user-flow") data = { flow: { version: 2, state: "completed", stage: "completed", steps: {} } };
    else if (url.pathname.includes("/invitations")) data = { invitations: [] };
    else if (url.pathname === "/api/ai-wallet") data = { wallet: { balance: "100", models: [] } };
    else if (url.pathname === "/api/positions") data = { positions: [], total: 0 };
    else if (url.pathname === "/api/candidates") data = { candidates: [privateResume], total: 1, page: 1 };
    else if (url.pathname.endsWith("/notes")) data = { notes: [{ id: "private-note", content: "内部跟进备注" }] };
    else if (url.pathname.endsWith("/recommendations")) data = { recommendations: [] };
    else if (url.pathname === "/api/candidates/share-resume") data = { candidate: privateResume };
    return route.fulfill({ status: 200, headers, json: data });
  });
  return state;
}

test("详情与抽屉共用分享入口，链接和二维码可生成、撤销并重新开启", async ({ page }) => {
  const state = await mockShareSession(page);
  await page.goto("/admin/resumes/detail?candidate_id=share-resume");
  await expect(page.getByRole("heading", { name: privateResume.candidate_name, exact: true })).toBeVisible();
  await page.getByRole("button", { name: "分享简历", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "分享简历", exact: true });
  await expect(dialog.getByLabel("简历分享链接")).toHaveValue(`${origin}/resumes/share/${shareToken}`);
  await expect(dialog.locator(".ant-qrcode canvas, .ant-qrcode svg")).toHaveCount(1);
  await expect(dialog.getByText("包含简历中的联系方式", { exact: false })).toBeVisible();
  await expect.poll(() => state.creates).toBe(1);
  await dialog.getByRole("button", { name: "取消分享", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "重新开启分享", exact: true })).toBeVisible();
  await expect(dialog.locator(".ant-qrcode")).toHaveCount(0);
  expect(state.revoked).toBe(true);
  await dialog.getByRole("button", { name: "重新开启分享", exact: true }).click();
  await expect(dialog.getByLabel("简历分享链接")).toHaveValue(`${origin}/resumes/share/${shareToken}`);
  await expect.poll(() => state.creates).toBe(2);
  await dialog.getByRole("button", { name: "收好", exact: true }).click();
  await page.goto("/admin/resumes");
  await page.getByRole("button", { name: privateResume.candidate_name, exact: true }).click();
  const drawer = page.getByRole("dialog", { name: privateResume.candidate_name, exact: true });
  await drawer.getByRole("button", { name: "分享简历", exact: true }).click();
  await expect(dialog.getByLabel("简历分享链接")).toHaveValue(`${origin}/resumes/share/${shareToken}`);
  expect(state.errors).toEqual([]);
});

test("公开页无需登录，只请求公开接口并展示二维码，手机布局没有横向溢出且不加载统计", async ({ page }) => {
  const state = await mockShareSession(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/resumes/share/${shareToken}`);
  await expect(page.getByRole("heading", { name: privateResume.candidate_name, exact: true })).toBeVisible();
  await expect(page.getByLabel("公开简历资料").getByText("公开工作经历", { exact: true })).toBeVisible();
  await expect(page.getByLabel("简历二维码").locator("canvas, svg")).toHaveCount(1);
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute("content", /noindex/);
  await expect(page.locator('meta[name="googlebot"]')).toHaveAttribute("content", /noindex/);
  await expect(page.locator('meta[name="referrer"]')).toHaveAttribute("content", "no-referrer");
  await expect(page.locator('script[src*="hm.baidu.com"], script#baidu-analytics')).toHaveCount(0);
  expect(state.analytics).toEqual([]);
  expect(state.calls.filter((call) => call.path.startsWith("/api/"))).toEqual([{ path: `/api/public/resume-shares/${shareToken}`, method: "GET", authorization: "" }]);
  for (const text of ["内部跟进备注", "内部任务记录", "内部AI评分原因", "不公开的完整原文", "不公开的AI摘要", "内部入库人", "private-owner@example.invalid"]) await expect(page.getByText(text, { exact: false })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "分享简历", exact: true })).toHaveCount(0);
  const publicBox = await page.getByLabel("公开简历资料").boundingBox();
  expect(publicBox).not.toBeNull();
  expect(publicBox!.x).toBeGreaterThanOrEqual(0);
  expect(publicBox!.x + publicBox!.width).toBeLessThanOrEqual(390);
  const screenshots = resolve("artifacts/resume-share"); mkdirSync(screenshots, { recursive: true });
  await page.screenshot({ path: resolve(screenshots, "public-mobile.png"), fullPage: true });
  expect(state.errors).toEqual([]);
});

test("取消后的公开链接有下一步提示，不返回简历资料也不跳转登录", async ({ page }) => {
  const state = await mockShareSession(page); state.revoked = true;
  await page.goto(`/resumes/share/${shareToken}`);
  await expect(page.getByText("这份分享已取消，或简历已删除", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "重新读取", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: privateResume.candidate_name, exact: true })).toHaveCount(0);
  await expect(page).toHaveURL(new RegExp(`/resumes/share/${shareToken}$`));
  expect(state.calls.every((call) => call.path.startsWith("/api/public/resume-shares/") && !call.authorization)).toBe(true);
  expect(state.errors).toEqual([]);
});
