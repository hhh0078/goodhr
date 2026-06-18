/** 本文件负责生成搜索引擎使用的网站地图。 */

import type { MetadataRoute } from "next";

/** sitemap 返回官网公开页面地址。 */
export default function sitemap(): MetadataRoute.Sitemap {
  const baseURL = (process.env.NEXT_PUBLIC_SITE_URL || "https://goodhr5.58it.cn").replace(/\/$/, "");
  return ["", "/features", "/pricing", "/videos", "/download", "/contact"].map((path, index) => ({ url: `${baseURL}${path || "/"}`, changeFrequency: index === 0 ? "daily" : "monthly", priority: index === 0 ? 1 : 0.8 }));
}
