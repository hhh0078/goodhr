/** 本文件负责官网功能介绍页及其 SEO 内容。 */

import AutoAwesomeRoundedIcon from "@ant-design/icons/lib/icons/ThunderboltOutlined";
import ChatRoundedIcon from "@ant-design/icons/lib/icons/CommentOutlined";
import DownloadRoundedIcon from "@ant-design/icons/lib/icons/DownloadOutlined";
import ManageAccountsRoundedIcon from "@ant-design/icons/lib/icons/TeamOutlined";
import PsychologyRoundedIcon from "@ant-design/icons/lib/icons/BulbOutlined";
import QueryStatsRoundedIcon from "@ant-design/icons/lib/icons/LineChartOutlined";
import SmartToyRoundedIcon from "@ant-design/icons/lib/icons/RobotOutlined";
import TuneRoundedIcon from "@ant-design/icons/lib/icons/SlidersOutlined";

import type { Metadata } from "next";
import Link from "next/link";
import MarketingShell from "@/components/MarketingShell";
import RecruitmentPlatformOverview from "@/components/RecruitmentPlatformOverview";
import { BROWSER_EXTENSION_SUPPORTED_PLATFORM_TEXT } from "@/lib/browser-extension-platforms";
import { AUTO_REPLY_RELEASE_NOTICE } from "@/lib/recruitment-platforms";
import { createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "多平台招聘自动化功能 - AI筛选、自动打招呼与HR猎头工具",
  description:
    "了解 BOSS直聘、猎聘、智联招聘的候选人筛选与打招呼、前程无忧51job适配进度，以及简历导入、查重、猎头推荐和企业HR招聘流程。",
  path: "/features",
  keywords: [
    "招聘自动化功能",
    "AI自动回复候选人",
    "跨平台招聘工具",
    "自动下载招聘简历",
    "结构化简历库",
    "猎头推荐流程",
    "企业HR招聘流程",
  ],
});

const features = [
  {
    icon: PsychologyRoundedIcon,
    title: "AI 候选人筛选",
    text: "结合岗位要求和候选人信息生成匹配分与判断理由，减少重复查看。",
  },
  {
    icon: TuneRoundedIcon,
    title: "关键词规则筛选",
    text: "支持关键词、排除词和多种匹配关系，规则明确的岗位可以永久免费使用。",
  },
  {
    icon: AutoAwesomeRoundedIcon,
    title: "候选人详情分析",
    text: "支持 AI、OCR 和页面结构三种详情读取方式，并按岗位模板继续判断。",
  },
  {
    icon: ManageAccountsRoundedIcon,
    title: "平台账号管理",
    text: "招聘平台登录状态和浏览器资料保留在本地，云端仅保存账号名称与业务信息。",
  },
  {
    icon: SmartToyRoundedIcon,
    title: "本地自动执行",
    text: "页面滚动、截图、OCR、提示音和浏览器操作由本地程序完成。",
  },
  {
    icon: QueryStatsRoundedIcon,
    title: "岗位运行记录与统计",
    text: "岗位运行状态、打招呼数量和关键日志清楚可见，出现问题更容易定位。",
  },
  {
    icon: ChatRoundedIcon,
    title: "招聘消息自动回复（开发中）",
    text: AUTO_REPLY_RELEASE_NOTICE,
  },
  {
    icon: DownloadRoundedIcon,
    title: "结构化简历库",
    text: `通过 AI 批量解析、手动添加或浏览器插件导入候选人。插件目前支持${BROWSER_EXTENSION_SUPPORTED_PLATFORM_TEXT}；入库后按手机号和履历线索查重，并保留备注与来源。`,
  },
  {
    icon: QueryStatsRoundedIcon,
    title: "猎头推荐流程",
    text: "一位候选人可以推荐到多个岗位，每个岗位独立记录推荐阶段、备注、操作人、时间和推进历史。",
  },
  {
    icon: ManageAccountsRoundedIcon,
    title: "企业 HR 招聘流程",
    text: "按候选人与岗位启动独立流程，用团队模板推进面试安排、面评、审核、Offer 和入职确认。",
  },
];

/** FeaturesPage 展示 GoodHR 的主要产品能力。 */
export default function FeaturesPage() {
  return (
    <MarketingShell
      eyebrow="功能介绍"
      title="围绕真实招聘流程，减少每天重复的动作"
      description="GoodHR 把候选人读取、筛选和沟通接到结构化简历库，再通过猎头推荐和企业 HR 招聘流程继续推进，每段历史独立保留。"
    >
      <section className="public-features-2">
        <div className="public-container">
          <div className="public-features-4">
            {features.map((item, index) => {
              const Icon = item.icon;
              return (
                <div
                  key={item.title}
                  style={
                    {
                      "--public-features-5-0":
                        index % 3 !== 2 ? "1px solid" : "none",
                    } as import("react").CSSProperties
                  }
                  className="public-features-5"
                >
                  <Icon className="public-features-6" aria-hidden="true" />
                  <h2 className="public-text public-features-7">
                    {item.title}
                  </h2>
                  <p className="public-text public-features-8">{item.text}</p>
                </div>
              );
            })}
          </div>
          <nav className="public-topic-navigation" aria-label="招聘工具详细说明">
            <Link href="/recruitment-automation">多平台筛选与自动打招呼</Link>
            <Link href="/recruitment-auto-reply">招聘自动回复场景与进度</Link>
            <Link href="/hr-tools">HR与猎头简历管理流程</Link>
          </nav>
          <h2 className="public-text public-platform-title">按平台查看当前能力</h2>
          <RecruitmentPlatformOverview />
        </div>
      </section>
    </MarketingShell>
  );
}
