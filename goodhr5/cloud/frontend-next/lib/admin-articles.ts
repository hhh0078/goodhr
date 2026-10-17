/** 本文件复用云端请求管理官网文章，并生成供 SEO 发布使用的完整链接。 */
"use client";

import { cloudRequest } from "./admin-api";
import { absoluteURL } from "./seo";
import { ARTICLE_ID_PATTERN } from "./article-public-utils";

export type AdminArticle = {
  id: string;
  title: string;
  content: string;
  excerpt?: string;
  published_at: string;
  is_published: boolean;
  created_at: string;
  updated_at: string;
};

export type ArticleInput = Pick<AdminArticle, "title" | "content" | "published_at" | "is_published">;

/** articlePublicURL 用稳定文章标识生成官网规范地址，避免复制后台或当前预览域名。 */
export function articlePublicURL(id: string): string {
  return absoluteURL(`/articles/${encodeURIComponent(id)}`);
}

/** fetchAllArticleLinks 从单次导出快照读取全部已公开链接，不受当前表格分页和筛选影响。 */
export async function fetchAllArticleLinks(): Promise<string> {
  const data = await cloudRequest("/api/admin/articles/links");
  const items: unknown = data?.items;
  if (!Array.isArray(items) || !items.every((item) => typeof item?.id === "string" && ARTICLE_ID_PATTERN.test(item.id))) {
    throw new Error("文章链接没取完整，请再试一次。");
  }
  return [...new Set(items.map((item) => articlePublicURL(item.id)))].join("\n");
}

/** saveArticle 创建或更新文章，保持用户正文的原始换行。 */
export async function saveArticle(id: string | undefined, input: ArticleInput): Promise<AdminArticle> {
  const data = await cloudRequest(`/api/admin/articles${id ? `/${encodeURIComponent(id)}` : ""}`, {
    method: id ? "PUT" : "POST",
    body: input,
  });
  return data.article;
}
