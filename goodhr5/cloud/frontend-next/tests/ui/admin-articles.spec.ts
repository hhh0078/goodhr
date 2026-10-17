/** 本文件以合成接口验证文章管理的换行保存、全量链接复制、失败保留及管理员权限。 */
import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { mockUniformAdmin, uniformOrigin } from "./admin-uniform-fixture";

const firstId = "11111111-1111-4111-8111-111111111111";
const secondId = "22222222-2222-4222-8222-222222222222";
const time = "2026-10-01T08:00:00Z";
const screenshotDir = resolve("../../../output/articles-admin-20261017");
type Article = { id: string; title: string; content: string; is_published: boolean; published_at: string; created_at: string; updated_at: string };

test.use({ storageState: { cookies: [], origins: [{ origin: uniformOrigin, localStorage: [{ name: "goodhr5_access_token", value: "article-synthetic-only" }] }] } });

/** mockArticles 只在浏览器内处理文章数据，其他请求沿用已有完全隔离的后台合成接口。 */
async function mockArticles(page: Page, role = "super_admin") {
  const uniform = await mockUniformAdmin(page);
  const state = { articles: [
    { id: firstId, title: "合成文章标题", content: "第一行正文\n\n第二段正文\n最后一行", is_published: true, published_at: time, created_at: time, updated_at: time },
    { id: secondId, title: "第二篇公开文章", content: "另一份合成正文", is_published: true, published_at: time, created_at: time, updated_at: time },
  ] as Article[], writes: [] as Array<{ method: string; body: any }>, calls: [] as string[], failSave: false };
  await page.context().route("**/api/auth/me", (route) => route.fulfill({ json: { user: { id: "article-synthetic-user", email: "articles@example.invalid", role, tenant_id: "article-team", has_openid: true } } }));
  await page.context().route("**/api/admin/articles**", async (route) => {
    const request = route.request(), url = new URL(request.url()), method = request.method();
    state.calls.push(`${method} ${url.pathname}`);
    if (url.pathname.endsWith("/links")) return route.fulfill({ json: { ok: true, items: state.articles.filter((article) => article.is_published).map(({ id }) => ({ id })) } });
    const id = url.pathname.split("/")[4];
    if (method === "GET" && id) return route.fulfill({ json: { ok: true, article: state.articles.find((article) => article.id === id) } });
    if (method === "GET") {
      const q = url.searchParams.get("q") || "";
      const filtered = state.articles.filter((article) => article.title.includes(q));
      const size = Number(url.searchParams.get("page_size")) || 20, current = Number(url.searchParams.get("page")) || 1;
      return route.fulfill({ json: { ok: true, items: filtered.slice((current - 1) * size, current * size).map(({ content: _, ...summary }) => ({ ...summary, excerpt: "只有列表摘要" })), total: filtered.length, page: current, page_size: size } });
    }
    const body = method === "DELETE" ? null : request.postDataJSON();
    state.writes.push({ method, body });
    if (state.failSave) return route.fulfill({ status: 503, json: { error: "合成保存失败，正文仍保留" } });
    if (method === "DELETE") { state.articles = state.articles.filter((article) => article.id !== id); return route.fulfill({ json: { ok: true } }); }
    const article: Article = { ...body, id: id || "33333333-3333-4333-8333-333333333333", created_at: time, updated_at: time };
    state.articles = id ? state.articles.map((old) => old.id === id ? article : old) : [...state.articles, article];
    return route.fulfill({ json: { ok: true, article } });
  });
  return { state, uniform };
}

test("文章编辑读取完整正文并保留空行，保存失败不丢草稿", async ({ page }) => {
  const { state, uniform } = await mockArticles(page);
  await page.goto("/admin/articles");
  await page.getByRole("main").getByRole("button", { name: "合成文章标题", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "编辑文章", exact: true });
  await expect(dialog.getByLabel("正文", { exact: true })).toHaveValue(state.articles[0].content);
  const body = "新的第一行\n\n保留空行后的第二段\n<script>这也是普通文本</script>";
  await dialog.getByLabel("正文", { exact: true }).fill(body);
  state.failSave = true;
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog.getByLabel("正文", { exact: true })).toHaveValue(body);
  await expect(page.getByText("合成保存失败，正文仍保留", { exact: true })).toBeVisible();
  state.failSave = false;
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect(dialog).toBeHidden();
  expect(state.writes.at(-1)?.body.content).toBe(body);
  expect(state.articles[0].content).toBe(body);
  expect(uniform.errors).toEqual([]);
});

test("复制全部公开链接忽略当前筛选，实际剪贴板每行一条", async ({ page, context }) => {
  const { uniform } = await mockArticles(page);
  await context.grantPermissions(["clipboard-read", "clipboard-write"], { origin: uniformOrigin });
  await page.goto("/admin/articles");
  await page.getByRole("searchbox", { name: "搜索文章标题", exact: true }).fill("第二篇");
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page.getByRole("main").getByRole("button", { name: "合成文章标题", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "复制全部文章链接", exact: true }).click();
  await expect(page.getByText("已复制 2 条文章链接，每行一条", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "新建文章", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "新建文章", exact: true });
  await dialog.getByLabel("正文", { exact: true }).focus();
  await page.keyboard.press("Control+V");
  const base = (process.env.NEXT_PUBLIC_SITE_URL || "https://goodhr.58it.cn").replace(/\/$/, "");
  await expect(dialog.getByLabel("正文", { exact: true })).toHaveValue(`${base}/articles/${firstId}\n${base}/articles/${secondId}`);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  expect(uniform.errors).toEqual([]);
});

test("发布开关可以撤回与重新公开，删除需要确认且会更新列表", async ({ page }) => {
  const { state, uniform } = await mockArticles(page);
  await page.goto("/admin/articles");
  await page.getByRole("main").getByRole("button", { name: "合成文章标题", exact: true }).click();
  const editor = page.getByRole("dialog", { name: "编辑文章", exact: true });
  await expect(editor.getByRole("switch")).toBeChecked();
  await editor.getByRole("switch").uncheck();
  await editor.getByRole("button", { name: "保存", exact: true }).click();
  await expect(editor).toBeHidden();
  expect(state.articles[0].is_published).toBe(false);
  await page.getByRole("main").getByRole("button", { name: "合成文章标题", exact: true }).click();
  await editor.getByRole("switch").check();
  await editor.getByRole("button", { name: "保存", exact: true }).click();
  await expect(editor).toBeHidden();
  expect(state.articles[0].is_published).toBe(true);
  const row = page.getByRole("row").filter({ has: page.getByRole("button", { name: "合成文章标题", exact: true }) });
  await row.getByRole("button", { name: "删除", exact: true }).click();
  const confirmation = page.getByRole("dialog", { name: "删除文章", exact: true });
  await expect(confirmation).toBeVisible();
  expect(state.articles.some((article) => article.id === firstId)).toBe(true);
  await confirmation.getByRole("button", { name: "确认", exact: true }).click();
  await expect(page.getByRole("main").getByRole("button", { name: "合成文章标题", exact: true })).toHaveCount(0);
  expect(state.articles.some((article) => article.id === firstId)).toBe(false);
  expect(uniform.errors).toEqual([]);
});

for (const width of [1440, 390]) {
  test(`文章管理 ${width}px 新建控件可见，创建默认草稿`, async ({ page }) => {
    const { state, uniform } = await mockArticles(page);
    await page.setViewportSize({ width, height: 960 });
    await page.goto("/admin/articles");
    const create = page.getByRole("button", { name: "新建文章", exact: true });
    await expect(create).toBeVisible();
    const box = await create.boundingBox();
    expect(box!.height).toBe(32);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
    if (width >= 600) {
      const search = await page.getByRole("searchbox", { name: "搜索文章标题", exact: true }).boundingBox();
      const status = await page.getByRole("combobox", { name: "文章发布状态", exact: true }).boundingBox();
      expect(Math.abs(search!.y + search!.height / 2 - box!.y - box!.height / 2)).toBeLessThanOrEqual(2);
      expect(Math.abs(status!.y + status!.height / 2 - box!.y - box!.height / 2)).toBeLessThanOrEqual(2);
      expect(search!.width).toBeLessThanOrEqual(280);
    }
    mkdirSync(screenshotDir, { recursive: true });
    await page.screenshot({ path: resolve(screenshotDir, `list-${width}.png`), fullPage: true });
    await create.click();
    const dialog = page.getByRole("dialog", { name: "新建文章", exact: true });
    await dialog.getByLabel("标题", { exact: true }).fill("新建合成文章");
    await dialog.getByLabel("正文", { exact: true }).fill("一行\n下一行");
    await dialog.getByRole("button", { name: "保存", exact: true }).click();
    await expect(dialog).toBeHidden();
    expect(state.writes.at(-1)?.body.is_published).toBe(false);
    expect(state.writes.at(-1)?.body.published_at).toMatch(/^\d{4}-\d{2}-\d{2}T/);
    expect(uniform.errors).toEqual([]);
  });
}

test("普通账号不显示文章管理操作且不读取管理接口", async ({ page }) => {
  const { state, uniform } = await mockArticles(page, "user");
  await page.goto("/admin/articles");
  await expect(page.getByText("只有超级管理员可以管理官网文章", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "新建文章", exact: true })).toHaveCount(0);
  expect(state.writes).toEqual([]);
  expect(state.calls).toEqual([]);
  expect(uniform.errors).toEqual([]);
});
