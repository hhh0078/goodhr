/** 本文件负责展示 GoodHR 新版官网首页。 */
import "@/components/public-site.css";
import ArrowForwardRoundedIcon from "@ant-design/icons/lib/icons/ArrowRightOutlined";
import CheckCircleRoundedIcon from "@ant-design/icons/lib/icons/CheckCircleOutlined";
import DownloadRoundedIcon from "@ant-design/icons/lib/icons/DownloadOutlined";
import GitHubIcon from "@ant-design/icons/lib/icons/GithubOutlined";
import MarkChatReadRoundedIcon from "@ant-design/icons/lib/icons/CommentOutlined";
import PersonSearchRoundedIcon from "@ant-design/icons/lib/icons/UserOutlined";
import SaveAltRoundedIcon from "@ant-design/icons/lib/icons/DownloadOutlined";
import SpeedRoundedIcon from "@ant-design/icons/lib/icons/DashboardOutlined";
import Button from "antd/es/button";
import type { Metadata } from "next";
import SiteFooter from "@/components/SiteFooter";
import SiteHeader from "@/components/SiteHeader";
import StructuredData from "@/components/StructuredData";
import WorkflowBand from "@/components/WorkflowBand";
import {
  absoluteURL,
  createPageMetadata,
  GITHUB_REPOSITORY_URL,
  RECRUITMENT_PLATFORMS,
} from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "GoodHR AI招聘助手 - 简历库、猎头推荐与HR招聘流程",
  description:
    "GoodHR 是面向 HR 和猎头的完全开源招聘系统，支持招聘平台自动筛选与沟通、结构化简历库、候选人查重、猎头岗位推荐记录，以及企业 HR 面试、评价、审核和录用流程。",
  path: "/",
  keywords: [
    "免费招聘软件",
    "免费自动打招呼",
    "猎头AI工具",
    "HR自动化招聘",
    "多招聘平台自动化",
    "开源招聘系统",
    "开源AI招聘助手",
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
    icon: PersonSearchRoundedIcon,
    title: "自动筛选与 AI 筛选",
    description:
      "按关键词、排除词和岗位要求处理候选人，也可以通过 AI 完成简历评分、详情分析和筛选理由说明。",
  },
  {
    icon: SpeedRoundedIcon,
    title: "自动打招呼与 AI 打招呼",
    description:
      "根据岗位模板和筛选结果自动执行打招呼，减少 HR 与猎头逐个打开候选人、重复点击的时间。",
  },
  {
    icon: MarkChatReadRoundedIcon,
    title: "招聘消息自动回复",
    description:
      "围绕岗位要求继续沟通，支持 AI 自动回复、候选人意向确认、关键信息收集和面试邀约场景。",
  },
  {
    icon: SaveAltRoundedIcon,
    title: "简历下载与人才库整理",
    description:
      "整理候选人详情、评分与沟通结果，帮助管理 BOSS 简历下载、猎聘简历下载和跨平台候选人资料。",
  },
];

const faqs = [
  {
    question: "GoodHR 是什么？",
    answer:
      "GoodHR 是面向 HR、招聘团队和猎头顾问的招聘自动化工具，用于候选人筛选、AI 分析、自动打招呼、招聘消息回复和简历管理。",
  },
  {
    question: "GoodHR 可以免费使用吗？",
    answer:
      "可以。关键词筛选、排除词筛选、基础招聘岗位运行和自动打招呼等基础流程可以免费使用，AI 筛选和 AI 详情分析按会员方案使用。",
  },
  {
    question: "GoodHR 面向哪些招聘平台？",
    answer: `GoodHR 面向 ${RECRUITMENT_PLATFORMS.join("、")} 等招聘平台持续适配。`,
  },
  {
    question: "招聘平台登录信息会上传吗？",
    answer:
      "不会。招聘平台 Cookie、浏览器资料、截图和 OCR 数据保存在用户本机，云端主要保存账号认证、岗位、岗位运行和团队配置。",
  },
  {
    question: "简历怎样进入 GoodHR 简历库？",
    answer:
      "可以通过 AI 批量解析、手动添加或浏览器插件导入。浏览器插件会在支持的招聘平台候选人详情页自动识别资料，添加前需要确认有效手机号。",
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
            codeRepository: GITHUB_REPOSITORY_URL,
            description:
              "面向 HR 和猎头的完全开源招聘系统，支持招聘平台自动化、结构化简历库、猎头推荐流程和企业 HR 招聘流程。",
            offers: {
              "@type": "Offer",
              price: "0",
              priceCurrency: "CNY",
              description: "关键词筛选与基础招聘流程可免费使用",
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
          <div className="public-container">
            <div className="public-home-7">
              <div className="public-home-8">
                <CheckCircleRoundedIcon
                  className="public-home-9"
                  aria-hidden="true"
                />
                <p className="public-text public-home-10">关键词筛选永久免费</p>
              </div>
              <h1 className="public-text public-home-11">
                把重复招聘交给 GoodHR，
                <span className="public-home-12">把时间留给人</span>
              </h1>
              <p className="public-text public-home-13">
                自动读取候选人，结合岗位模板完成筛选、分析、打招呼和后续跟进。流程持续运转，判断始终清楚可见。
              </p>

              <p className="public-text public-home-14">
                HR
                和猎头的免费自动招聘助手，完全开源，代码公开。不是摸鱼神器，而是提效工具
              </p>
              <div className="public-home-15">
                <Button
                  href="/download"
                  type="primary"
                  size="large"
                  icon={<DownloadRoundedIcon aria-hidden="true" />}
                  className="public-home-16"
                >
                  免费下载
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
                <Button
                  href={GITHUB_REPOSITORY_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                  type="default"
                  size="large"
                  icon={<GitHubIcon aria-hidden="true" />}
                  className="public-home-20"
                >
                  开源地址
                </Button>
              </div>
            </div>
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
              围绕不同招聘平台的候选人列表、详情页、沟通和简历场景持续扩展，统一使用岗位模板、筛选规则和本地浏览器资料。
            </p>
            <div className="public-home-28">
              {RECRUITMENT_PLATFORMS.map((platform) => (
                <span key={platform} className="public-text public-home-29">
                  {platform}
                </span>
              ))}
            </div>
          </div>
        </section>

        <section className="public-home-30">
          <div className="public-container">
            <p className="public-text public-home-32">从寻找候选人到发起沟通</p>
            <h2 className="public-text public-home-33">
              招聘自动化，不只是批量点击
            </h2>
            <div className="public-home-34">
              {automationScenes.map((item, index) => {
                const Icon = item.icon;
                return (
                  <div
                    key={item.title}
                    style={
                      {
                        "--public-home-35-0":
                          (index % 2 === 0 ? 4 : 0) * 8 + "px",
                        "--public-home-35-1":
                          (index % 2 === 1 ? 4 : 0) * 8 + "px",
                        "--public-home-35-2":
                          index % 2 === 0 ? "1px solid" : "none",
                      } as import("react").CSSProperties
                    }
                    className="public-home-35"
                  >
                    <Icon className="public-home-36" aria-hidden="true" />
                    <h3 className="public-text public-home-37">{item.title}</h3>
                    <p className="public-text public-home-38">
                      {item.description}
                    </p>
                  </div>
                );
              })}
            </div>
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
          <div className="public-container">
            <p className="public-text public-home-50">常见问题</p>
            <h2 className="public-text public-home-51">
              HR 和猎头最关心的问题
            </h2>
            <div className="public-home-52">
              {faqs.map((item) => (
                <article key={item.question} className="public-home-53">
                  <h3 className="public-text public-home-54">
                    {item.question}
                  </h3>
                  <p className="public-text public-home-55">{item.answer}</p>
                </article>
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
                  关键词筛选和基础岗位运行可以长期免费使用。
                </p>
              </div>
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
