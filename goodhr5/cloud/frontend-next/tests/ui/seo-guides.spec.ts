/** 本文件验证招聘专题的无脚本阅读、真实能力说明、搜索入口与永久跳转，并保存桌面和手机截图；仅访问本机合成服务。 */
import { expect, test, type BrowserContext } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const site = "https://goodhr.58it.cn";
const guidePaths = ["/recruitment-automation", "/recruitment-auto-reply", "/hr-tools"];
const fixtureOrigin = `http://127.0.0.1:${process.env.ARTICLE_FIXTURE_PORT || "18167"}`;
const evidenceDir = resolve(process.env.SEO_TEST_ARTIFACT_DIR || "output/playwright/seo-20261031");
type FAQQuestion = { name: string; acceptedAnswer: { text: string } };

/** blockExternalRequests 阻止统计脚本及其他外部请求，允许的页面资源仅来自本机。 */
async function blockExternalRequests(context: BrowserContext) {
  await context.route("**/*", async (route) => {
    const hostname = new URL(route.request().url()).hostname;
    if (["127.0.0.1", "localhost", "[::1]"].includes(hostname)) await route.continue();
    else await route.abort();
  });
}

/** readFAQ 从服务端实际返回的 JSON-LD 读取问答，之后与用户可见正文核对。 */
function readFAQ(html: string): FAQQuestion[] {
  const documents: Array<Record<string, unknown>> = [];
  for (const match of html.matchAll(/<script\b[^>]*type="application\/ld\+json"[^>]*>([^]*?)<\/script>/gu)) {
    const value: unknown = JSON.parse(match[1]);
    for (const item of Array.isArray(value) ? value : [value]) {
      if (item && typeof item === "object") documents.push(item as Record<string, unknown>);
    }
  }
  const faq = documents.find((document) => document["@type"] === "FAQPage");
  expect(faq, "专题应输出 FAQPage").toBeDefined();
  expect(Array.isArray(faq?.mainEntity)).toBe(true);
  return faq!.mainEntity as FAQQuestion[];
}

/** beforeEach 保证使用回环预览与正常合成文章，禁止测试意外访问线上。 */
test.beforeEach(async ({ baseURL, context }) => {
  expect(baseURL).toBeTruthy();
  expect(new URL(baseURL!).hostname).toBe("127.0.0.1");
  const response = await fetch(`${fixtureOrigin}/__test__/articles?mode=normal`, { signal: AbortSignal.timeout(10_000) });
  expect(response.ok).toBe(true);
  await blockExternalRequests(context);
});

for (const path of guidePaths) {
  /** 无脚本专题仍须完整呈现正文、能力边界和与结构化数据一致的问答。 */
  test(`${path} 无 JavaScript 也可阅读，问答与 JSON-LD 一致`, async ({ browser, baseURL, request }) => {
    const response = await request.get(path);
    expect(response.status()).toBe(200);
    const html = await response.text();
    expect(html).toContain("<h1");
    expect(html).toContain("前程无忧");
    if (path === "/recruitment-auto-reply") expect(html).toContain("尚未正式发布");
    const questions = readFAQ(html);
    expect(questions.length).toBeGreaterThan(0);
    const context = await browser.newContext({ baseURL, javaScriptEnabled: false });
    await blockExternalRequests(context);
    try {
      const page = await context.newPage();
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", `${site}${path}`);
      if (path === "/recruitment-auto-reply") {
        await expect(page.getByRole("main").getByText(/^招聘消息自动回复仍在开发验证，尚未正式发布/u)).toBeVisible();
      }
      const platforms = page.getByRole("region", { name: "平台支持到哪一步，先看清楚", exact: true });
      for (const name of ["BOSS直聘", "猎聘企业端", "猎聘猎头端", "智联招聘", "前程无忧（51job）"]) {
        await expect(platforms.getByRole("heading", { name, exact: true })).toBeVisible();
      }
      const current51job = platforms.getByRole("listitem").filter({ has: page.getByRole("heading", { name: "前程无忧（51job）", exact: true }) });
      await expect(current51job).toContainText("招聘自动化：开发中");
      await expect(current51job).toContainText("简历插件已支持前程无忧资料导入");
      const faq = page.getByRole("region", { name: "你可能还想问", exact: true });
      expect(await faq.getByRole("heading", { level: 3 }).allTextContents()).toEqual(questions.map((question) => question.name));
      expect(await faq.locator("p").allTextContents()).toEqual(questions.map((question) => question.acceptedAnswer.text));
      for (const question of questions) {
        await expect(faq.getByRole("heading", { name: question.name, exact: true })).toBeVisible();
        await expect(faq.getByText(question.acceptedAnswer.text, { exact: true })).toBeVisible();
      }
      const actions = page.getByRole("navigation", { name: "开始使用与查看费用", exact: true });
      await expect(actions.getByRole("link", { name: "下载 GoodHR", exact: true })).toHaveAttribute("href", "/download");
      await expect(actions.getByRole("link", { name: /先看会员与费用/u })).toHaveAttribute("href", "/pricing");
    } finally {
      await context.close();
    }
  });
}

/** 新专题必须同时进入搜索引擎地图和公开 AI 阅读目录。 */
test("三个专题均被 sitemap 和 llms 发现，链接没有重复", async ({ request }) => {
  const sitemap = await request.get("/sitemap.xml");
  const llms = await request.get("/llms.txt");
  expect(sitemap.status()).toBe(200);
  expect(llms.status()).toBe(200);
  const xml = await sitemap.text();
  const markdown = await llms.text();
  for (const path of guidePaths) {
    expect(xml.split(`<loc>${site}${path}</loc>`).length - 1).toBe(1);
    expect(markdown).toContain(`](${site}${path})`);
  }
});

/** 永久跳转须保留邀请参数，域名统一只覆盖公开页面，不影响后台或接口。 */
test("历史安装地址与公开域名永久跳转，后台和接口保持原入口", async ({ request, baseURL }) => {
  const installation = await request.get("/installation.html?invite=seo-test&source=bing", { maxRedirects: 0 });
  expect(installation.status()).toBe(308);
  const destination = new URL(installation.headers().location, baseURL);
  expect(destination.pathname).toBe("/download");
  expect(destination.searchParams.get("invite")).toBe("seo-test");
  expect(destination.searchParams.get("source")).toBe("bing");
  const domainHeaders: Array<Record<string, string>> = [{ host: "goodhr5.58it.cn" }, { host: "goodhr.58it.cn", "x-forwarded-proto": "http" }];
  for (const headers of domainHeaders) {
    for (const path of ["/", ...guidePaths, "/articles", "/robots.txt", "/sitemap.xml", "/llms.txt"]) {
      const response = await request.get(`${path}?invite=seo-test`, { headers, maxRedirects: 0 });
      expect(response.status(), path).toBe(308);
      const location = new URL(response.headers().location);
      expect(location.origin).toBe(site);
      expect(location.pathname).toBe(path);
      expect(location.searchParams.get("invite")).toBe("seo-test");
    }
    for (const path of ["/api", "/api/seo-test", "/admin", "/login"]) {
      const response = await request.get(path, { headers, maxRedirects: 0 });
      expect(response.headers().location || "", path).not.toContain(site);
      expect(response.status(), path).toBeLessThan(500);
    }
  }
});

for (const width of [1366, 390]) {
  /** 用真实元素边界核验阅读宽度及站内入口，并保存可供人工检查的整页截图。 */
  test(`三个专题在 ${width}px 下内容与链接可用`, async ({ page, request }) => {
    await page.setViewportSize({ width, height: 1000 });
    const checkedLinks = new Set<string>();
    mkdirSync(evidenceDir, { recursive: true });
    for (const path of guidePaths) {
      await page.goto(path);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      for (const element of await page.locator("main h1, main h2, main h3, main p, main a, main dl, main ol").all()) {
        const box = await element.boundingBox();
        if (!box) continue;
        expect(box.x, `${path} 内容左侧`).toBeGreaterThanOrEqual(-1);
        expect(box.x + box.width, `${path} 内容右侧`).toBeLessThanOrEqual(width + 1);
      }
      for (const link of await page.getByRole("main").getByRole("link").all()) {
        const href = await link.getAttribute("href");
        expect(href).toMatch(/^\/(?!\/)/u);
        if (!href || checkedLinks.has(href)) continue;
        expect((await request.get(href)).status(), href).toBe(200);
        checkedLinks.add(href);
      }
      await page.screenshot({ path: resolve(evidenceDir, `${path.slice(1)}-${width}.png`), fullPage: true });
    }
  });
}
