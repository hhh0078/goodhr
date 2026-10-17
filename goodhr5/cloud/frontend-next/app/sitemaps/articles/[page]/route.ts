/** 本文件负责每一千篇公开文章的动态 XML 分片，避免搜索引擎单文件链接数量上限。 */

import { ARTICLE_CATALOG_PAGE_SIZE, articleSitemapResponse, positivePage, sitemapURLSetXML, unavailableSitemap } from "@/lib/article-public-utils";
import { getPublicArticleCatalogPage } from "@/lib/public-articles";
import { absoluteURL } from "@/lib/seo";

export const dynamic = "force-dynamic";

/** GET 按目录页实时输出已发布文章，非法或已消失的分片返回 404。 */
export async function GET(_request: Request, { params }: { params: Promise<{ page: string }> }) {
  const value = (await params).page;
  const page = value.endsWith(".xml") ? positivePage(value.slice(0, -4)) : null;
  if (!page) return new Response("站点地图不存在", { status: 404 });
  try {
    const catalog = await getPublicArticleCatalogPage(page);
    if ((page - 1) * ARTICLE_CATALOG_PAGE_SIZE >= catalog.total) return new Response("站点地图不存在", { status: 404 });
    return articleSitemapResponse(sitemapURLSetXML(catalog.items.map((article) => ({ url: absoluteURL(`/articles/${article.id}`), updatedAt: article.updated_at || article.published_at }))));
  } catch {
    return unavailableSitemap();
  }
}
