/** 本文件负责无需登录的文章目录，服务端输出可抓取的标题、摘要和分页链接。 */

import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import MarketingShell from "@/components/MarketingShell";
import StructuredData from "@/components/StructuredData";
import { ARTICLE_LIST_PAGE_SIZE, formatArticleDate, positivePage } from "@/lib/article-public-utils";
import { getPublicArticlePage } from "@/lib/public-articles";
import { absoluteURL, createPageMetadata } from "@/lib/seo";
import styles from "./articles.module.css";

export const dynamic = "force-dynamic";
type ArticleListProps = { searchParams: Promise<{ page?: string | string[] }> };

/** generateMetadata 为每一页文章目录生成独立的 canonical，避免分页都指向首页。 */
export async function generateMetadata({ searchParams }: ArticleListProps): Promise<Metadata> {
  const page = positivePage((await searchParams).page);
  if (!page) return { title: "文章目录", robots: { index: false, follow: true } };
  return createPageMetadata({
    title: page === 1 ? "文章/动态 - GoodHR 招聘实用分享与产品动态" : `文章/动态 - 第 ${page} 页`,
    description: "了解 GoodHR 产品动态、使用方法与招聘实践。文章持续更新，无需登录即可阅读。",
    path: page === 1 ? "/articles" : `/articles?page=${page}`,
    keywords: ["招聘文章", "GoodHR动态", "HR招聘经验"],
  });
}

/** ArticlesPage 输出可在关闭 JavaScript 后浏览的文章目录与上一页、下一页链接。 */
export default async function ArticlesPage({ searchParams }: ArticleListProps) {
  const page = positivePage((await searchParams).page);
  if (!page) notFound();
  const articles = await getPublicArticlePage(page);
  const totalPages = Math.max(1, Math.ceil(articles.total / ARTICLE_LIST_PAGE_SIZE));
  if (page > totalPages) notFound();
  return (
    <>
      <StructuredData data={{
        "@context": "https://schema.org",
        "@type": "CollectionPage",
        name: "GoodHR 文章/动态",
        url: absoluteURL(page === 1 ? "/articles" : `/articles?page=${page}`),
        mainEntity: {
          "@type": "ItemList",
          itemListElement: articles.items.map((article, index) => ({
            "@type": "ListItem",
            position: (page - 1) * ARTICLE_LIST_PAGE_SIZE + index + 1,
            name: article.title,
            url: absoluteURL(`/articles/${article.id}`),
          })),
        },
      }} />
      <MarketingShell eyebrow="文章/动态" title="招聘路上的实用分享与新消息" description="使用方法、招聘实践和 GoodHR 的最新变化，都在这里。">
        <section className={styles.section} aria-label="文章目录">
          <div className="public-container">
            {articles.items.length ? (
              <ul className={styles.list}>
                {articles.items.map((article) => (
                  <li key={article.id} className={styles.listItem}>
                    <Link href={`/articles/${article.id}`} className={styles.articleLink}>
                      <h2 className={styles.title}>{article.title}</h2>
                      <time className={styles.date} dateTime={article.published_at}>发布于 {formatArticleDate(article.published_at)}</time>
                      {article.excerpt ? <p className={styles.excerpt}>{article.excerpt}</p> : null}
                    </Link>
                  </li>
                ))}
              </ul>
            ) : <p className={styles.empty}>这里暂时空空的，新的文章正在路上。</p>}
            {totalPages > 1 ? (
              <nav className={styles.pagination} aria-label="文章分页">
                {page > 1 ? <Link href={page === 2 ? "/articles" : `/articles?page=${page - 1}`} rel="prev">上一页</Link> : null}
                <span>第 {page} / {totalPages} 页</span>
                {page < totalPages ? <Link href={`/articles?page=${page + 1}`} rel="next">下一页</Link> : null}
              </nav>
            ) : null}
          </div>
        </section>
      </MarketingShell>
    </>
  );
}
