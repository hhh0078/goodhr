import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "FAQ",
  description: "GoodHR 常见问题：注册、邀请、插件绑定、更新记录与账号同步说明。",
};

const faq = [
  {
    q: "点击“立即安装”没有反应怎么办？",
    a: "先检查浏览器右上角是否拦截下载，再使用首页“下载失败点这里”直接下载压缩包。",
  },
  {
    q: "插件导入后在浏览器里看不到？",
    a: "请确认已在扩展管理页打开开发者模式，并且选择的是解压后的插件目录。",
  },
  {
    q: "邀请链接是怎么生效的？",
    a: "官网会读取 URL 参数 invite 并缓存，注册时自动带上 inviter_id。",
  },
  {
    q: "邀请码可以覆盖吗？",
    a: "可以。按当前业务规则，后续注册请求带 inviter_id 时允许覆盖。",
  },
  {
    q: "插件可以直接读取官网 localStorage 吗？",
    a: "不能。不同域名存在浏览器隔离，建议通过后端接口做账号状态同步。",
  },
  {
    q: "为什么建议邮箱或手机号统一？",
    a: "因为后端账号标识就是 identifier，官网和插件使用同一标识可以无感同步。",
  },
  {
    q: "更新记录来源是哪里？",
    a: "由后端 update_records 表统一维护，官网通过 API 实时读取。",
  },
];

export default function FAQPage() {
  const faqLd = {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    mainEntity: faq.map((item) => ({
      "@type": "Question",
      name: item.q,
      acceptedAnswer: {
        "@type": "Answer",
        text: item.a,
      },
    })),
  };

  return (
    <section>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(faqLd) }}
      />
      <h1>常见问题</h1>
      <div className="faq-list">
        {faq.map((item) => (
          <details key={item.q} className="card faq-item">
            <summary>{item.q}</summary>
            <p>{item.a}</p>
          </details>
        ))}
      </div>
    </section>
  );
}
