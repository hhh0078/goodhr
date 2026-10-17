/** 本文件负责动态生成官网站点地图索引，文章发布与撤回无需重新构建。 */

import { articleSitemapResponse, sitemapIndexXML, unavailableSitemap } from "@/lib/article-public-utils";
import { getPublicArticleCatalogPage } from "@/lib/public-articles";
import { SITE_URL } from "@/lib/seo";

export const dynamic = "force-dynamic";

/** GET 读取当前公开文章总数，生成固定页面与文章分片的 XML 索引。 */
export async function GET() {
  try {
    const catalog = await getPublicArticleCatalogPage();
    return articleSitemapResponse(sitemapIndexXML(SITE_URL, catalog.total));
  } catch {
    return unavailableSitemap();
  }
}
