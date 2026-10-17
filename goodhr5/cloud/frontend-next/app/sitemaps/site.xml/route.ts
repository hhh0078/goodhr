/** 本文件负责官网固定公开页面的站点地图，独立于文章数量保持可抓取。 */

import { articleSitemapResponse, sitemapURLSetXML } from "@/lib/article-public-utils";
import { absoluteURL } from "@/lib/seo";
import { PUBLIC_SITE_PAGES } from "@/lib/public-site-pages";

export const dynamic = "force-dynamic";

/** GET 输出无需登录的官网页面与文章目录入口。 */
export function GET() {
  return articleSitemapResponse(sitemapURLSetXML(PUBLIC_SITE_PAGES.map(({ path }) => ({ url: absoluteURL(path) }))));
}
