import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "功能介绍",
  description: "GoodHR 核心功能：自动打招呼、关键词筛选、AI筛选、多岗位管理与运行参数配置。",
};

const items = [
  {
    title: "多平台支持",
    desc: "覆盖 BOSS直聘、猎聘、智联、58招聘及部分猎头端页面。",
  },
  {
    title: "关键词筛选",
    desc: "支持包含词与全匹配策略，先平台筛基础条件再插件精筛。",
  },
  {
    title: "AI筛选",
    desc: "按岗位描述做智能评估，可结合 token 余额选择不同模型。",
  },
  {
    title: "自动打招呼",
    desc: "根据岗位、关键词和 AI 结果自动执行沟通动作，减少重复点击。",
  },
  {
    title: "账号同步",
    desc: "手机号或邮箱绑定后可同步官网和插件配置，降低配置丢失风险。",
  },
  {
    title: "更新记录",
    desc: "版本说明由后端统一维护，官网实时读取 update_records 数据。",
  },
];

export default function FeaturesPage() {
  return (
    <section>
      <h1>功能介绍</h1>
      <p className="lead">以下条目按旧站“平台/功能/使用路径”重排，不改变原能力边界。</p>
      <div className="feature-grid">
        {items.map((item) => (
          <article key={item.title} className="card feature-item">
            <h2>{item.title}</h2>
            <p>{item.desc}</p>
          </article>
        ))}
      </div>
    </section>
  );
}
