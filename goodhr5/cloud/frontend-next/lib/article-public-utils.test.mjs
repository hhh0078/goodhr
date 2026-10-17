/** 本文件负责验证公开文章摘要、时区日期、分页边界及站点地图安全分片。 */
import test from "node:test";
import assert from "node:assert/strict";
import { articleDescription, formatArticleDate, positivePage, sitemapIndexXML, sitemapURLSetXML } from "./article-public-utils.ts";

test("文章摘要收拢空白但不切断 emoji，日期固定中国时区", () => {
  assert.equal(articleDescription("第一段\r\n\r\n 第二段\t内容"), "第一段 第二段 内容");
  assert.equal(articleDescription("招聘😀助手", 4), "招聘😀…");
  assert.equal(formatArticleDate("2026-10-16T17:20:00Z"), "2026/10/17 01:20");
  assert.equal(formatArticleDate("bad"), "");
});

test("文章目录分页拒绝数组、负数、小数、溢出以及歧义前缀", () => {
  assert.equal(positivePage(undefined), 1);
  assert.equal(positivePage("12"), 12);
  for (const value of ["0", "-1", "2.5", "01", "+2", "Infinity", "999999999999999999", ["1", "2"]]) assert.equal(positivePage(value), null);
});

test("超过五万篇文章自动拆成轻量分片，索引无全文与未发布字段", () => {
  const xml = sitemapIndexXML("https://example.test", 51001);
  assert.equal((xml.match(/<sitemap>/g) || []).length, 53);
  assert.match(xml, /\/sitemaps\/articles\/52\.xml/);
  assert.doesNotMatch(xml, /\/sitemaps\/articles\/53\.xml/);
  assert.equal((sitemapIndexXML("https://example.test", 0).match(/<sitemap>/g) || []).length, 1);
  assert.throws(() => sitemapIndexXML("https://example.test", 50_000_000));
});

test("XML 转义链接并使用实际更新时间，非法时间不冒充 lastmod", () => {
  const xml = sitemapURLSetXML([{ url: "https://example.test/articles?a=1&b=<x>", updatedAt: "2026-10-17T00:00:00Z" }, { url: "https://example.test/", updatedAt: "bad" }]);
  assert.match(xml, /a=1&amp;b=&lt;x&gt;/);
  assert.equal((xml.match(/<lastmod>/g) || []).length, 1);
  assert.match(xml, /2026-10-17T00:00:00\.000Z/);
});
