/** 本文件负责公开文章的日期、摘要与 XML 安全输出，不包含浏览器或接口状态。 */

export const ARTICLE_CATALOG_PAGE_SIZE = 1000;
export const ARTICLE_LIST_PAGE_SIZE = 20;
export const ARTICLE_ID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** articleDescription 将纯文本压缩为搜索摘要，正文中的换行仍由文章页完整保留。 */
export function articleDescription(content: string, limit = 160): string {
  const text = content.replace(/\s+/gu, " ").trim();
  return [...text].length > limit ? `${[...text].slice(0, limit - 1).join("")}…` : text;
}

/** formatArticleDate 按网站使用的中国时区输出可读日期，避免服务器时区改变发布日期。 */
export function formatArticleDate(value: string): string {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  return new Intl.DateTimeFormat("zh-CN", { timeZone: "Asia/Shanghai", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).format(date);
}

/** positivePage 读取公开分页参数，不接受小数、负数或超过安全范围的页数。 */
export function positivePage(value: string | string[] | undefined): number | null {
  if (value === undefined) return 1;
  if (Array.isArray(value) || !/^[1-9]\d*$/.test(value)) return null;
  const page = Number(value);
  return Number.isSafeInteger(page) ? page : null;
}

/** escapeXML 转义 XML 特殊字符，避免文章链接破坏站点地图文档。 */
export function escapeXML(value: string): string {
  return value.replace(/[<>&"']/g, (character) => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;", "'": "&apos;" })[character]!);
}

/** sitemapIndexXML 根据实时文章数量生成索引，每个文章分片最多包含一千个链接。 */
export function sitemapIndexXML(siteURL: string, total: number): string {
  const count = Math.ceil(total / ARTICLE_CATALOG_PAGE_SIZE);
  if (!Number.isSafeInteger(total) || total < 0 || count > 49999) throw new Error("文章站点地图数量超出索引范围");
  const urls = [`${siteURL}/sitemaps/site.xml`, ...Array.from({ length: count }, (_, index) => `${siteURL}/sitemaps/articles/${index + 1}.xml`)];
  return `<?xml version="1.0" encoding="UTF-8"?>\n<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${urls.map((url) => `<sitemap><loc>${escapeXML(url)}</loc></sitemap>`).join("")}</sitemapindex>`;
}

/** sitemapURLSetXML 输出公开页面地址与真实更新时间，不伪造每次请求的修改日期。 */
export function sitemapURLSetXML(entries: Array<{ url: string; updatedAt?: string }>): string {
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${entries.map(({ url, updatedAt }) => `<url><loc>${escapeXML(url)}</loc>${updatedAt && Number.isFinite(new Date(updatedAt).getTime()) ? `<lastmod>${escapeXML(new Date(updatedAt).toISOString())}</lastmod>` : ""}</url>`).join("")}</urlset>`;
}

/** articleSitemapResponse 返回不缓存的 XML，确保文章发布、撤回和删除在下次抓取时立即生效。 */
export function articleSitemapResponse(body: string): Response {
  return new Response(body, { headers: { "Content-Type": "application/xml; charset=utf-8", "Cache-Control": "no-store" } });
}

/** unavailableSitemap 返回可重试状态，防止上游故障被搜索引擎误认为文章已全部删除。 */
export function unavailableSitemap(): Response {
  return new Response("站点地图暂时没有准备好，请稍后再试。", { status: 503, headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": "no-store", "Retry-After": "60" } });
}
