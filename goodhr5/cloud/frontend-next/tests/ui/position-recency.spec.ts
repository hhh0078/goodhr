/** 文件作用：验证本人岗位优先、组内更新时间排序、历史时间兜底和成功启动后的列表刷新，所有业务请求均使用合成回包。 */
import { expect, test } from "@playwright/test";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "position-recency-synthetic-token" }] }] } });

test("本人岗位优先且各组按更新时间排序，成功启动后重新读取并前移", async ({ page }) => {
  const base = await mockUniformAdmin(page, true);
  const headers = { "access-control-allow-origin": "*", "access-control-allow-headers": "*", "access-control-allow-methods": "*" };
  const own = { creator_email: "uniform@example.invalid", is_current_user: true, is_open: true, platform_id: "boss", status: "stopped", keywords: ["招聘"], common_config: { mode_default: "keyword", detail_mode: "ocr" } };
  const start = { ...own, id: "start-own", name: "合成待启动岗位", created_at: "2026-10-01T08:00:00Z", updated_at: "2026-10-03T08:00:00Z" };
  const positions = [
    start,
    { ...own, id: "tie-a", name: "合成同时间甲", created_at: start.created_at, updated_at: start.updated_at },
    { ...own, id: "missing-update", name: "合成历史岗位", created_at: "2026-10-04T08:00:00Z" },
    { ...own, id: "closed", name: "合成关闭岗位", is_open: false, created_at: start.created_at, updated_at: "2026-10-08T08:00:00Z" },
    { ...own, id: "invalid-update", name: "合成异常时间岗位", created_at: "2026-10-05T08:00:00Z", updated_at: "无效时间" },
    { ...own, id: "tie-z", name: "合成同时间乙", created_at: start.created_at, updated_at: start.updated_at },
    { ...own, id: "recent-team", name: "合成队友岗位", creator_email: "colleague@example.invalid", is_current_user: false, created_at: "2026-09-30T08:00:00Z", updated_at: "2026-10-07T08:00:00Z" },
    { ...own, id: "older-team", name: "合成队友旧岗位", creator_email: "colleague@example.invalid", is_current_user: false, created_at: "2026-10-02T08:00:00Z", updated_at: "2026-10-06T08:00:00Z" },
  ];
  let runs = 0;
  let listsAfterStart = 0;
  await page.route((url) => url.pathname === "/api/positions", (route) => {
    const method = route.request().method();
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (method !== "GET") return route.fallback();
    const isOpen = new URL(route.request().url()).searchParams.get("is_open") !== "false";
    if (runs) listsAfterStart += 1;
    return route.fulfill({ headers, json: { positions: positions.filter((position) => position.is_open === isOpen) } });
  });
  const recruitingURL = "https://position-recency.example.invalid/recruiting";
  await page.route("**/api/platforms/config/", (route) => route.fulfill({ headers, json: { configs: [{ config_key: "platform.boss", config_value: { id: "boss", open: true, auth: { pages: [{ url: recruitingURL, match: "exact", entry: true }] }, public: { pages: [{ url: "https://position-recency.example.invalid/login", code: "login" }] } } }] } }));
  await page.route("**/api/runtime/config", (route) => route.fulfill(route.request().method() === "OPTIONS" ? { status: 204, headers } : { headers, json: { config: { local_agent: [{ version: "6.2.11.0", url: "https://position-recency.example.invalid/installer" }] } } }));
  await page.route((url) => /^\/api\/v1\/(?:page\/(?:open|url)|local\/positions\/start-own\/(?:run|status|logs))$/.test(url.pathname), (route) => {
    const method = route.request().method();
    if (method === "OPTIONS") return route.fulfill({ status: 204, headers });
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/run") && method === "POST") {
      runs += 1;
      start.updated_at = "2026-10-09T08:00:00Z";
      start.status = "running";
      return route.fulfill({ status: 202, headers, json: { ok: true, data: { task_id: "synthetic-run", status: "running" } } });
    }
    if (path.endsWith("/url")) return route.fulfill({ headers, json: { ok: true, data: { url: recruitingURL } } });
    if (path.endsWith("/open")) return route.fulfill({ headers, json: { ok: true } });
    if (path.endsWith("/logs")) return route.fulfill({ headers, json: { ok: true, data: { logs: [] } } });
    return route.fulfill({ headers, json: { ok: true, data: { status: start.status } } });
  });

  await page.goto("/admin/positions");
  const cards = page.locator(".ant-card[data-position-id]");
  /** readOrder 读取岗位卡片的实际显示顺序。 */
  const readOrder = () => cards.evaluateAll((elements) => elements.map((element) => element.getAttribute("data-position-id")));
  const initialOrder = ["invalid-update", "missing-update", "tie-z", "tie-a", "start-own", "recent-team", "older-team"];
  await expect.poll(readOrder).toEqual(initialOrder);
  await expect(cards.last().getByRole("button", { name: "开始", exact: true })).toHaveCount(0);
  await page.reload();
  await expect.poll(readOrder).toEqual(initialOrder);

  const toolbar = page.getByTestId("positions-toolbar");
  await toolbar.getByText("关闭", { exact: true }).click();
  await expect.poll(readOrder).toEqual(["closed"]);
  await toolbar.getByText("开启", { exact: true }).click();
  await expect.poll(readOrder).toEqual(initialOrder);

  await page.locator('.ant-card[data-position-id="start-own"]').getByRole("button", { name: "开始", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "开始招聘岗位", exact: true });
  await dialog.getByRole("button", { name: "打开平台，并筛选条件", exact: true }).click();
  await expect(dialog).toContainText("招聘浏览器已打开");
  await dialog.getByRole("button", { name: "我已筛选好，立即开始", exact: true }).click();
  await expect.poll(() => runs, { timeout: 20_000 }).toBe(1);
  await expect(dialog).toBeHidden();
  await expect.poll(readOrder).toEqual(["start-own", ...initialOrder.filter((id) => id !== "start-own")]);
  await expect(cards.first().getByRole("button", { name: "停止", exact: true })).toBeVisible();
  expect(runs).toBe(1);
  expect(listsAfterStart).toBeGreaterThan(0);
  expect(base.writes).toEqual([]);
  expect(base.errors).toEqual([]);
});
