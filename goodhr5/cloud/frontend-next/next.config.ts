/** 本文件负责配置 GoodHR 新版 Next.js 前端的构建行为。 */
import type { NextConfig } from "next";
import { PUBLIC_SITE_PAGES } from "./lib/public-site-pages";

const staticExport = process.env.GOODHR_STATIC_EXPORT === "1";

const nextConfig: NextConfig = {
  output: staticExport ? "export" : "standalone",
  poweredByHeader: false,
  images: staticExport ? { unoptimized: true } : undefined,
  ...(staticExport
    ? {}
    : {
        /** redirects 修复历史官网入口，并仅统一公开页面的域名和 HTTPS，保留旧客户端使用的后台与 API。 */
        async redirects() {
          const legacyPages = ["features", "pricing", "videos", "download", "contact"].map((name) => ({ source: `/${name}.html`, destination: `/${name}`, permanent: true }));
          const publicPaths = [...new Set([...PUBLIC_SITE_PAGES.map((page) => page.path), "/articles/:path*", "/sitemaps/:path*", "/llms/:path*", "/robots.txt", "/sitemap.xml", "/llms.txt"])];
          return [
            ...legacyPages,
            { source: "/installation.html", destination: "/download", permanent: true },
            ...publicPaths.map((source) => ({
              source,
              has: [{ type: "host" as const, value: "goodhr5\\.58it\\.cn" }],
              destination: `https://goodhr.58it.cn${source}`,
              permanent: true,
            })),
            ...publicPaths.map((source) => ({
              source,
              has: [
                { type: "host" as const, value: "goodhr\\.58it\\.cn" },
                { type: "header" as const, key: "x-forwarded-proto", value: "http" },
              ],
              destination: `https://goodhr.58it.cn${source}`,
              permanent: true,
            })),
          ];
        },
      }),
};

export default nextConfig;
