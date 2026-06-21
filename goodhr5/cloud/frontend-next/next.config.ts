/** 本文件负责配置 GoodHR 新版 Next.js 前端的构建行为。 */
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  async redirects() {
    return ["features", "pricing", "videos", "download", "contact"].map((name) => ({ source: `/${name}.html`, destination: `/${name}`, permanent: true }));
  },
};

export default nextConfig;
