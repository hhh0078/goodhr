import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "AI版教程",
  description:
    "GoodHR AI版使用教程，包含 token 获取、AI配置、岗位描述优化、模型选择与成本说明。",
};

const steps = [
  "进入轨迹流动（SiliconFlow）注册并完成手机号绑定。",
  "创建 API Key，并记录秘钥用于插件 AI 配置。",
  "在插件右上角进入“AI配置”，填入秘钥并保存。",
  "添加岗位描述与筛选要求，建议直接粘贴完整 JD。",
  "按结果调优关键词、模型和筛选参数。",
];

const notes = [
  "模型价格与效果相关：模型越强，单次筛选成本通常越高。",
  "不同模型按 token 消耗计费，批量筛选时应关注余额变化。",
  "建议先用轻量模型跑通流程，再切换高精度模型做重点岗位。",
];

export default function AITutorialPage() {
  return (
    <section>
      <h1>AI版教程</h1>
      <p className="lead">
        本页对应旧站 <code>aidescribe.html</code> 的主流程，保留“先取 token 再配置插件”的路径。
      </p>

      <article className="card">
        <h2>快速上手</h2>
        <ol className="step-list">
          {steps.map((step) => (
            <li key={step}>{step}</li>
          ))}
        </ol>
      </article>

      <article className="card">
        <h2>价格与模型说明</h2>
        <ul>
          {notes.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
        <p className="lead">
          你可以先从 <Link href="/tutorial/free">免费版教程</Link> 开始，再切换 AI 配置。
        </p>
      </article>
    </section>
  );
}
