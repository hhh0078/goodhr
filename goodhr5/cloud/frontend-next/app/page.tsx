/** 本文件负责展示 GoodHR 新版官网首页。 */
import "@/components/public-site.css";
import ArrowForwardRoundedIcon from "@ant-design/icons/lib/icons/ArrowRightOutlined";
import CheckCircleRoundedIcon from "@ant-design/icons/lib/icons/CheckCircleOutlined";
import DownloadRoundedIcon from "@ant-design/icons/lib/icons/DownloadOutlined";
import MarkChatReadRoundedIcon from "@ant-design/icons/lib/icons/CommentOutlined";
import SaveAltRoundedIcon from "@ant-design/icons/lib/icons/DownloadOutlined";
import Button from "antd/es/button";
import type { Metadata } from "next";
import Link from "next/link";
import RecruitmentPlatformOverview from "@/components/RecruitmentPlatformOverview";
import SiteFooter from "@/components/SiteFooter";
import SiteHeader from "@/components/SiteHeader";
import StructuredData from "@/components/StructuredData";
import WorkflowBand from "@/components/WorkflowBand";
import PublicIllustration from "@/components/PublicIllustration";
import PublicFeatureShowcase from "@/components/PublicFeatureShowcase";
import DownOutlined from "@ant-design/icons/lib/icons/DownOutlined";
import { BROWSER_EXTENSION_SUPPORTED_PLATFORM_TEXT } from "@/lib/browser-extension-platforms";
import { AUTO_REPLY_RELEASE_NOTICE, RECRUITMENT_PLATFORM_SUMMARY } from "@/lib/recruitment-platforms";
import {
  absoluteURL,
  createPageMetadata,
} from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "GoodHR招聘自动化工具 - AI筛选与自动打招呼，免费试用",
  description:
    "GoodHR帮HR和猎头自动筛选候选人、打招呼并管理简历与招聘进度。注册即送体验金额，先免费试用，满意再充值，最低1元起充。",
  path: "/",
  keywords: [
    "招聘软件免费试用",
    "自动打招呼免费试用",
    "猎头AI工具",
    "HR自动化招聘",
    "多招聘平台自动化",
    "BOSS直聘自动打招呼",
    "猎聘自动打招呼",
    "智联招聘自动筛选",
    "前程无忧简历导入",
    "结构化简历库",
    "猎头推荐流程",
    "企业HR招聘流程",
  ],
});

const productFlows = [
  {
    icon: SaveAltRoundedIcon,
    title: "结构化简历库",
    description:
      "支持 AI 批量解析、手动添加和浏览器插件导入；按手机号和履历线索查重，并保留头像、备注、来源和岗位记录。",
  },
  {
    icon: MarkChatReadRoundedIcon,
    title: "猎头推荐流程",
    description:
      "候选人可推荐到多个岗位，按岗位保存推荐状态、备注、操作人和时间，全部推进历史都能追踪。",
  },
  {
    icon: CheckCircleRoundedIcon,
    title: "企业 HR 招聘流程",
    description:
      "按候选人与岗位启动流程，用可配置模板推进面试安排、面评、审核、Offer 和入职确认，并汇总团队待办。",
  },
];

const automationScenes = [
  {
    title: "自动筛选与 AI 筛选",
    href: "/recruitment-automation",
    description:
      "按岗位要求处理候选人，通过 AI 结合经历完成简历评分、详情分析和筛选理由说明。",
  },
  {
    title: "自动打招呼与 AI 打招呼",
    href: "/recruitment-automation",
    description:
      "根据岗位模板和筛选结果自动执行打招呼，减少 HR 与猎头逐个打开候选人、重复点击的时间。",
  },
  {
    title: "招聘消息自动回复（开发中）",
    href: "/recruitment-auto-reply",
    description:
      "围绕岗位目标、候选人意向和面试沟通继续适配，当前仍在开发验证，尚未正式发布。",
  },
  {
    title: "简历下载与人才库整理",
    href: "/hr-tools",
    description:
      "将 BOSS直聘、猎聘、智联招聘、前程无忧等平台的候选人资料确认后导入简历库，保留来源、查重与跟进记录。",
  },
];

const faqs = [
  {
    question: "GoodHR 是什么？",
    answer:
      "GoodHR 是面向 HR、招聘团队和猎头顾问的多平台招聘工具，用于候选人筛选、AI 分析、自动打招呼、简历管理和招聘流程跟进。招聘消息自动回复仍在开发验证。",
  },
  {
    question: "GoodHR 可以免费试用吗？",
    answer:
      "可以。注册即送体验金额，先免费试用，满意再充值，无需购买会员。最低1元起充，不同模型价格不同；部分模型1元可筛选上千份简历，实际消耗以所选模型和简历内容为准。",
  },
  {
    question: "GoodHR 面向哪些招聘平台？",
    answer: RECRUITMENT_PLATFORM_SUMMARY,
  },
  {
    question: "前程无忧（51job）现在能用哪些功能？",
    answer: "新版本地程序已支持人才推荐、搜索人才页的筛选与打招呼，本次 Mac 安装包已更新，Windows 安装包随后更新。浏览器简历插件继续独立支持资料导入，招聘消息自动回复尚未发布。",
  },
  {
    question: "招聘消息自动回复现在可以使用吗？",
    answer: AUTO_REPLY_RELEASE_NOTICE,
  },
  {
    question: "招聘平台登录信息会上传吗？",
    answer:
      "不会。招聘平台 Cookie、浏览器资料、截图数据保存在用户本机，云端主要保存账号认证、岗位、岗位运行和团队配置。",
  },
  {
    question: "简历怎样进入 GoodHR 简历库？",
    answer:
      `可以通过 AI 批量解析、手动添加或浏览器插件导入。浏览器插件目前支持${BROWSER_EXTENSION_SUPPORTED_PLATFORM_TEXT}，会在候选人详情页自动识别资料，添加前需要确认有效手机号。`,
  },
  {
    question: "简历库、猎头推荐和企业 HR 招聘流程是什么关系？",
    answer:
      "简历库保存候选人资料和通用备注；猎头推荐按候选人与岗位记录推荐阶段和跟进历史；企业 HR 招聘使用独立流程推进面试、评价、审核和录用。三类记录互相关联，但不会覆盖彼此的历史。",
  },
];

/** HomePage 输出新版官网首页。 */
export default function HomePage() {
  return (
    <div className="public-marketing public-home-1">
      <StructuredData
        data={[
          {
            "@context": "https://schema.org",
            "@type": "SoftwareApplication",
            name: "GoodHR",
            applicationCategory: "BusinessApplication",
            operatingSystem: "Windows, macOS",
            url: absoluteURL("/"),
            downloadUrl: absoluteURL("/download"),
            description:
              "面向 HR 和猎头的招聘系统，支持招聘平台自动化、结构化简历库、猎头推荐流程和企业 HR 招聘流程。",
            offers: {
              "@type": "Offer",
              name: "注册免费试用",
              price: "0",
              priceCurrency: "CNY",
              description: "注册赠送体验金额，先免费试用；满意再充值，最低1元起充",
            },
            featureList: [...automationScenes, ...productFlows].map(
              (item) => item.title,
            ),
          },
          {
            "@context": "https://schema.org",
            "@type": "FAQPage",
            mainEntity: faqs.map((item) => ({
              "@type": "Question",
              name: item.question,
              acceptedAnswer: { "@type": "Answer", text: item.answer },
            })),
          },
        ]}
      />
      <SiteHeader />
      <main>
        <section className="public-home-5">
          <div className="public-container public-home-hero-layout">
            <div className="public-home-7">
              <div className="public-home-8">
                <CheckCircleRoundedIcon
                  className="public-home-9"
                  aria-hidden="true"
                />
                <p className="public-text public-home-10">注册即送体验金额 · 免费试用</p>
              </div>
              <h1 className="public-text public-home-11">
                多平台招聘自动化，
                <span className="public-home-12">把时间留给人</span>
              </h1>
              <p className="public-text public-home-14">
                HR
                和猎头的自动招聘助手。先免费试用，满意再充值，最低1元起充
              </p>
              <div className="public-home-15">
                <Button
                  href="/download"
                  type="primary"
                  size="large"
                  icon={<DownloadRoundedIcon aria-hidden="true" />}
                  className="public-home-16"
                >
                  下载试用
                </Button>
                <Button
                  href="/login"
                  type="default"
                  size="large"
                  icon={<ArrowForwardRoundedIcon aria-hidden="true" />}
                  className="public-home-18"
                  iconPlacement="end"
                >
                  进入控制台
                </Button>
              </div>
            </div>
            <PublicIllustration className="public-home-hero-art" priority />
          </div>
        </section>

        <WorkflowBand />

        <section className="public-home-23">
          <div className="public-container">
            <p className="public-text public-home-25">主流招聘平台持续适配</p>
            <h2 className="public-text public-home-26">
              一个 GoodHR，承接不同招聘平台的重复工作
            </h2>
            <p className="public-text public-home-27">
              {RECRUITMENT_PLATFORM_SUMMARY}
            </p>
            <RecruitmentPlatformOverview />
            <Link href="/recruitment-automation" className="public-topic-link">查看多平台筛选与自动打招呼流程 →</Link>
          </div>
        </section>

        <section className="public-home-30">
          <div className="public-container">
            <p className="public-text public-home-32">从寻找候选人到发起沟通</p>
            <h2 className="public-text public-home-33">
              招聘自动化，不只是批量点击
            </h2>
            <PublicFeatureShowcase scenes={automationScenes} />
          </div>
        </section>

        <section id="capabilities" className="public-home-39">
          <div className="public-container">
            <p className="public-text public-home-41">从人才入库到招聘推进</p>
            <h2 className="public-text public-home-42">
              简历、推荐、面试，一条流程接着走
            </h2>
            <p className="public-text public-home-27">
              候选人先进入简历库，猎头按岗位记录推荐进度，企业 HR
              再用独立流程推进面试、评价、审核和录用。每段记录都保留，不会互相覆盖。
            </p>
            <div className="public-home-43">
              {productFlows.map((item, index) => {
                const Icon = item.icon;
                return (
                  <div
                    key={item.title}
                    style={
                      {
                        "--public-home-44-0": (index === 0 ? 0 : 4) * 8 + "px",
                        "--public-home-44-1":
                          index < productFlows.length - 1
                            ? "1px solid"
                            : "none",
                        "--public-home-44-2":
                          index < productFlows.length - 1
                            ? "1px solid"
                            : "none",
                      } as import("react").CSSProperties
                    }
                    className="public-home-44"
                  >
                    <Icon className="public-home-45" aria-hidden="true" />
                    <h3 className="public-text public-home-46">{item.title}</h3>
                    <p className="public-text public-home-47">
                      {item.description}
                    </p>
                  </div>
                );
              })}
            </div>
          </div>
        </section>

        <section className="public-home-48">
          <div className="public-container public-home-faq-layout">
            <div className="public-home-faq-intro">
            <p className="public-text public-home-50">常见问题</p>
            <h2 className="public-text public-home-51">
              HR 和猎头最关心的问题
            </h2>
            <PublicIllustration kind="followup" className="public-home-faq-art" />
            </div>
            <div className="public-home-52">
              {faqs.map((item, index) => (
                <details key={item.question} className="public-home-53" name="goodhr-home-faq" open={index === 0}>
                  <summary>
                  <h3 className="public-text public-home-54">
                    {item.question}
                    <DownOutlined aria-hidden="true" />
                  </h3>
                  </summary>
                  <p className="public-text public-home-55">{item.answer}</p>
                </details>
              ))}
            </div>
          </div>
        </section>

        <section className="public-home-56">
          <div className="public-container">
            <div className="public-home-58">
              <div>
                <h2 className="public-text public-home-60">
                  从今天开始，少做一点重复工作
                </h2>
                <p className="public-text public-home-61">
                  注册即送体验金额，先免费试用，使用满意了再充值。
                </p>
              </div>
              <PublicIllustration className="public-home-closing-art" />
              <Button
                href="/download"
                type="primary"
                size="large"
                icon={<DownloadRoundedIcon aria-hidden="true" />}
                className="public-home-62"
              >
                下载 GoodHR
              </Button>
            </div>
          </div>
        </section>
      </main>
      <SiteFooter />
    </div>
  );
}
