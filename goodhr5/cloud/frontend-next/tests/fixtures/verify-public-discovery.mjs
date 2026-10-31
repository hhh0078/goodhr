/** 本文件在隔离容器内验证候选官网的动态 sitemap、robots 与 AI 阅读文件；仅请求显式指定的 loopback 合成服务，不接触生产数据。 */

import assert from "node:assert/strict";

const SITE = "https://goodhr.58it.cn";
const FIXED_PATHS = ["/", "/features", "/recruitment-automation", "/recruitment-auto-reply", "/hr-tools", "/pricing", "/videos", "/articles", "/download", "/contact"];
const FIXED_PAGES = [["首页", "/"], ["功能介绍", "/features"], ["多平台招聘自动化", "/recruitment-automation"], ["招聘消息自动回复", "/recruitment-auto-reply"], ["HR与猎头工具", "/hr-tools"], ["产品定价", "/pricing"], ["视频教程", "/videos"], ["文章/动态", "/articles"], ["下载", "/download"], ["联系我们", "/contact"]];
const CONTENT = '第一段：每天少做一点重复工作。\n\n第二段：换行和空行都会保留。\n<script>window.__articleInjected=true</script>\n最后一行 & <b>这是纯文本，不是粗体标签</b>。';
const passed = [];

/** loopbackOrigin 只接受显式 HTTP 回环 IP 源，拒绝域名、认证、额外路径及非本机目标。 */
function loopbackOrigin(name, value) {
  assert.equal(typeof value, "string", `必须显式提供 ${name}`);
  assert.match(value, /^http:\/\/(?:127\.0\.0\.1|\[::1\])(?::[1-9]\d{0,4})?\/?$/u, `${name} 只能是 HTTP loopback origin`);
  const url = new URL(value);
  assert.ok(["127.0.0.1", "[::1]"].includes(url.hostname), `${name} 必须使用回环 IP`);
  assert.equal(url.username + url.password + url.search + url.hash, "", `${name} 不允许认证或查询参数`);
  assert.equal(url.pathname, "/", `${name} 不允许附带业务路径`);
  return url.origin;
}

/** articleID 生成与合成 API 相同的公开文章 UUID，不读取真实文章。 */
function articleID(index) { return `10000000-0000-4000-8000-${String(index).padStart(12, "0")}`; }

/** canonicalURL 返回预期网页地址，仅参与断言，绝不作为请求目标。 */
function canonicalURL(path) { return path === "/" ? SITE : `${SITE}${path}`; }

/** requestText 禁止跳转与凭据，并为每次隔离请求设置超时，不跟随正文中的任何地址。 */
async function requestText(origin, path) {
  assert.ok(path.startsWith("/") && !path.startsWith("//"), "请求路径必须是站内绝对路径");
  const response = await fetch(`${origin}${path}`, { redirect: "error", credentials: "omit", signal: AbortSignal.timeout(20000) });
  return { response, text: await response.text() };
}

/** setMode 仅调用指定回环合成服务的状态接口，不访问官网管理接口。 */
async function setMode(fixture, mode) {
  assert.ok(["normal", "deleted", "empty", "thousand", "overflow", "many", "unavailable", "partial"].includes(mode), "不支持的合成状态");
  const { response, text } = await requestText(fixture, `/__test__/articles?mode=${mode}`);
  assert.equal(response.status, 200, "合成服务状态切换失败");
  assert.equal(JSON.parse(text).mode, mode, "合成服务未进入指定状态");
}

/** check 按序记录可读检查名称，失败立即中止，避免后续状态影响诊断。 */
async function check(name, run) {
  await run();
  passed.push({ name, status: "PASS" });
  process.stdout.write(`${JSON.stringify({ name, status: "PASS" })}\n`);
}

/** assertFresh 核验机器文件类型和禁止缓存，防止发布状态被静态缓存掩盖。 */
function assertFresh(response, kind = "text/plain") {
  assert.ok(response.headers.get("content-type")?.includes(kind), "响应 MIME 类型不正确");
  assert.equal(response.headers.get("cache-control"), "no-store", "机器文件必须禁止缓存");
  if (kind === "text/plain") assert.equal(response.headers.get("x-content-type-options"), "nosniff", "纯文本必须禁止 MIME 嗅探");
}

/** xmlLocations 从小型测试 XML 中读取 loc，用于校验链接集合、唯一性及分片数。 */
function xmlLocations(text) { return [...text.matchAll(/<loc>([^<]+)<\/loc>/gu)].map((match) => match[1]); }

/** assertURLSet 校验 XML 根类型、链接数量和去重，避免静默漏页或重复链接。 */
function assertURLSet(text, count) {
  assert.match(text, /<urlset\s/u);
  assert.doesNotMatch(text, /<sitemapindex/u);
  assert.equal((text.match(/<url>/gu) || []).length, count);
  const urls = xmlLocations(text);
  assert.equal(urls.length, count);
  assert.equal(new Set(urls).size, count);
  return urls;
}

/** articleMarkdownLinks 只读取实际 Markdown 文章条目，不把导航链接计入文章数。 */
function articleMarkdownLinks(text) {
  return [...text.matchAll(/^- \[[^\n]*\]\((https:\/\/goodhr\.58it\.cn\/articles\/[0-9a-f-]+\/content\.md)\)/gmu)].map((match) => match[1]);
}

/** assertFailure 核验上游故障可重试，不能把失败误报为没有文章。 */
async function assertFailure(origin, path) {
  const { response, text } = await requestText(origin, path);
  assert.equal(response.status, 503, `${path} 应返回可重试故障`);
  assert.equal(response.headers.get("cache-control"), "no-store", `${path} 故障不能缓存`);
  assert.equal(response.headers.get("retry-after"), "60", `${path} 应说明重试时间`);
  assert.doesNotMatch(text, /<urlset|<sitemapindex|目前还没有已公开文章/u, `${path} 不能伪造空目录`);
}

/** decodedHTMLText 解码正文中的标准 HTML 实体，只用于验证服务端按纯文本输出文章。 */
function decodedHTMLText(value) {
  return value.replace(/<!--[^]*?-->/gu, "").replace(/&lt;/gu, "<").replace(/&gt;/gu, ">").replace(/&quot;/gu, '"').replace(/&#x27;/gu, "'").replace(/&amp;/gu, "&");
}

/** main 在隔离候选与合成 API 之间执行完整回归，退出前恢复正常合成状态。 */
async function main() {
  const origin = loopbackOrigin("DISCOVERY_TEST_ORIGIN", process.argv[2] || process.env.DISCOVERY_TEST_ORIGIN);
  const fixture = loopbackOrigin("ARTICLE_FIXTURE_ORIGIN", process.argv[3] || process.env.ARTICLE_FIXTURE_ORIGIN);
  assert.notEqual(origin, fixture, "候选服务与合成 API 必须使用不同 origin");
  let robotsText = "";
  try {
    await setMode(fixture, "normal");
    await check("normal.root_sitemap_10_pages_and_21_articles", async () => {
      const { response, text } = await requestText(origin, "/sitemap.xml");
      assert.equal(response.status, 200);
      assertFresh(response, "application/xml");
      const urls = assertURLSet(text, FIXED_PATHS.length + 21);
      for (const path of FIXED_PATHS) assert.ok(urls.includes(canonicalURL(path)), `站点地图缺少 ${path}`);
      for (let index = 1; index <= 21; index++) assert.ok(urls.includes(canonicalURL(`/articles/${articleID(index)}`)));
      assert.doesNotMatch(text, /\/admin|\/login|\/api\//u);
    });
    await check("normal.fixed_and_article_sitemaps", async () => {
      const fixed = await requestText(origin, "/sitemaps/site.xml");
      assert.equal(fixed.response.status, 200);
      assertFresh(fixed.response, "application/xml");
      assert.deepEqual(new Set(assertURLSet(fixed.text, FIXED_PATHS.length)), new Set(FIXED_PATHS.map(canonicalURL)));
      const article = await requestText(origin, "/sitemaps/articles/1.xml");
      assert.equal(article.response.status, 200);
      assertURLSet(article.text, 21);
      assert.match(article.text, /<lastmod>2026-10-17T03:00:00\.000Z<\/lastmod>/u);
    });
    await check("normal.robots_dynamic_rules_and_single_sitemap", async () => {
      const { response, text } = await requestText(origin, "/robots.txt");
      assert.equal(response.status, 200);
      assertFresh(response);
      assert.match(text, /^User-agent: \*$/mu);
      assert.match(text, /^Allow: \/$/mu);
      for (const path of ["/admin", "/login", "/api/"]) assert.ok(text.includes(`Disallow: ${path}\n`));
      assert.deepEqual(text.match(/^Sitemap: .+$/gmu), [`Sitemap: ${SITE}/sitemap.xml`]);
      assert.ok(text.includes(`${SITE}/llms.txt`));
      robotsText = text;
    });
    await check("normal.llms_standard_pages_article_links_and_metadata", async () => {
      const { response, text } = await requestText(origin, "/llms.txt");
      assert.equal(response.status, 200);
      assertFresh(response);
      assert.match(text, /^# GoodHR\n/u);
      for (const [title, path] of FIXED_PAGES) assert.ok(text.includes(`[${title}](${canonicalURL(path)})`), `AI 目录缺少 ${path}`);
      assert.equal(articleMarkdownLinks(text).length, 21);
      assert.ok(text.includes(`${SITE}/articles/${articleID(1)}/content.md`));
      assert.match(text, /2026-10-17T02:30:00\.000Z/u);
      assert.doesNotMatch(text, /\/admin\/|平台与自动化检索词/u);
    });
    await check("normal.markdown_full_content_and_canonical_headers", async () => {
      const { response, text } = await requestText(origin, `/articles/${articleID(1)}/content.md`);
      assert.equal(response.status, 200);
      assertFresh(response);
      assert.equal(response.headers.get("link"), `<${SITE}/articles/${articleID(1)}>; rel="canonical"`);
      assert.match(text, /^# GoodHR 招聘实用分享：/u);
      assert.ok(text.includes("发布时间：2026-10-17T02:30:00.000Z"));
      assert.ok(text.includes(`原文：${SITE}/articles/${articleID(1)}`));
      assert.ok(text.endsWith(`## 正文\n\n${CONTENT}\n`), "完整正文或空行丢失");
    });
    await check("normal.ssr_plain_text_canonical_and_article_jsonld", async () => {
      const { response, text } = await requestText(origin, `/articles/${articleID(1)}`);
      assert.equal(response.status, 200);
      assert.ok(response.headers.get("content-type")?.includes("text/html"));
      const body = text.match(/<div[^>]*data-testid="article-content"[^>]*>([^]*?)<\/div>/u);
      assert.ok(body, "原始 HTML 必须直接包含文章正文");
      assert.equal(decodedHTMLText(body[1]), CONTENT);
      assert.ok(text.includes(`rel="canonical" href="${SITE}/articles/${articleID(1)}"`));
      assert.ok(text.includes("&lt;script&gt;window.__articleInjected=true&lt;/script&gt;"));
      assert.doesNotMatch(text, /<script[^>]*>\s*window\.__articleInjected/u);
      const structured = [...text.matchAll(/<script[^>]*type="application\/ld\+json"[^>]*>([^]*?)<\/script>/gu)].flatMap((match) => [JSON.parse(match[1])].flat());
      const article = structured.find((entry) => entry["@type"] === "Article");
      assert.ok(article, "原始 HTML 缺少 Article JSON-LD");
      assert.equal(article.articleBody, CONTENT);
      assert.equal(article.mainEntityOfPage, `${SITE}/articles/${articleID(1)}`);
      assert.ok(structured.some((entry) => entry["@type"] === "BreadcrumbList"));
      const navigation = text.match(/<nav[^>]*aria-label="官网导航"[^>]*>([^]*?)<\/nav>/u);
      assert.ok(navigation, "原始 HTML 缺少官网导航");
      const links = [...navigation[1].matchAll(/<a\b[^>]*href="([^"]+)"[^>]*>([^]*?)<\/a>/gu)];
      assert.deepEqual(links.map((entry) => decodedHTMLText(entry[2].replace(/<[^>]+>/gu, ""))), ["首页", "功能介绍", "定价", "视频教程", "动态", "下载", "联系作者"]);
      assert.deepEqual(links.map((entry) => entry[1]), ["/", "/features", "/pricing", "/videos", "/articles", "/download", "/contact"]);
    });
    await check("normal.invalid_or_missing_markdown_404", async () => {
      for (const id of ["not-a-uuid", articleID(99)]) {
        const { response, text } = await requestText(origin, `/articles/${id}/content.md`);
        assert.equal(response.status, 404);
        assertFresh(response);
        assert.doesNotMatch(text, /第一段：每天少做一点重复工作/u);
      }
    });
    await setMode(fixture, "deleted");
    await check("deleted.sitemap_and_llms_remove_article_immediately", async () => {
      const sitemap = await requestText(origin, "/sitemap.xml");
      assert.equal(sitemap.response.status, 200);
      assertURLSet(sitemap.text, FIXED_PATHS.length + 20);
      assert.ok(!sitemap.text.includes(articleID(1)));
      const llms = await requestText(origin, "/llms.txt");
      assert.equal(llms.response.status, 200);
      assert.equal(articleMarkdownLinks(llms.text).length, 20);
      assert.ok(!llms.text.includes(articleID(1)));
      const shard = await requestText(origin, "/sitemaps/articles/1.xml");
      assertURLSet(shard.text, 20);
      assert.ok(!shard.text.includes(articleID(1)));
      assert.equal((await requestText(origin, `/articles/${articleID(1)}/content.md`)).response.status, 404);
    });
    await setMode(fixture, "empty");
    await check("empty.fixed_pages_remain_and_first_ai_page_is_readable", async () => {
      const sitemap = await requestText(origin, "/sitemap.xml");
      assert.equal(sitemap.response.status, 200);
      assert.deepEqual(new Set(assertURLSet(sitemap.text, FIXED_PATHS.length)), new Set(FIXED_PATHS.map(canonicalURL)));
      for (const path of ["/llms.txt", "/llms/articles/1.txt"]) {
        const result = await requestText(origin, path);
        assert.equal(result.response.status, 200);
        assertFresh(result.response);
        assert.match(result.text, /目前还没有已公开文章/u);
        assert.equal(articleMarkdownLinks(result.text).length, 0);
      }
      for (const path of ["/llms/articles/2.txt", "/sitemaps/articles/1.xml", `/articles/${articleID(1)}/content.md`]) assert.equal((await requestText(origin, path)).response.status, 404);
    });
    await setMode(fixture, "thousand");
    await check("boundary.1000_articles_stay_in_single_root_urlset", async () => {
      const sitemap = await requestText(origin, "/sitemap.xml");
      assert.equal(sitemap.response.status, 200);
      assertURLSet(sitemap.text, FIXED_PATHS.length + 1000);
      const llms = await requestText(origin, "/llms.txt");
      assert.equal(llms.response.status, 200);
      assert.equal(articleMarkdownLinks(llms.text).length, 1000);
      assert.ok(!llms.text.includes("/llms/articles/2.txt"));
    });
    await setMode(fixture, "overflow");
    await check("boundary.1001_articles_use_three_sitemaps_and_one_tail_entry", async () => {
      const sitemap = await requestText(origin, "/sitemap.xml");
      assert.equal(sitemap.response.status, 200);
      assert.match(sitemap.text, /<sitemapindex\s/u);
      assert.deepEqual(xmlLocations(sitemap.text), [`${SITE}/sitemaps/site.xml`, `${SITE}/sitemaps/articles/1.xml`, `${SITE}/sitemaps/articles/2.xml`]);
      const tail = await requestText(origin, "/sitemaps/articles/2.xml");
      assert.equal(tail.response.status, 200);
      assert.deepEqual(assertURLSet(tail.text, 1), [`${SITE}/articles/${articleID(1001)}`]);
      const aiTail = await requestText(origin, "/llms/articles/2.txt");
      assert.equal(aiTail.response.status, 200);
      assertFresh(aiTail.response);
      assert.deepEqual(articleMarkdownLinks(aiTail.text), [`${SITE}/articles/${articleID(1001)}/content.md`]);
      assert.ok(aiTail.text.includes("[上一页]"));
      assert.ok(!aiTail.text.includes("[下一页]"));
    });
    await setMode(fixture, "many");
    await check("many.51001_articles_have_52_complete_sitemap_shards", async () => {
      const sitemap = await requestText(origin, "/sitemap.xml");
      assert.equal(sitemap.response.status, 200);
      const urls = xmlLocations(sitemap.text);
      assert.equal(urls.length, 53);
      assert.equal(new Set(urls).size, 53);
      assert.equal(urls.at(-1), `${SITE}/sitemaps/articles/52.xml`);
      const tail = await requestText(origin, "/sitemaps/articles/52.xml");
      assert.equal(tail.response.status, 200);
      assert.deepEqual(assertURLSet(tail.text, 1), [`${SITE}/articles/${articleID(51001)}`]);
    });
    await check("many.ai_directory_chains_second_and_last_pages_without_truncation", async () => {
      const root = await requestText(origin, "/llms.txt");
      assert.equal(root.response.status, 200);
      assert.equal(articleMarkdownLinks(root.text).length, 1000);
      assert.ok(root.text.includes("/llms/articles/2.txt"));
      const second = await requestText(origin, "/llms/articles/2.txt");
      assert.equal(second.response.status, 200);
      const links = articleMarkdownLinks(second.text);
      assert.equal(links.length, 1000);
      assert.equal(links[0], `${SITE}/articles/${articleID(1001)}/content.md`);
      assert.equal(links.at(-1), `${SITE}/articles/${articleID(2000)}/content.md`);
      assert.ok(second.text.includes("/llms/articles/3.txt"));
      const tail = await requestText(origin, "/llms/articles/52.txt");
      assert.equal(tail.response.status, 200);
      assert.deepEqual(articleMarkdownLinks(tail.text), [`${SITE}/articles/${articleID(51001)}/content.md`]);
      assert.ok(!tail.text.includes("[下一页]"));
    });
    await check("many.invalid_and_out_of_range_ai_pages_404", async () => {
      for (const page of ["53.txt", "0.txt", "-1.txt", "01.txt", "1", "1.xml", "9007199254740991.txt"]) {
        const result = await requestText(origin, `/llms/articles/${page}`);
        assert.equal(result.response.status, 404, `不应接受目录页 ${page}`);
        assertFresh(result.response);
      }
    });
    await setMode(fixture, "unavailable");
    await check("unavailable.machine_indexes_and_markdown_fail_with_retry", async () => {
      for (const path of ["/sitemap.xml", "/sitemaps/articles/1.xml", "/llms.txt", "/llms/articles/1.txt", `/articles/${articleID(1)}/content.md`]) await assertFailure(origin, path);
    });
    await check("unavailable.robots_and_fixed_sitemap_remain_independent", async () => {
      const robots = await requestText(origin, "/robots.txt");
      assert.equal(robots.response.status, 200);
      assertFresh(robots.response);
      assert.equal(robots.text, robotsText);
      const fixed = await requestText(origin, "/sitemaps/site.xml");
      assert.equal(fixed.response.status, 200);
      assertURLSet(fixed.text, FIXED_PATHS.length);
    });
    await setMode(fixture, "partial");
    await check("partial.incomplete_api_page_does_not_publish_truncated_indexes", async () => {
      for (const path of ["/sitemap.xml", "/sitemaps/articles/1.xml", "/llms.txt", "/llms/articles/1.txt"]) await assertFailure(origin, path);
      assert.equal((await requestText(origin, "/robots.txt")).response.status, 200);
    });
  } finally {
    await setMode(fixture, "normal");
  }
  process.stdout.write(`${JSON.stringify({ status: "PASS", total: passed.length, checks: passed, fixtureRestored: true })}\n`);
}

main().catch((error) => {
  process.stderr.write(`${JSON.stringify({ status: "FAIL", totalPassed: passed.length, checks: passed, error: error instanceof Error ? error.message : "未知测试错误" })}\n`);
  process.exitCode = 1;
});
