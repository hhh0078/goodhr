/** 本文件负责生成搜索引擎抓取规则。 */

import type { MetadataRoute } from "next";

/** robots 允许抓取官网并禁止抓取后台页面。 */
export default function robots(): MetadataRoute.Robots {
  const baseURL = (process.env.NEXT_PUBLIC_SITE_URL || "https://goodhr5.58it.cn").replace(/\/$/, "");
  return { rules: { userAgent: "*", allow: "/", disallow: ["/admin/", "/login"] }, sitemap: `${baseURL}/sitemap.xml` };
}
