/** 本文件动态列出官网页面和公开文章，超过单次目录容量后自动改用完整分片索引。 */

import { ARTICLE_CATALOG_PAGE_SIZE, articleSitemapResponse, sitemapIndexXML, sitemapURLSetXML, unavailableSitemap } from "@/lib/article-public-utils";
import { getPublicArticleCatalogPage } from "@/lib/public-articles";
import { absoluteURL, SITE_URL } from "@/lib/seo";
import { PUBLIC_SITE_PAGES } from "@/lib/public-site-pages";

export const dynamic = "force-dynamic";

/** GET 在普通规模下直接列全站链接，文章较多时保留原有合法分片结构，避免截断。 */
export async function GET() {
  try {
    const catalog = await getPublicArticleCatalogPage();
    if (catalog.total <= ARTICLE_CATALOG_PAGE_SIZE) {
      return articleSitemapResponse(sitemapURLSetXML([
        ...PUBLIC_SITE_PAGES.map(({ path }) => ({ url: absoluteURL(path) })),
        ...catalog.items.map((article) => ({ url: absoluteURL(`/articles/${article.id}`), updatedAt: article.updated_at || article.published_at })),
      ]));
    }
    return articleSitemapResponse(sitemapIndexXML(SITE_URL, catalog.total));
  } catch {
    return unavailableSitemap();
  }
}
