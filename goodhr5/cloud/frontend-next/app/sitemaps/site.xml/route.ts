/** 本文件负责官网固定公开页面的站点地图，独立于文章数量保持可抓取。 */

import { articleSitemapResponse, sitemapURLSetXML } from "@/lib/article-public-utils";
import { absoluteURL } from "@/lib/seo";

export const dynamic = "force-dynamic";

/** GET 输出无需登录的官网页面与文章目录入口。 */
export function GET() {
  return articleSitemapResponse(sitemapURLSetXML(["/", "/features", "/pricing", "/videos", "/download", "/contact", "/articles"].map((path) => ({ url: absoluteURL(path) }))));
}
