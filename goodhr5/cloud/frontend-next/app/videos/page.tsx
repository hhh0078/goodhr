/** 本文件负责官网视频教程页及其 SEO 信息。 */

import PlayCircleOutlineRoundedIcon from "@mui/icons-material/PlayCircleOutlineRounded";
import { Box, Container, Typography } from "@mui/material";
import type { Metadata } from "next";
import MarketingShell from "@/components/MarketingShell";
import StructuredData from "@/components/StructuredData";
import { absoluteURL, createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({ title: "GoodHR视频教程 - 招聘平台自动化、AI筛选与自动打招呼", description: "观看 GoodHR 安装、AI接口配置、岗位模板、自动筛选候选人、自动打招呼和招聘任务使用教程。", path: "/videos", keywords: ["BOSS自动打招呼教程", "AI筛选简历教程", "招聘自动化教程", "GoodHR安装教程"] });

const videos = [
  { title: "安装本地程序", description: "下载并安装 GoodHR，确认本地控制台和浏览器组件可以正常使用。", src: "https://player.bilibili.com/player.html?bvid=BV1FUV26PEuv&page=1" },
  { title: "配置 AI 接口", description: "注册 AI 平台账号、获取 API 密钥并在个人配置中完成验证。", src: "https://player.bilibili.com/player.html?bvid=BV18MVm6JEve&page=1" },
];

/** VideosPage 展示安装和使用视频教程。 */
export default function VideosPage() {
  return <><StructuredData data={{ "@context": "https://schema.org", "@type": "ItemList", name: "GoodHR 招聘自动化视频教程", url: absoluteURL("/videos"), itemListElement: videos.map((video, index) => ({ "@type": "ListItem", position: index + 1, name: video.title, description: video.description })) }} /><MarketingShell eyebrow="视频教程" title="从安装到开始第一条招聘任务" description="按照步骤完成本地程序、招聘平台账号、岗位模板、AI筛选和自动打招呼任务配置。视频来自哔哩哔哩播放器。">
    <Box component="section" sx={{ pb: { xs: 8, md: 12 } }}><Container maxWidth="lg"><Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(2, 1fr)" }, gap: 4 }}>
      {videos.map((video) => <Box component="article" key={video.title}><Box sx={{ overflow: "hidden", aspectRatio: "16 / 9", border: "1px solid", borderColor: "divider", borderRadius: "8px", bgcolor: "#17211c" }}><Box component="iframe" src={video.src} title={video.title} loading="lazy" allowFullScreen sx={{ width: "100%", height: "100%", border: 0 }} /></Box><Typography component="h2" sx={{ mt: 2.5, fontSize: 22, fontWeight: 760 }}><PlayCircleOutlineRoundedIcon color="primary" sx={{ mr: 1, verticalAlign: "middle" }} />{video.title}</Typography><Typography sx={{ mt: 1, color: "text.secondary", lineHeight: 1.8 }}>{video.description}</Typography></Box>)}
      <Box component="article" sx={{ py: 4, borderTop: "1px solid", borderColor: "divider" }}><Typography component="h2" sx={{ fontSize: 22, fontWeight: 760 }}>任务参数和日志说明</Typography><Typography sx={{ mt: 1, color: "text.secondary", lineHeight: 1.8 }}>后续教程将补充筛选模式、任务上限、延迟参数和常见问题排查。</Typography></Box>
    </Box></Container></Box>
  </MarketingShell></>;
}
