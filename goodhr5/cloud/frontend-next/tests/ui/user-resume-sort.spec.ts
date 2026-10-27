/** 文件作用：用全拦截的合成用户验证简历数远程排序、分页搜索保留、旧回包隔离与桌面手机展示，不访问真实业务接口。 */
import { expect, test, type Page } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { expectUniformNetwork, mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const output = resolve(process.env.UI_TEST_SCREENSHOT_DIR || "../../../output/user-resume-sort-20261027");
const createdAt = "2026-10-27T08:00:00Z";
type SortRequest = { page: number; pageSize: number; query: string; sortBy: string | null; order: string | null };
type SortUser = { id: string; email: string; role: string; status: string; resume_count?: number; created_at: string; ai_balance: string };

test.use({
  actionTimeout: 12_000,
  storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "resume-sort-synthetic-only" }] }] },
});

/** userRows 创建六十一位合成用户，最大数量位于默认第三页，零值和缺值均保留以验证边界展示。 */
function userRows(): SortUser[] {
  return Array.from({ length: 61 }, (_, index) => ({
    id: `sort-user-${index + 1}`, email: `sort-user-${index + 1}@example.invalid`, role: "user", status: "active",
    ...(index === 1 ? {} : { resume_count: index === 0 ? 0 : index + 1 }), created_at: createdAt, ai_balance: "0.0000",
  }));
}

/** fixture 复用统一后台全拦截，并在分页之前对完整合成用户集执行服务端排序，支持主动挂起一次降序响应。 */
async function fixture(page: Page) {
  const base = await mockUniformAdmin(page);
  const state = { requests: [] as SortRequest[], completed: [] as SortRequest[], holdDescending: false, release: null as (() => void) | null };
  await page.route(/\/api\/admin\/users(?:\?|$)/, async (route) => {
    const request = route.request(), url = new URL(request.url());
    const headers = { "access-control-allow-origin": "*", "access-control-allow-methods": "*", "access-control-allow-headers": "*" };
    if (!["127.0.0.1", "localhost", "[::1]"].includes(url.hostname)) {
      base.unknown.push(`拒绝非本机用户接口：${url.origin}${url.pathname}`);
      return route.abort("blockedbyclient");
    }
    if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    if (request.method() !== "GET") { base.writes.push(`${request.method()} ${url.pathname}`); return route.abort("blockedbyclient"); }
    const current: SortRequest = {
      page: Number(url.searchParams.get("page")) || 1, pageSize: Number(url.searchParams.get("page_size")) || 20,
      query: url.searchParams.get("q") || "", sortBy: url.searchParams.get("sort_by"), order: url.searchParams.get("sort_order"),
    };
    state.requests.push(current);
    if (state.holdDescending && current.order === "desc") {
      state.holdDescending = false;
      await new Promise<void>((release) => { state.release = release; });
    }
    const users = userRows().filter((user) => user.email.includes(current.query));
    if (current.sortBy === "resume_count") {
      const direction = current.order === "asc" ? 1 : -1;
      users.sort((left, right) => direction * ((left.resume_count ?? 0) - (right.resume_count ?? 0)));
    }
    const offset = (current.page - 1) * current.pageSize;
    await route.fulfill({ headers, json: { users: users.slice(offset, offset + current.pageSize), total: users.length, page: current.page, stats: {} } });
    state.completed.push(current);
  });
  return { base, state };
}

/** firstEmail 读取当前桌面表格第一行账号，判断使用的是完整数据排序而不是当前页本地排序。 */
function firstEmail(page: Page) {
  return page.locator(".ant-table-tbody .ant-table-row").first().locator("td").first();
}

/** expectRequest 确认本次远程请求已到达且用户界面完成读取，避免点击之间相互覆盖。 */
async function expectRequest(page: Page, requests: SortRequest[], expected: Partial<SortRequest>) {
  await expect.poll(() => requests.at(-1)).toMatchObject(expected);
  await expect(page.locator(".ant-table-wrapper .ant-spin-spinning")).toHaveCount(0);
}

test("1366px 简历数按全部用户降序升序恢复默认，翻页和搜索保留排序并回第一页", async ({ page }) => {
  const { base, state } = await fixture(page);
  await page.setViewportSize({ width: 1366, height: 960 });
  await page.goto("/admin/users");
  await expect(firstEmail(page)).toContainText("sort-user-1@example.invalid");
  await expect(page.locator(".ant-table-tbody .ant-table-row")).toHaveCount(20);
  for (const email of ["sort-user-1@example.invalid", "sort-user-2@example.invalid"]) {
    const row = page.locator(".ant-table-tbody .ant-table-row").filter({ hasText: email });
    await expect(row.locator("td").nth(6)).toHaveText("0");
  }
  await page.locator(".ant-pagination-item-2").click();
  await expectRequest(page, state.requests, { page: 2, sortBy: null, order: null });
  await expect(firstEmail(page)).toContainText("sort-user-21@example.invalid");

  const header = page.getByRole("columnheader", { name: /简历数/ });
  await header.click();
  await expectRequest(page, state.requests, { page: 1, sortBy: "resume_count", order: "desc" });
  await expect(header).toHaveAttribute("aria-sort", "descending");
  await expect(firstEmail(page)).toContainText("sort-user-61@example.invalid");
  mkdirSync(output, { recursive: true });
  await page.screenshot({ path: resolve(output, "users-resume-sort-1366-desc.png"), fullPage: true });
  await page.locator(".ant-pagination-item-2").click();
  await expectRequest(page, state.requests, { page: 2, sortBy: "resume_count", order: "desc" });
  await expect(firstEmail(page)).toContainText("sort-user-41@example.invalid");

  await header.click();
  await expectRequest(page, state.requests, { page: 1, sortBy: "resume_count", order: "asc" });
  await expect(header).toHaveAttribute("aria-sort", "ascending");
  await expect(firstEmail(page)).toContainText("sort-user-1@example.invalid");
  await page.getByLabel("搜索用户", { exact: true }).fill("  sort-user  ");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await expectRequest(page, state.requests, { page: 1, query: "sort-user", sortBy: "resume_count", order: "asc" });
  await page.locator(".ant-pagination-item-2").click();
  await expectRequest(page, state.requests, { page: 2, query: "sort-user", sortBy: "resume_count", order: "asc" });
  await expect(firstEmail(page)).toContainText("sort-user-21@example.invalid");

  await header.click();
  await expectRequest(page, state.requests, { page: 1, query: "sort-user", sortBy: null, order: null });
  await expect(header).not.toHaveAttribute("aria-sort");
  await expect(firstEmail(page)).toContainText("sort-user-1@example.invalid");
  await header.click();
  await expectRequest(page, state.requests, { page: 1, query: "sort-user", order: "desc" });
  await page.getByLabel("搜索用户", { exact: true }).fill("sort-user-6@");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await expectRequest(page, state.requests, { page: 1, query: "sort-user-6@", sortBy: "resume_count", order: "desc" });
  await expect(page.locator(".ant-table-tbody .ant-table-row")).toHaveCount(1);
  await expect(firstEmail(page)).toContainText("sort-user-6@example.invalid");
  await page.getByRole("button", { name: "重置", exact: true }).click();
  await expectRequest(page, state.requests, { page: 1, query: "", sortBy: "resume_count", order: "desc" });
  await expect(firstEmail(page)).toContainText("sort-user-61@example.invalid");
  expectUniformNetwork(base);
  writeFileSync(resolve(output, "users-resume-sort-requests.json"), JSON.stringify(state.requests, null, 2));
});

test("快速切换简历数排序时，最后返回的旧降序响应不能覆盖较新的升序结果", async ({ page }) => {
  const { base, state } = await fixture(page);
  await page.setViewportSize({ width: 1366, height: 960 });
  await page.goto("/admin/users");
  await expect(firstEmail(page)).toContainText("sort-user-1@example.invalid");
  state.holdDescending = true;
  const header = page.getByRole("columnheader", { name: /简历数/ });
  await header.click();
  await expect.poll(() => state.release !== null).toBe(true);
  // 真实键盘继续触发已聚焦表头，覆盖读取中连续切换而不借助脚本触发事件。
  await header.press("Enter");
  await expectRequest(page, state.requests, { page: 1, sortBy: "resume_count", order: "asc" });
  await expect(firstEmail(page)).toContainText("sort-user-1@example.invalid");
  const oldResponse = page.waitForResponse((response) => new URL(response.url()).searchParams.get("sort_order") === "desc");
  const release = state.release;
  if (!release) throw new Error("旧降序响应未进入合成挂起状态。");
  release();
  await (await oldResponse).finished();
  // 给已完整交付的旧响应一次渲染机会，确保断言能发现覆盖，而非在旧响应应用之前过早通过。
  await page.waitForTimeout(200);
  await expect(firstEmail(page)).toContainText("sort-user-1@example.invalid");
  await expect(header).toHaveAttribute("aria-sort", "ascending");
  expectUniformNetwork(base);
  mkdirSync(output, { recursive: true });
  writeFileSync(resolve(output, "users-resume-sort-response-order.json"), JSON.stringify(state.completed, null, 2));
});

test("390px 手机卡片保留真实零值及缺失数量回退，未触发真实接口或写入", async ({ page }) => {
  const { base, state } = await fixture(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/admin/users");
  await expect(page.getByText("sort-user-1@example.invalid", { exact: true }).filter({ visible: true })).toBeVisible();
  await expect(page.getByText("简历数 0", { exact: true }).filter({ visible: true })).toHaveCount(2);
  await expect(page.getByText("简历数 3", { exact: true }).filter({ visible: true })).toBeVisible();
  await expectRequest(page, state.requests, { page: 1, sortBy: null, order: null });
  expectUniformNetwork(base);
  mkdirSync(output, { recursive: true });
  await page.screenshot({ path: resolve(output, "users-resume-sort-390-cards.png"), fullPage: true });
});
