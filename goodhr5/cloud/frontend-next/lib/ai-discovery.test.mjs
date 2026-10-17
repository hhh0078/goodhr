/** 本文件验证 AI 公开索引的标题安全、分页完整性、全文换行以及故障响应，不依赖私有接口。 */

import test from "node:test";
import assert from "node:assert/strict";
import { aiArticleCatalogMarkdown, aiArticleCatalogPage, aiArticleMarkdown, aiDiscoveryResponse, aiMarkdownText, assertCompleteAIArticleCatalog, unavailableAIDiscovery } from "./ai-discovery.ts";

const first = { id: "11111111-1111-4111-8111-111111111111", title: "招聘实践", published_at: "2026-10-17T16:00:00Z", updated_at: "2026-10-17T16:00:00Z" };
const second = { ...first, id: "22222222-2222-4222-8222-222222222222", title: "第二篇" };

/** publicURL 在单测中构造固定站点地址，便于核验所有链接均来自官方公开路由。 */
function publicURL(path) { return `https://example.test${path}`; }

/** catalog 构造单页公开文章目录，允许测试真实边界和不完整接口响应。 */
function catalog(items = [first], page = 1, pageSize = 1000, total = items.length) { return { items, page, page_size: pageSize, total }; }

test("Markdown 标题收拢换行并转义链接、HTML 与标题语法", () => {
  assert.equal(aiMarkdownText("  招聘\r\n[点我](https://evil.test) <script>\t # 新页\u0000 "), "招聘 \\[点我\\]\\(https://evil\\.test\\) &lt;script&gt; \\# 新页");
  const text = aiArticleCatalogMarkdown(catalog([{ ...first, title: "正常标题\n- [伪造](https://evil.test)" }]), publicURL);
  assert.equal(text.split("\n").filter((line) => line.startsWith("- [正常标题")).length, 1);
  assert.doesNotMatch(text, /\n- \[伪造\]/u);
  assert.doesNotMatch(text, /\]\(https:\/\/evil\.test\)/u);
});

test("AI 目录页必须是带 txt 后缀的正整数，并能精确计算分页偏移", () => {
  assert.equal(aiArticleCatalogPage("1.txt", 1000), 1);
  assert.equal(aiArticleCatalogPage("52.txt", 1000), 52);
  for (const value of ["1", "1.xml", "01.txt", "0.txt", "-1.txt", "+2.txt", "1.5.txt", "1.txt.txt", "1.TXT", " 1.txt", "9007199254740991.txt"]) assert.equal(aiArticleCatalogPage(value, 1000), null);
  for (const pageSize of [0, -1, 1.5, Number.MAX_VALUE]) assert.equal(aiArticleCatalogPage("1.txt", pageSize), null);
});

test("AI 目录拒绝漏项、越量、错误页码和大小写重复的链接", () => {
  assert.doesNotThrow(() => assertCompleteAIArticleCatalog(catalog([first, second], 1, 2), 1, 2));
  assert.doesNotThrow(() => assertCompleteAIArticleCatalog(catalog([first], 2, 2, 3), 2, 2));
  assert.doesNotThrow(() => assertCompleteAIArticleCatalog(catalog([], 1, 1000, 0), 1, 1000));
  for (const invalid of [catalog([first], 1, 2, 2), catalog([first, second], 1, 2, 1), catalog([first], 2, 2, 1), catalog([first], 1, 1, -1), catalog([{ ...first, id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" }, { ...first, id: "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA" }], 1, 2, 2)]) {
    assert.throws(() => assertCompleteAIArticleCatalog(invalid, 1, invalid.page_size), /不完整/u);
  }
  assert.throws(() => assertCompleteAIArticleCatalog(catalog([], Number.MAX_SAFE_INTEGER, 1000, 0), Number.MAX_SAFE_INTEGER, 1000), /不完整/u);
});

test("公开目录含全文和网页链接，分页保留上一页和下一页，尾页不产生多余页", () => {
  const middle = aiArticleCatalogMarkdown(catalog([first, second], 2, 2, 5), publicURL);
  assert.match(middle, /当前第 2 \/ 3 页/u);
  assert.match(middle, /\[招聘实践\]\(https:\/\/example.test\/articles\/11111111-1111-4111-8111-111111111111\/content.md\)/u);
  assert.match(middle, /\[网页\]\(https:\/\/example.test\/articles\/11111111-1111-4111-8111-111111111111\)/u);
  assert.match(middle, /\[上一页\]\(https:\/\/example.test\/llms\/articles\/1.txt\)/u);
  assert.match(middle, /\[下一页\]\(https:\/\/example.test\/llms\/articles\/3.txt\)/u);
  const last = aiArticleCatalogMarkdown(catalog([first], 52, 1000, 51001), publicURL);
  assert.match(last, /当前第 52 \/ 52 页/u);
  assert.doesNotMatch(last, /\[下一页\]/u);
  assert.doesNotMatch(last, /\/53.txt/u);
});

test("空库仍提供可读目录入口，不假造文章或下一页", () => {
  const body = aiArticleCatalogMarkdown(catalog([], 1, 1000, 0), publicURL);
  assert.match(body, /目前还没有已公开文章/u);
  assert.match(body, /\/llms\/articles\/1.txt/u);
  assert.doesNotMatch(body, /content.md|\[下一页\]|\[上一页\]/u);
});

test("全文提供 canonical 地址、发布时间并完整保留长正文与所有空行", () => {
  const content = `第一段\n\n第二段\n<script>只是原文</script>\n${"长正文😀".repeat(2000)}\n\n结尾`;
  const body = aiArticleMarkdown({ ...first, title: "标题\n# 额外标题", content }, publicURL);
  assert.match(body, /^# 标题 \\# 额外标题\n/u);
  assert.match(body, /发布时间：2026-10-17T16:00:00.000Z/u);
  assert.match(body, /原文：https:\/\/example.test\/articles\/11111111-1111-4111-8111-111111111111/u);
  assert.ok(body.endsWith(`## 正文\n\n${content}\n`));
});

test("AI 文件响应始终使用纯文本、禁止缓存与 MIME 嗅探，故障明确可重试", async () => {
  const normal = aiDiscoveryResponse("<script>原文</script>\n");
  assert.equal(normal.status, 200);
  assert.equal(normal.headers.get("Content-Type"), "text/plain; charset=utf-8");
  assert.equal(normal.headers.get("Cache-Control"), "no-store");
  assert.equal(normal.headers.get("X-Content-Type-Options"), "nosniff");
  assert.equal(normal.headers.get("Retry-After"), null);
  assert.equal(await normal.text(), "<script>原文</script>\n");
  const unavailable = unavailableAIDiscovery();
  assert.equal(unavailable.status, 503);
  assert.equal(unavailable.headers.get("Retry-After"), "60");
  assert.equal(unavailable.headers.get("Cache-Control"), "no-store");
  assert.doesNotMatch(await unavailable.text(), /没有已公开文章/u);
  assert.equal(aiDiscoveryResponse("文章不存在", 404).headers.get("Retry-After"), null);
});
