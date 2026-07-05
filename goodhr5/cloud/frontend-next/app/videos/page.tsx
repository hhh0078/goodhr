/** 本文件负责官网视频教程页外壳、SEO 信息和视频配置入口。 */

import { Box, Container } from "@mui/material";
import type { Metadata } from "next";
import MarketingShell from "@/components/MarketingShell";
import StructuredData from "@/components/StructuredData";
import { getGuideVideos } from "@/lib/public-data";
import VideoGuideList from "./VideoGuideList";
import { absoluteURL, createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "GoodHR视频教程 - 招聘平台自动化、AI筛选与自动打招呼",
  description:
    "观看 GoodHR 安装、AI接口配置、岗位模板、自动筛选候选人、自动打招呼和招聘任务使用教程。",
  path: "/videos",
  keywords: [
    "BOSS自动打招呼教程",
    "AI筛选简历教程",
    "招聘自动化教程",
    "GoodHR安装教程",
  ],
});

/** VideosPage 展示安装和使用视频教程。 */
export default async function VideosPage() {
  const videos = await getGuideVideos();

  return (
    <>
      <StructuredData
        data={{
          "@context": "https://schema.org",
          "@type": "ItemList",
          name: "GoodHR 招聘自动化视频教程",
          url: absoluteURL("/videos"),
        }}
      />
      <MarketingShell
        eyebrow="视频教程"
        title="从安装到开始第一条招聘任务"
        description="按照步骤完成本地程序、招聘平台账号、岗位模板、AI筛选和自动打招呼任务配置。"
      >
        <Box component="section" sx={{ pb: { xs: 8, md: 12 } }}>
          <Container maxWidth="lg">
            <VideoGuideList videos={videos} />
          </Container>
        </Box>
      </MarketingShell>
    </>
  );
}
