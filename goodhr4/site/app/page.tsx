import Link from "next/link";
import { Suspense } from "react";
import { RegisterCard } from "@/components/register-card";
import { UpdateTimeline } from "@/components/update-timeline";
import { InstallCTA } from "@/components/install-cta";
import { fetchBootstrap } from "@/lib/api";

export const revalidate = 60;

export default async function HomePage() {
  let announcement = "GoodHR 让招聘流程更轻松。";
  let updates: Awaited<ReturnType<typeof fetchBootstrap>>["updates"] = [];
  let downloadUrl = "https://goodhr.58it.cn/zip/goodHR3.2.1.zip";
  let chromeUrl = "https://www.google.cn/intl/zh-CN_ALL/chrome/fallback/";

  try {
    const bootstrap = await fetchBootstrap();
    announcement = (bootstrap.config.announcement as string) || announcement;
    updates = bootstrap.updates || [];
    downloadUrl = (bootstrap.config.download_url as string) || downloadUrl;
    chromeUrl = (bootstrap.config.chrome_download_url as string) || chromeUrl;
  } catch {
    // ignore backend bootstrap failure during static rendering
  }

  return (
    <>
      <section className="hero">
        <p className="eyebrow">Boss直聘 / 猎聘 / 智联 / 58招聘</p>
        <h1>AI筛选 + 自动打招呼，完整安装即用</h1>
        <p>{announcement} 首次使用请先点击“立即安装”，再按安装步骤导入扩展。</p>
        <div className="hero-actions">
          <InstallCTA downloadUrl={downloadUrl} chromeUrl={chromeUrl} />
          <Link className="btn btn-outline" href="/guide">
            安装步骤
          </Link>
          <Link className="btn btn-outline" href="/tutorial/free">
            免费版教程
          </Link>
          <Link className="btn btn-outline" href="/tutorial/ai">
            AI版教程
          </Link>
          <Link className="btn btn-outline" href="/features">
            功能介绍
          </Link>
        </div>
      </section>

      <section className="grid-two">
        <article className="card">
          <h2>支持平台</h2>
          <div className="info-columns">
            <div>
              <h3>招聘平台</h3>
              <p>BOSS直聘 / BOSS猎头端</p>
              <p>猎聘 / 猎聘猎头端</p>
              <p>智联招聘 / 58招聘</p>
            </div>
            <div>
              <h3>核心功能</h3>
              <p>关键词筛选</p>
              <p>AI筛选</p>
              <p>自动打招呼 / 索要手机号 / 索要简历</p>
            </div>
            <div>
              <h3>浏览器</h3>
              <p>微软 Edge</p>
              <p>谷歌 Chrome</p>
              <p>其他 Chrome 内核浏览器</p>
            </div>
          </div>
        </article>
        <Suspense fallback={<section className="register-card">正在加载注册表单...</section>}>
          <RegisterCard />
        </Suspense>
      </section>

      <section className="card">
        <h2>安装步骤（免费）</h2>
        <ol className="step-list">
          <li>确保你正在使用 Chrome/Edge 浏览器打开官网。</li>
          <li>点击上方“立即安装”，若被浏览器拦截请允许下载。</li>
          <li>
            打开 <code>chrome://extensions/</code>，开启开发者模式。
          </li>
          <li>将下载并解压后的插件目录拖入扩展管理页，完成导入。</li>
          <li>在 Boss/猎聘页面打开插件，输入邮箱或手机号后开始使用。</li>
        </ol>
        <p className="lead">
          如果你是通过邀请链接进入，邀请码会自动缓存并在注册时写入。
        </p>
      </section>

      <section className="card">
        <div className="section-head">
          <h2>最近更新</h2>
          <Link href="/updates">查看全部</Link>
        </div>
        <UpdateTimeline updates={updates.slice(0, 5)} />
      </section>
    </>
  );
}
