import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "免费版教程",
  description:
    "GoodHR 免费版使用教程，包含基础使用演示、AI配置说明、手机号绑定、岗位信息与关键词设置。",
};

const sections = [
  {
    title: "基础使用演示",
    items: [
      "先完成安装并在浏览器中打开 Boss直聘/猎聘 页面。",
      "在推荐候选人页筛选年龄、学历等基础条件后再启动插件。",
      "配置岗位信息与关键词后再执行自动打招呼。",
    ],
  },
  {
    title: "AI配置教程演示",
    items: [
      "插件本身不包含模型费用，模型按照 token 消耗计费。",
      "可先使用免费模型体验，再按需求升级高阶模型。",
      "配置时将 API 秘钥填入插件 AI 配置区域并保存。",
    ],
  },
  {
    title: "绑定手机号 / 邮箱",
    items: [
      "绑定后设置会保存在服务器，更新或异常时不易丢失。",
      "不绑定也可使用，但建议绑定以便官网和插件无感同步。",
      "统一使用同一手机号或邮箱，可减少多端状态不一致。",
    ],
  },
  {
    title: "岗位信息设置",
    items: [
      "岗位信息就是你正在招聘的岗位名称，至少添加一个岗位。",
      "多岗位场景可在插件中切换岗位，不需要重复配置全部参数。",
      "岗位描述尽量贴近实际 JD，便于后续 AI 筛选。",
    ],
  },
  {
    title: "关键词配置",
    items: [
      "关键词不要写成整句，建议使用短词组，如“数学老师”。",
      "不要把年龄、学历、性别写进关键词，这些用平台筛选功能处理。",
      "关键词过少会降低筛选精度，建议按岗位补充多个有效词。",
    ],
  },
];

export default function FreeTutorialPage() {
  return (
    <section>
      <h1>免费版教程</h1>
      <p className="lead">
        本页对应旧站 <code>describe.html</code> 的核心内容，按实际使用顺序整理。
      </p>
      <div className="tutorial-list">
        {sections.map((section) => (
          <article key={section.title} className="card tutorial-item">
            <h2>{section.title}</h2>
            <ul>
              {section.items.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </article>
        ))}
      </div>
    </section>
  );
}
