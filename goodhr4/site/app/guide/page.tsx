import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "安装步骤",
  description:
    "GoodHR 插件安装与启动指南，包含浏览器检查、下载、解压、导入扩展、首次运行与更新入口。",
};

const steps = [
  "检查浏览器：建议 Windows 使用 Edge，Mac 使用 Chrome。",
  "点击官网首页“立即安装”下载插件压缩包并解压。",
  "打开扩展管理页（chrome://extensions/）并开启开发者模式。",
  "点击“加载已解压的扩展程序”并选择解压后的插件目录。",
  "在 Boss/猎聘 推荐页设置基础筛选条件后再打开插件。",
  "输入手机号或邮箱，配置岗位信息与关键词后开始运行。",
  "后续版本变更请在“更新记录”页面查看。",
];

export default function GuidePage() {
  return (
    <section>
      <h1>安装步骤</h1>
      <p className="lead">按旧站安装流程整理，第一次安装建议完整执行一遍。</p>
      <ol className="step-list">
        {steps.map((step) => (
          <li key={step}>{step}</li>
        ))}
      </ol>
      <div className="card note">
        <h2>提示</h2>
        <p>
          如果你通过邀请链接进入官网，请先在首页完成一次注册/登录；后续插件使用同一邮箱或手机号即可无感同步。
        </p>
      </div>
    </section>
  );
}
