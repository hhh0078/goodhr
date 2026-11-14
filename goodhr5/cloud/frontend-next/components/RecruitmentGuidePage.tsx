/** 本文件复用官网布局展示招聘专题，并从同一份问答内容生成可见正文与 JSON-LD。 */

import Link from "next/link";
import MarketingShell from "@/components/MarketingShell";
import RecruitmentPlatformOverview from "@/components/RecruitmentPlatformOverview";
import StructuredData from "@/components/StructuredData";
import { AUTO_REPLY_RELEASE_NOTICE, RECRUITMENT_PLATFORM_SUMMARY } from "@/lib/recruitment-platforms";
import type { RecruitmentGuide } from "@/lib/recruitment-guides";
import { absoluteURL } from "@/lib/seo";
import { PLATFORM_GUIDES } from "@/lib/platform-guides";
import "./recruitment-guide.css";
import DownOutlined from "@ant-design/icons/lib/icons/DownOutlined";

/** RecruitmentGuidePage 输出专题正文、平台能力、费用边界、常见问题和相关入口。 */
export default function RecruitmentGuidePage({ guide }: { guide: RecruitmentGuide }) {
  return (
    <MarketingShell eyebrow={guide.eyebrow} title={guide.heading} description={guide.introduction} platformID={guide.platformId}>
      <StructuredData data={[
        {
          "@context": "https://schema.org",
          "@type": "WebPage",
          name: guide.title,
          description: guide.description,
          url: absoluteURL(guide.path),
          inLanguage: "zh-CN",
        },
        {
          "@context": "https://schema.org",
          "@type": "BreadcrumbList",
          itemListElement: [
            { "@type": "ListItem", position: 1, name: "首页", item: absoluteURL("/") },
            ...(guide.platformId ? [{ "@type": "ListItem", position: 2, name: "多平台招聘自动化", item: absoluteURL("/recruitment-automation") }] : []),
            { "@type": "ListItem", position: guide.platformId ? 3 : 2, name: guide.eyebrow, item: absoluteURL(guide.path) },
          ],
        },
        {
          "@context": "https://schema.org",
          "@type": "FAQPage",
          mainEntity: guide.faqs.map((faq) => ({
            "@type": "Question",
            name: faq.question,
            acceptedAnswer: { "@type": "Answer", text: faq.answer },
          })),
        },
      ]} />

      <div className={`public-container recruitment-guide${guide.platformId ? " platform-guide" : ""}`}>
        <nav className="recruitment-guide-breadcrumb" aria-label="当前位置">
          <Link href="/">首页</Link><span aria-hidden="true">/</span>
          {guide.platformId ? <><Link href="/recruitment-automation">多平台招聘</Link><span aria-hidden="true">/</span></> : null}
          <span aria-current="page">{guide.eyebrow}</span>
        </nav>
        <nav className="recruitment-guide-actions" aria-label="开始使用与查看费用">
          <Link className="recruitment-guide-button" href="/download">下载 GoodHR</Link>
          <Link className="recruitment-guide-link" href="/pricing">先看试用与费用 <span aria-hidden="true">→</span></Link>
        </nav>
        {guide.path === "/recruitment-auto-reply" ? <p className="recruitment-guide-release-note">{AUTO_REPLY_RELEASE_NOTICE}</p> : null}

        <section className="recruitment-guide-section" aria-labelledby="guide-scenes">
          <h2 id="guide-scenes">{guide.sceneTitle}</h2>
          <div className="recruitment-guide-columns">
            {guide.scenes.map((scene) => <div key={scene.title}><h3>{scene.title}</h3><p>{scene.text}</p></div>)}
          </div>
        </section>

        <section className="recruitment-guide-section" aria-labelledby="guide-steps">
          <h2 id="guide-steps">{guide.stepsTitle}</h2>
          <ol className="recruitment-guide-steps">
            {guide.steps.map((step) => <li key={step.title}><h3>{step.title}</h3><p>{step.text}</p></li>)}
          </ol>
        </section>

        {guide.platformId ? (
          <section className="recruitment-guide-section" aria-labelledby="guide-platforms">
            <h2 id="guide-platforms">本地程序与简历插件，分别做什么</h2>
            <p>{guide.platformIntroduction}</p>
            <div className="recruitment-guide-columns platform-guide-tools">
              <div><h3>本地程序：AI 筛选与自动打招呼</h3><p>Windows 与 macOS 均提供本地程序。先准备岗位要求，再登录对应招聘平台，确认筛选条件后开始任务。运行记录帮助你检查分析与处理结果。</p></div>
              <div><h3>浏览器插件：手动确认并导入简历</h3><p>在已支持的候选人页面查看资料，核对有效手机号后手动导入。插件不替本地程序执行自动招聘，导入后可在简历库继续整理和跟进。</p></div>
            </div>
            <p className="recruitment-guide-release-note">{AUTO_REPLY_RELEASE_NOTICE}</p>
            <div className="platform-guide-tutorial">
              <div><h3>先看操作，再跑第一个岗位</h3><p>视频教程讲解安装本地程序、创建岗位和开始任务；本页上方按所选平台说明具体步骤。</p></div>
              <Link className="recruitment-guide-button" href="/videos">观看使用教程 <span aria-hidden="true">→</span></Link>
            </div>
          </section>
        ) : <section className="recruitment-guide-section" aria-labelledby="guide-platforms">
          <h2 id="guide-platforms">平台支持到哪一步，先看清楚</h2>
          <p>{guide.platformIntroduction}</p>
          <p className="recruitment-guide-summary">{RECRUITMENT_PLATFORM_SUMMARY}</p>
          <RecruitmentPlatformOverview />
        </section>}

        <section className="recruitment-guide-section" aria-labelledby="guide-limits">
          <h2 id="guide-limits">我小声提醒一下，使用范围也要看</h2>
          <div className="recruitment-guide-columns">
            {guide.limits.map((limit) => <div key={limit.title}><h3>{limit.title}</h3><p>{limit.text}</p></div>)}
          </div>
        </section>

        <section className="recruitment-guide-section" aria-labelledby="guide-faq">
          <h2 id="guide-faq">你可能还想问</h2>
          <div className="recruitment-guide-faq">
            {guide.faqs.map((faq, index) => <details key={faq.question} name="recruitment-guide-faq" open={index === 0}><summary><h3>{faq.question}<DownOutlined aria-hidden="true" /></h3></summary><p>{faq.answer}</p></details>)}
          </div>
        </section>

        <section className="recruitment-guide-section" aria-labelledby="guide-related">
          <h2 id="guide-related">接下来，挑你需要的看</h2>
          <div className="recruitment-guide-columns">
            {guide.related.map((item) => <div key={item.path}><h3><Link className="recruitment-guide-link" href={item.path}>{item.title} <span aria-hidden="true">→</span></Link></h3><p>{item.description}</p></div>)}
          </div>
        </section>
        {guide.platformId ? <>
          <section className="recruitment-guide-section" aria-labelledby="guide-other-platforms">
            <h2 id="guide-other-platforms">其他招聘平台，也可以分别了解</h2>
            <nav className="platform-guide-navigation" aria-label="招聘平台专题">
              {PLATFORM_GUIDES.filter((item) => item.platformId !== guide.platformId).map((item) => (
                <Link key={item.path} className="recruitment-guide-link" href={item.path}>{item.eyebrow.split(" · ")[0]} <span aria-hidden="true">→</span></Link>
              ))}
            </nav>
          </section>
          <section className="platform-guide-start" aria-labelledby="guide-start">
            <div><h2 id="guide-start">先从一个岗位开始，少做一点重复工作</h2><p>注册即送体验金额。先免费试用，满意再充值。</p></div>
            <Link className="recruitment-guide-button" href="/download">下载免费试用 <span aria-hidden="true">→</span></Link>
          </section>
        </> : null}
      </div>
    </MarketingShell>
  );
}
