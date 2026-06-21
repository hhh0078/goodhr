/** 本文件负责官网功能介绍页及其 SEO 内容。 */

import AutoAwesomeRoundedIcon from "@mui/icons-material/AutoAwesomeRounded";
import ChatRoundedIcon from "@mui/icons-material/ChatRounded";
import DownloadRoundedIcon from "@mui/icons-material/DownloadRounded";
import ManageAccountsRoundedIcon from "@mui/icons-material/ManageAccountsRounded";
import PsychologyRoundedIcon from "@mui/icons-material/PsychologyRounded";
import QueryStatsRoundedIcon from "@mui/icons-material/QueryStatsRounded";
import SmartToyRoundedIcon from "@mui/icons-material/SmartToyRounded";
import TuneRoundedIcon from "@mui/icons-material/TuneRounded";
import { Box, Container, Typography } from "@mui/material";
import type { Metadata } from "next";
import MarketingShell from "@/components/MarketingShell";
import { createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({ title: "招聘自动化功能 - AI筛选、自动打招呼、自动回复与简历下载", description: "了解 GoodHR 在 BOSS直聘、猎聘、智联招聘等平台场景中的关键词筛选、AI筛选、自动打招呼、AI自动回复、候选人跟进和简历下载管理能力。", path: "/features", keywords: ["招聘自动化功能", "AI自动回复候选人", "跨平台招聘工具", "自动下载招聘简历"] });

const features = [
  { icon: PsychologyRoundedIcon, title: "AI 候选人筛选", text: "结合岗位要求和候选人信息生成匹配分与判断理由，减少重复查看。" },
  { icon: TuneRoundedIcon, title: "关键词规则筛选", text: "支持关键词、排除词和多种匹配关系，规则明确的岗位可以永久免费使用。" },
  { icon: AutoAwesomeRoundedIcon, title: "候选人详情分析", text: "支持 AI、OCR 和页面结构三种详情读取方式，并按岗位模板继续判断。" },
  { icon: ManageAccountsRoundedIcon, title: "平台账号管理", text: "招聘平台登录状态和浏览器资料保留在本地，云端仅保存账号名称与业务信息。" },
  { icon: SmartToyRoundedIcon, title: "本地自动执行", text: "页面滚动、截图、OCR、提示音和浏览器操作由本地程序完成。" },
  { icon: QueryStatsRoundedIcon, title: "任务记录与统计", text: "任务状态、打招呼数量和关键日志清楚可见，出现问题更容易定位。" },
  { icon: ChatRoundedIcon, title: "自动回复与候选人跟进", text: "根据岗位目标继续沟通，支持招聘消息自动回复、AI 自动回复、意向确认和面试邀约场景。" },
  { icon: DownloadRoundedIcon, title: "简历下载与人才库", text: "整理招聘平台候选人详情、评分和沟通结果，便于管理 BOSS、猎聘、智联等平台简历。" },
];

/** FeaturesPage 展示 GoodHR 的主要产品能力。 */
export default function FeaturesPage() {
  return <MarketingShell eyebrow="功能介绍" title="围绕真实招聘流程，减少每天重复的动作" description="GoodHR 把候选人读取、关键词筛选、AI筛选、详情分析、自动打招呼、招聘消息回复和简历整理连续完成。">
    <Box component="section" sx={{ pb: { xs: 8, md: 12 } }}><Container maxWidth="lg">
      <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "repeat(3, 1fr)" }, borderTop: "1px solid", borderColor: "divider" }}>
        {features.map((item, index) => { const Icon = item.icon; return <Box key={item.title} sx={{ py: 4, px: { md: 3 }, borderRight: { md: index % 3 !== 2 ? "1px solid" : "none" }, borderBottom: "1px solid", borderColor: "divider" }}>
          <Icon color="primary" /><Typography component="h2" sx={{ mt: 2, fontSize: 21, fontWeight: 760 }}>{item.title}</Typography><Typography sx={{ mt: 1.25, color: "text.secondary", lineHeight: 1.8 }}>{item.text}</Typography>
        </Box>; })}
      </Box>
    </Container></Box>
  </MarketingShell>;
}
