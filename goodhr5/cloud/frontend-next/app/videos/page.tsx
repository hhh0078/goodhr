/** 本文件负责官网视频教程页外壳、SEO 信息和视频配置入口。 */

import type { Metadata } from "next";
import MarketingShell from "@/components/MarketingShell";
import StructuredData from "@/components/StructuredData";
import { getGuideVideos } from "@/lib/public-data";
import VideoGuideList from "./VideoGuideList";
import { absoluteURL, createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "GoodHR视频教程 - 招聘平台自动化、AI筛选与自动打招呼",
  description:
    "观看 GoodHR 注册安装、创建招聘岗位、设置筛选条件、AI筛选候选人和自动打招呼视频教程。",
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
        title="从安装到跑通第一次招聘任务"
        description="注册、安装本地程序、创建岗位，再登录招聘平台开始筛选。跟着视频，一步步来。"
      >
        <section className="public-videos-3">
          <div className="public-container">
            <VideoGuideList videos={videos} />
          </div>
        </section>
      </MarketingShell>
    </>
  );
}
