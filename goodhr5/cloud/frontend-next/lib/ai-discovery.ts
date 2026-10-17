/** 本文件负责 AI 阅读文件的分页校验、Markdown 安全标题及纯文本响应，不读取任何私有业务数据。 */

import type { PublicArticle, PublicArticleCatalogEntry, PublicArticlePage } from "./public-articles";

type PublicURL = (path: string) => string;

/** aiMarkdownText 将标题收拢为一行并转义 Markdown，防止文章标题制造额外链接或目录项。 */
export function aiMarkdownText(value: string): string {
  return value.replace(/[\u0000-\u0008\u000e-\u001f\u007f]/gu, "").replace(/\s+/gu, " ").trim()
    .replace(/&/gu, "&amp;").replace(/</gu, "&lt;").replace(/>/gu, "&gt;")
    .replace(/[\\`*_[\]{}()#+.!|~\-]/gu, "\\$&");
}

/** aiArticleCatalogPage 只接受带 .txt 后缀的正整数页，拒绝歧义页码与无法精确计算的偏移。 */
export function aiArticleCatalogPage(value: string, pageSize: number): number | null {
  if (!/^[1-9]\d*\.txt$/u.test(value) || !Number.isSafeInteger(pageSize) || pageSize <= 0) return null;
  const page = Number(value.slice(0, -4));
  return Number.isSafeInteger(page) && Number.isSafeInteger((page - 1) * pageSize) ? page : null;
}

/** assertCompleteAIArticleCatalog 校验完整分页和唯一链接，接口漏项时应重试而非公布残缺目录。 */
export function assertCompleteAIArticleCatalog(catalog: PublicArticlePage<PublicArticleCatalogEntry>, page: number, pageSize: number): void {
  const offset = (page - 1) * pageSize;
  if (!Number.isSafeInteger(page) || page < 1 || !Number.isSafeInteger(pageSize) || pageSize < 1 || !Number.isSafeInteger(offset)
    || !Number.isSafeInteger(catalog.total) || catalog.total < 0 || catalog.page !== page || catalog.page_size !== pageSize
    || catalog.items.length !== Math.min(pageSize, Math.max(0, catalog.total - offset))
    || new Set(catalog.items.map((article) => article.id.toLowerCase())).size !== catalog.items.length) {
    throw new Error("公开文章 AI 目录分页不完整");
  }
}

/** aiArticleCatalogMarkdown 输出当前页全部公开文章，并用真实前后页链接串起完整目录。 */
export function aiArticleCatalogMarkdown(catalog: PublicArticlePage<PublicArticleCatalogEntry>, publicURL: PublicURL, heading = "## 公开文章"): string {
  const pages = Math.max(1, Math.ceil(catalog.total / catalog.page_size));
  const lines = [heading, "", `已公开文章共 ${catalog.total} 篇；当前第 ${catalog.page} / ${pages} 页，每页最多 ${catalog.page_size} 篇。`, ""];
  if (catalog.items.length === 0) lines.push("目前还没有已公开文章。", "");
  for (const article of catalog.items) {
    const path = `/articles/${article.id.toLowerCase()}`;
    lines.push(`- [${aiMarkdownText(article.title)}](${publicURL(`${path}/content.md`)})：[网页](${publicURL(path)})；发布于 ${new Date(article.published_at).toISOString()}。`);
  }
  lines.push("", `- [文章网页目录](${publicURL("/articles")})`, `- [AI 文章目录首页](${publicURL("/llms/articles/1.txt")})`);
  if (catalog.page > 1) lines.push(`- [上一页](${publicURL(`/llms/articles/${catalog.page - 1}.txt`)})`);
  if (catalog.page < pages) lines.push(`- [下一页](${publicURL(`/llms/articles/${catalog.page + 1}.txt`)})：后续文章请继续读取下一页目录。`);
  lines.push(`- [站点说明与官方页面](${publicURL("/llms.txt")})`, "");
  return lines.join("\n");
}

/** aiArticleMarkdown 输出单篇已公开文章的完整正文，保留原文全部换行与空行。 */
export function aiArticleMarkdown(article: PublicArticle, publicURL: PublicURL): string {
  return `# ${aiMarkdownText(article.title)}\n\n发布时间：${new Date(article.published_at).toISOString()}\n\n原文：${publicURL(`/articles/${article.id.toLowerCase()}`)}\n\n## 正文\n\n${article.content}\n`;
}

/** aiDiscoveryResponse 统一输出不缓存的 UTF-8 纯文本，禁止浏览器将文章当 HTML 执行。 */
export function aiDiscoveryResponse(body: string, status = 200): Response {
  return new Response(body, {
    status,
    headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff", ...(status === 503 ? { "Retry-After": "60" } : {}) },
  });
}

/** unavailableAIDiscovery 在上游故障时返回可重试状态，避免把故障误报为文章已删除或目录为空。 */
export function unavailableAIDiscovery(): Response {
  return aiDiscoveryResponse("公开内容暂时没有准备好，请稍后再试。\n", 503);
}
