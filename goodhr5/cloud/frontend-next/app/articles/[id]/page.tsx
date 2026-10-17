/** 本文件负责无需登录的文章正文与 Article 结构化数据；正文始终按纯文本安全输出。 */

import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import MarketingShell from "@/components/MarketingShell";
import StructuredData from "@/components/StructuredData";
import { articleDescription, formatArticleDate } from "@/lib/article-public-utils";
import { getPublicArticle } from "@/lib/public-articles";
import { absoluteURL, createPageMetadata } from "@/lib/seo";
import styles from "../articles.module.css";

export const dynamic = "force-dynamic";
type ArticleProps = { params: Promise<{ id: string }> };

/** generateMetadata 从已发布正文生成标题、摘要和文章分享信息，不向访客泄露草稿。 */
export async function generateMetadata({ params }: ArticleProps): Promise<Metadata> {
  const { id } = await params;
  const article = await getPublicArticle(id);
  if (!article) notFound();
  const metadata = createPageMetadata({ title: article.title, description: articleDescription(article.content), path: `/articles/${article.id}` });
  return { ...metadata, openGraph: { ...metadata.openGraph, type: "article", publishedTime: article.published_at, modifiedTime: article.updated_at || article.published_at, authors: ["GoodHR"] } };
}

/** ArticlePage 保留原文的换行和空行，供访客、搜索爬虫和 AI 检索直接阅读。 */
export default async function ArticlePage({ params }: ArticleProps) {
  const article = await getPublicArticle((await params).id);
  if (!article) notFound();
  const url = absoluteURL(`/articles/${article.id}`);
  return (
    <>
      <StructuredData data={[
        {
          "@context": "https://schema.org",
          "@type": "Article",
          headline: article.title,
          description: articleDescription(article.content),
          articleBody: article.content,
          datePublished: article.published_at,
          dateModified: article.updated_at || article.published_at,
          inLanguage: "zh-CN",
          mainEntityOfPage: url,
          url,
          author: { "@type": "Organization", name: "GoodHR", url: absoluteURL("/") },
          publisher: { "@type": "Organization", name: "GoodHR", url: absoluteURL("/") },
        },
        {
          "@context": "https://schema.org",
          "@type": "BreadcrumbList",
          itemListElement: [
            { "@type": "ListItem", position: 1, name: "首页", item: absoluteURL("/") },
            { "@type": "ListItem", position: 2, name: "文章/动态", item: absoluteURL("/articles") },
            { "@type": "ListItem", position: 3, name: article.title, item: url },
          ],
        },
      ]} />
      <MarketingShell eyebrow="文章/动态" title={article.title} description="" showIntro={false}>
        <div className="public-container">
          <article className={styles.article}>
            <Link href="/articles" className={styles.back}>← 返回文章目录</Link>
            <h1 className={styles.articleTitle}>{article.title}</h1>
            <time dateTime={article.published_at} className={styles.date}>发布于 {formatArticleDate(article.published_at)}</time>
            <div className={styles.body} data-testid="article-content">{article.content}</div>
          </article>
        </div>
      </MarketingShell>
    </>
  );
}
