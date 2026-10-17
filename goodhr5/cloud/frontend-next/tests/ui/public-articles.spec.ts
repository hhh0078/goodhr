/** 本文件验证公开文章无脚本阅读、纯文本换行、移动端导航与动态 sitemap 的增删分片。 */
import { expect, test } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const fixtureOrigin = `http://127.0.0.1:${process.env.ARTICLE_FIXTURE_PORT || "18167"}`;
const firstID = "10000000-0000-4000-8000-000000000001";

/** setArticlesMode 切换本机合成 API，不修改任何真实文章。 */
async function setArticlesMode(mode: string) {
  const response = await fetch(`${fixtureOrigin}/__test__/articles?mode=${mode}`);
  if (!response.ok) throw new Error("切换合成文章失败");
}

test.beforeEach(async () => { await setArticlesMode("normal"); });
test.afterEach(async () => { await setArticlesMode("normal"); });

test("无 JavaScript 也能读取目录、分页及全文，HTML 只按文本显示", async ({ browser, baseURL, request }) => {
  const context = await browser.newContext({ javaScriptEnabled: false, baseURL });
  const page = await context.newPage();
  await page.goto("/articles");
  await expect(page.getByRole("region", { name: "文章目录" }).getByRole("listitem")).toHaveCount(20);
  await expect(page.getByRole("link", { name: "下一页", exact: true })).toHaveAttribute("href", "/articles?page=2");
  await page.getByRole("link", { name: "下一页", exact: true }).click();
  await expect(page.getByRole("region", { name: "文章目录" }).getByRole("listitem")).toHaveCount(1);
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", "https://goodhr.58it.cn/articles?page=2");
  await page.goto(`/articles/${firstID}`);
  const content = page.getByTestId("article-content");
  await expect(content).toHaveCSS("white-space", "pre-wrap");
  const text = await content.textContent();
  expect(text).toContain("第一段：每天少做一点重复工作。\n\n第二段");
  expect(text).toContain("<script>window.__articleInjected=true</script>");
  await expect(content.locator("script, b")).toHaveCount(0);
  const html = await (await request.get(`/articles/${firstID}`)).text();
  expect(html).toContain("&lt;script&gt;window.__articleInjected=true&lt;/script&gt;");
  expect(html).toContain('"@type":"Article"');
  expect(html).toContain('"@type":"BreadcrumbList"');
  await context.close();
});

test("文章详情元数据完整，非法 ID 与不存在文章不会公开正文", async ({ page, request }) => {
  await page.goto(`/articles/${firstID}`);
  await expect(page.locator('meta[property="og:type"]')).toHaveAttribute("content", "article");
  await expect(page.locator('meta[property="article:published_time"]')).toHaveAttribute("content", "2026-10-17T02:30:00Z");
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", `https://goodhr.58it.cn/articles/${firstID}`);
  for (const path of ["/articles/not-an-id", "/articles/10000000-0000-4000-8000-000000000099", "/articles?page=0", "/articles?page=3"]) {
    const response = await request.get(path, { headers: { "User-Agent": "Googlebot" } });
    expect(response.status(), path).toBe(404);
    expect(await response.text()).not.toContain('data-testid="article-content"');
  }
});

test("文章新增与删除立即改变 sitemap，五万篇以上有分片，接口故障返回 503", async ({ request }) => {
  const index = await request.get("/sitemap.xml");
  expect(index.status()).toBe(200);
  expect(index.headers()["content-type"]).toContain("application/xml");
  expect(index.headers()["cache-control"]).toContain("no-store");
  const rootXML = await index.text();
  expect(rootXML).toContain("<urlset");
  expect((rootXML.match(/<url>/g) || [])).toHaveLength(28);
  for (const path of ["/features", "/pricing", "/videos", "/download", "/contact", "/articles", `/articles/${firstID}`]) {
    expect(rootXML).toContain(`https://goodhr.58it.cn${path}</loc>`);
  }
  expect(await (await request.get("/sitemaps/site.xml")).text()).toContain("https://goodhr.58it.cn/articles</loc>");
  expect(await (await request.get("/sitemaps/articles/1.xml")).text()).toContain(firstID);
  await setArticlesMode("deleted");
  expect(await (await request.get("/sitemap.xml")).text()).not.toContain(firstID);
  expect(await (await request.get("/sitemaps/articles/1.xml")).text()).not.toContain(firstID);
  await setArticlesMode("normal");
  expect(await (await request.get("/sitemaps/articles/1.xml")).text()).toContain(firstID);
  await setArticlesMode("many");
  const many = await (await request.get("/sitemap.xml")).text();
  expect(many).toContain("/sitemaps/articles/52.xml");
  const last = await request.get("/sitemaps/articles/52.xml");
  expect(last.status()).toBe(200);
  expect(((await last.text()).match(/<url>/g) || [])).toHaveLength(1);
  await setArticlesMode("empty");
  const emptyXML = await (await request.get("/sitemap.xml")).text();
  expect(emptyXML).toContain("<urlset");
  expect((emptyXML.match(/<url>/g) || [])).toHaveLength(7);
  expect(emptyXML).toContain("/videos</loc>");
  expect(emptyXML).toContain("/contact</loc>");
  expect((await request.get("/sitemaps/articles/1.xml")).status()).toBe(404);
  await setArticlesMode("unavailable");
  expect((await request.get("/sitemap.xml")).status()).toBe(503);
  expect((await request.get("/sitemaps/articles/1.xml")).status()).toBe(503);
});

test("robots 指向动态地图，AI 目录和逐篇纯文本随公开文章同步", async ({ request }) => {
  const robots = await request.get("/robots.txt");
  expect(robots.status()).toBe(200);
  expect(robots.headers()["cache-control"]).toContain("no-store");
  const rules = await robots.text();
  expect(rules).toContain("User-agent: *");
  expect(rules).toContain("Disallow: /admin");
  expect(rules).toContain("Disallow: /api/");
  expect(rules).toContain("Sitemap: https://goodhr.58it.cn/sitemap.xml");
  expect(rules).toContain("https://goodhr.58it.cn/llms.txt");
  const llms = await request.get("/llms.txt");
  expect(llms.status()).toBe(200);
  expect(llms.headers()["cache-control"]).toContain("no-store");
  expect(llms.headers()["content-type"]).toContain("text/plain");
  const index = await llms.text();
  expect(index).toContain("[视频教程](https://goodhr.58it.cn/videos)");
  expect(index).toContain("[联系我们](https://goodhr.58it.cn/contact)");
  expect(index).toContain(`/articles/${firstID}/content.md`);
  expect(index).not.toContain("/admin/");
  const markdown = await request.get(`/articles/${firstID}/content.md`);
  expect(markdown.status()).toBe(200);
  expect(markdown.headers()["content-type"]).toContain("text/plain");
  expect(markdown.headers()["x-content-type-options"]).toBe("nosniff");
  expect(await markdown.text()).toContain("第一段：每天少做一点重复工作。\n\n第二段");
  await setArticlesMode("deleted");
  expect(await (await request.get("/llms.txt")).text()).not.toContain(firstID);
  expect((await request.get(`/articles/${firstID}/content.md`)).status()).toBe(404);
  expect((await request.get("/articles/not-a-uuid/content.md")).status()).toBe(404);
  await setArticlesMode("empty");
  expect((await request.get("/llms.txt")).status()).toBe(200);
  expect((await request.get("/llms/articles/1.txt")).status()).toBe(200);
  expect((await request.get("/llms/articles/2.txt")).status()).toBe(404);
  await setArticlesMode("many");
  expect(await (await request.get("/llms.txt")).text()).toContain("/llms/articles/2.txt");
  const next = await request.get("/llms/articles/2.txt");
  expect(next.status()).toBe(200);
  expect(await next.text()).toContain("/articles/10000000-0000-4000-8000-000000001001/content.md");
  expect((await request.get("/llms/articles/53.txt")).status()).toBe(404);
  expect((await request.get("/llms/articles/0.txt")).status()).toBe(404);
  await setArticlesMode("unavailable");
  for (const path of ["/llms.txt", "/llms/articles/1.txt", `/articles/${firstID}/content.md`]) {
    const failed = await request.get(path);
    expect(failed.status(), path).toBe(503);
    expect(failed.headers()["retry-after"]).toBe("60");
  }
  expect((await request.get("/robots.txt")).status()).toBe(200);
});

test("千篇边界切换完整站点地图，目录缺项会返回可重试错误", async ({ request }) => {
  await setArticlesMode("thousand");
  const flat = await (await request.get("/sitemap.xml")).text();
  expect(flat).toContain("<urlset");
  expect((flat.match(/<url>/g) || [])).toHaveLength(1007);
  await setArticlesMode("overflow");
  const index = await (await request.get("/sitemap.xml")).text();
  expect(index).toContain("<sitemapindex");
  expect((index.match(/<sitemap>/g) || [])).toHaveLength(3);
  const tail = await request.get("/sitemaps/articles/2.xml");
  expect(tail.status()).toBe(200);
  expect((await tail.text()).match(/<url>/g)).toHaveLength(1);
  await setArticlesMode("partial");
  for (const path of ["/sitemap.xml", "/sitemaps/articles/1.xml", "/llms.txt", "/llms/articles/1.txt"]) {
    const failed = await request.get(path);
    expect(failed.status(), path).toBe(503);
    expect(failed.headers()["retry-after"]).toBe("60");
  }
});

for (const width of [1440, 1051, 390]) {
  test(`公开文章在 ${width}px 下不横向溢出，顶部可进入文章目录`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 });
    await page.goto("/articles");
    if (width < 1051) await page.getByRole("button", { name: "打开导航菜单", exact: true }).click();
    const nav = page.getByRole("navigation", { name: width < 1051 ? "移动端官网导航" : "官网导航", exact: true });
    await expect(nav.getByRole("link", { name: "动态", exact: true })).toHaveAttribute("href", "/articles");
    if (width < 1051) await page.getByLabel("关闭导航菜单", { exact: true }).click();
    const screenshotDir = resolve("artifacts/articles-20261017");
    mkdirSync(screenshotDir, { recursive: true });
    await page.screenshot({ path: resolve(screenshotDir, `directory-${width}.png`), fullPage: true });
    await page.goto(`/articles/${firstID}`);
    const titleBox = await page.getByRole("heading", { level: 1 }).boundingBox();
    const bodyBox = await page.getByTestId("article-content").boundingBox();
    expect(titleBox!.x).toBeGreaterThanOrEqual(0);
    expect(titleBox!.x + titleBox!.width).toBeLessThanOrEqual(width);
    expect(bodyBox!.x + bodyBox!.width).toBeLessThanOrEqual(width);
    await page.screenshot({ path: resolve(screenshotDir, `article-${width}.png`), fullPage: true });
  });
}
