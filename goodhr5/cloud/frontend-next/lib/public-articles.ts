/** 本文件负责在服务端读取已公开文章；不携带后台凭证，不缓存草稿或公开状态。 */

import { cache } from "react";
import { ARTICLE_CATALOG_PAGE_SIZE, ARTICLE_ID_PATTERN, ARTICLE_LIST_PAGE_SIZE } from "./article-public-utils";

export type PublicArticleCatalogEntry = { id: string; title: string; published_at: string; updated_at: string };
export type PublicArticleSummary = PublicArticleCatalogEntry & { excerpt: string };
export type PublicArticle = PublicArticleCatalogEntry & { content: string };
export type PublicArticlePage<T extends PublicArticleCatalogEntry = PublicArticleSummary> = { items: T[]; total: number; page: number; page_size: number };

/** publicArticleAPIURL 复用官网公开 API 的服务端地址配置。 */
function publicArticleAPIURL(path: string): string {
  const fallback = process.env.NODE_ENV === "production" ? "https://goodhr.58it.cn" : "http://127.0.0.1:8084";
  return `${(process.env.CLOUD_API_BASE || process.env.NEXT_PUBLIC_CLOUD_API_BASE || fallback).replace(/\/$/, "")}${path}`;
}

/** requestPublicArticles 请求公开文章接口，错误继续交给页面或 XML 路由处理。 */
async function requestPublicArticles(path: string): Promise<Response> {
  return fetch(publicArticleAPIURL(path), { cache: "no-store", signal: AbortSignal.timeout(8000), redirect: "error" });
}

/** readPublicArticlePage 校验公开分页响应，避免接口异常时输出不完整的 sitemap。 */
async function readPublicArticlePage<T extends PublicArticleCatalogEntry>(path: string, page: number, pageSize: number): Promise<PublicArticlePage<T>> {
  const response = await requestPublicArticles(path);
  if (!response.ok) throw new Error(`读取公开文章失败：${response.status}`);
  const data = await response.json();
  if (!Array.isArray(data?.items) || !Number.isSafeInteger(data.total) || data.total < 0 || data.page !== page || data.page_size !== pageSize) throw new Error("公开文章分页响应不完整");
  if (data.items.some((item: PublicArticleCatalogEntry) => !item || !ARTICLE_ID_PATTERN.test(item.id) || typeof item.title !== "string" || !Number.isFinite(new Date(item.published_at).getTime()))) throw new Error("公开文章字段不完整");
  return { items: data.items, total: data.total, page: data.page, page_size: data.page_size };
}

/** getPublicArticlePage 读取访客文章目录，不读取完整正文或任何后台信息。 */
export async function getPublicArticlePage(page = 1): Promise<PublicArticlePage> {
  return readPublicArticlePage<PublicArticleSummary>(`/api/public/articles?page=${page}&page_size=${ARTICLE_LIST_PAGE_SIZE}`, page, ARTICLE_LIST_PAGE_SIZE);
}

/** getPublicArticleCatalogPage 读取轻量链接分片，供动态站点地图使用。 */
export async function getPublicArticleCatalogPage(page = 1): Promise<PublicArticlePage<PublicArticleCatalogEntry>> {
  return readPublicArticlePage<PublicArticleCatalogEntry>(`/api/public/articles/catalog?page=${page}&page_size=${ARTICLE_CATALOG_PAGE_SIZE}`, page, ARTICLE_CATALOG_PAGE_SIZE);
}

/** getPublicArticle 在同一次服务端渲染中复用元数据与正文请求，404 保持不可见。 */
export const getPublicArticle = cache(async function getPublicArticle(id: string): Promise<PublicArticle | null> {
  if (!ARTICLE_ID_PATTERN.test(id)) return null;
  const normalizedID = id.toLowerCase();
  const response = await requestPublicArticles(`/api/public/articles/${encodeURIComponent(normalizedID)}`);
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(`读取公开文章失败：${response.status}`);
  const data = await response.json();
  const article = data?.article;
  if (!article || article.id !== normalizedID || typeof article.title !== "string" || typeof article.content !== "string" || !Number.isFinite(new Date(article.published_at).getTime())) throw new Error("公开文章正文响应不完整");
  return article as PublicArticle;
});
