/** 本文件复用官网布局展示招聘专题，并从同一份问答内容生成可见正文与 JSON-LD。 */

import Link from "next/link";
import MarketingShell from "@/components/MarketingShell";
import RecruitmentPlatformOverview from "@/components/RecruitmentPlatformOverview";
import StructuredData from "@/components/StructuredData";
import { AUTO_REPLY_RELEASE_NOTICE, RECRUITMENT_PLATFORM_SUMMARY } from "@/lib/recruitment-platforms";
import type { RecruitmentGuide } from "@/lib/recruitment-guides";
import { absoluteURL } from "@/lib/seo";
import "./recruitment-guide.css";

/** RecruitmentGuidePage 输出专题正文、平台能力、费用边界、常见问题和相关入口。 */
export default function RecruitmentGuidePage({ guide }: { guide: RecruitmentGuide }) {
  return (
    <MarketingShell eyebrow={guide.eyebrow} title={guide.heading} description={guide.introduction}>
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
            { "@type": "ListItem", position: 2, name: guide.eyebrow, item: absoluteURL(guide.path) },
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

      <div className="public-container recruitment-guide">
        <nav className="recruitment-guide-breadcrumb" aria-label="当前位置">
          <Link href="/">首页</Link><span aria-hidden="true">/</span><span aria-current="page">{guide.eyebrow}</span>
        </nav>
        <nav className="recruitment-guide-actions" aria-label="开始使用与查看费用">
          <Link className="recruitment-guide-button" href="/download">下载 GoodHR</Link>
          <Link className="recruitment-guide-link" href="/pricing">先看会员与费用 <span aria-hidden="true">→</span></Link>
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

        <section className="recruitment-guide-section" aria-labelledby="guide-platforms">
          <h2 id="guide-platforms">平台支持到哪一步，先看清楚</h2>
          <p>{guide.platformIntroduction}</p>
          <p className="recruitment-guide-summary">{RECRUITMENT_PLATFORM_SUMMARY}</p>
          <RecruitmentPlatformOverview />
        </section>

        <section className="recruitment-guide-section" aria-labelledby="guide-limits">
          <h2 id="guide-limits">我小声提醒一下，使用范围也要看</h2>
          <div className="recruitment-guide-columns">
            {guide.limits.map((limit) => <div key={limit.title}><h3>{limit.title}</h3><p>{limit.text}</p></div>)}
          </div>
        </section>

        <section className="recruitment-guide-section" aria-labelledby="guide-faq">
          <h2 id="guide-faq">你可能还想问</h2>
          <div className="recruitment-guide-faq">
            {guide.faqs.map((faq) => <div key={faq.question}><h3>{faq.question}</h3><p>{faq.answer}</p></div>)}
          </div>
        </section>

        <section className="recruitment-guide-section" aria-labelledby="guide-related">
          <h2 id="guide-related">接下来，挑你需要的看</h2>
          <div className="recruitment-guide-columns">
            {guide.related.map((item) => <div key={item.path}><h3><Link className="recruitment-guide-link" href={item.path}>{item.title} <span aria-hidden="true">→</span></Link></h3><p>{item.description}</p></div>)}
          </div>
        </section>
      </div>
    </MarketingShell>
  );
}
