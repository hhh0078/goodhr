/** 本文件负责官网本地程序、浏览器插件的分类下载与版本更新入口。 */

import AppleIcon from "@ant-design/icons/lib/icons/AppleOutlined";
import CheckCircleRoundedIcon from "@ant-design/icons/lib/icons/CheckCircleOutlined";
import DownloadRoundedIcon from "@ant-design/icons/lib/icons/DownloadOutlined";
import WindowRoundedIcon from "@ant-design/icons/lib/icons/WindowsOutlined";
import Button from "antd/es/button";
import type { Metadata } from "next";
import type { ReactNode } from "react";
import MarketingShell from "@/components/MarketingShell";
import DownloadUpdateRecords from "@/components/download/DownloadUpdateRecords";
import BrowserExtensionInstallGuide from "@/components/download/BrowserExtensionInstallGuide";
import styles from "@/components/download/download.module.css";
import { getLocalAgentUpdates, getBrowserExtensionRelease, type BrowserExtensionRelease } from "@/lib/public-data";
import { createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "下载GoodHR - HR与猎头招聘自动化工具",
  description:
    "下载 GoodHR Windows 或 macOS 本地程序与浏览器插件，用于招聘自动化、AI筛选、自动回复和候选人简历导入；查看各版本更新说明。",
  path: "/download",
  keywords: [
    "招聘软件免费下载",
    "BOSS自动打招呼软件下载",
    "猎聘自动化工具下载",
    "HR招聘助手下载",
  ],
});

/** DownloadPage 分开展示本地程序与浏览器插件，并保留服务端渲染的近期更新记录。 */
export default async function DownloadPage() {
  const [updates, extension] = await Promise.all([getLocalAgentUpdates(), getBrowserExtensionRelease()]);
  const windows = updates.find((item) => item.urlWin);
  const mac = updates.find((item) => item.urlMac);

  return (
    <MarketingShell
      eyebrow="下载中心"
      title="下载并安装 GoodHR"
      description="本地程序帮你跑招聘任务，浏览器插件帮你把候选人记入简历库。按需要选择，也可以一起用。"
    >
      <section className="public-download-2">
        <div className="public-container">
          <nav className={styles.categories} aria-label="下载分类">
            <Button href="#local-agent" icon={<DownloadRoundedIcon aria-hidden="true" />}>
              本地程序下载
            </Button>
            <Button href="#browser-extension" icon={<DownloadRoundedIcon aria-hidden="true" />}>
              浏览器插件下载
            </Button>
          </nav>
          <section id="local-agent" className={styles.downloadSection} aria-labelledby="local-agent-heading">
            <h2 id="local-agent-heading" className="public-text public-download-11">本地程序下载</h2>
            <p className={`public-text public-download-18 ${styles.sectionDescription}`}>
              运行招聘任务、AI 筛选和自动回复。安装后会自动打开控制台。
            </p>
            <div className="public-download-4">
              <DownloadCard
                icon={<WindowRoundedIcon aria-hidden="true" />}
                system="Windows"
                note={`支持 Windows 10 / 11，推荐 64 位系统。${windows?.version ? `当前版本 ${windows.version}。` : ""}`}
                href={windows?.urlWin || ""}
                available={Boolean(windows?.urlWin)}
              />
              <DownloadCard
                icon={<AppleIcon aria-hidden="true" />}
                system="macOS"
                note={`适用于 Apple 芯片和 Intel 芯片电脑。${mac?.version ? `当前版本 ${mac.version}。` : ""}`}
                href={mac?.urlMac || ""}
                available={Boolean(mac?.urlMac)}
              />
            </div>
          </section>
          <BrowserExtensionDownloads release={extension} />
          <DownloadUpdateRecords updates={updates} />
          <div className="public-download-10">
            <h2 className="public-text public-download-11">安装前说明</h2>
            <div className="public-download-12">
              {[
                "安装时请勾选创建桌面快捷方式。",
                "首次启动会检查浏览器、Node 和 OCR 等运行组件。",
                "招聘平台登录状态、截图和浏览器资料只保存在当前电脑。",
              ].map((text) => (
                <div key={text} className="public-download-13">
                  <CheckCircleRoundedIcon
                    className="public-download-14"
                    aria-hidden="true"
                  />
                  <p className="public-text public-download-15">{text}</p>
                </div>
              ))}
            </div>
          </div>
          <div className="public-download-16">
            <h2 className="public-text public-download-17">适合哪些招聘工作</h2>
            <p className="public-text public-download-18">
              适合需要在
              BOSS直聘、猎聘、智联招聘、前程无忧、拉勾、58同城、店长直聘、赶集直招、鱼泡直聘和脉脉等平台处理大量候选人的
              HR
              与猎头。可用于关键词筛选、AI筛选简历、自动打招呼、AI自动回复消息、候选人跟进和招聘简历下载整理。
            </p>
          </div>
        </div>
      </section>
    </MarketingShell>
  );
}
/** BrowserExtensionDownloads 展示后台配置的 Chrome/Firefox 插件包与准确的安装方式。 */
function BrowserExtensionDownloads({release}: {release: BrowserExtensionRelease | null}) {
  return <section id="browser-extension" className={`public-download-10 ${styles.downloadSection}`} aria-labelledby="browser-extension-heading">
    <h2 id="browser-extension-heading" className="public-text public-download-11">浏览器插件下载{release?.version ? ` · v${release.version}` : ""}</h2>
    <p className={`public-text public-download-18 ${styles.sectionDescription}`}>打开猎聘企业端、猎头端、智联或前程无忧候选人详情后，插件会自动识别简历并打开助手。查重、补手机号、记备注和入库都在助手里完成。</p>
    <div className="public-download-4">
      <DownloadCard icon={<DownloadRoundedIcon />} system="Chrome" buttonLabel="下载 Chrome 插件" note="下载前会先展示安装教程。Chrome 在 Windows 上需要解压后，通过开发者模式加载插件文件夹。" href={release?.url_chrome || ""} available={Boolean(release?.url_chrome)} browser="Chrome" />
      <DownloadCard icon={<DownloadRoundedIcon />} system="Firefox" buttonLabel="下载 Firefox 插件" note={!release?.url_firefox ? "本轮先开放 Chrome，Firefox 等功能稳定后再提供。" : release.firefox_signed ? "已签名安装包，下载前会先展示安装教程。" : "当前为开发包，下载页会说明临时加载方法。"} href={release?.url_firefox || ""} available={Boolean(release?.url_firefox)} browser="Firefox" />
    </div>
    {release?.note ? <p className="public-text public-download-18">{release.note}</p> : null}
    <p className="public-text public-download-18">浏览器插件统一版本号，开放的版本以本页下载入口为准。插件仅读取可见候选人信息；悬浮界面独立显示，不自动点击或解锁手机号。</p>
  </section>;
}

/** DownloadCard 展示一个操作系统的下载入口。 */
function DownloadCard({
  icon,
  system,
  note,
  href,
  available,
  buttonLabel = "下载安装程序",
  browser,
}: {
  icon: ReactNode;
  system: string;
  note: string;
  href: string;
  available: boolean;
  buttonLabel?: string;
  browser?: "Chrome" | "Firefox";
}) {
  return (
    <div className="public-panel public-download-19">
      <div className="public-download-20">{icon}</div>
      <h3 className="public-text public-download-21">{system}</h3>
      <p className="public-text public-download-22">{note}</p>
      {available && browser ? (
        <BrowserExtensionInstallGuide
          href={href}
          label={buttonLabel}
          browser={browser}
          type="primary"
          className="public-download-23"
        />
      ) : available ? (
        <Button
          href={href}
          type="primary"
          icon={<DownloadRoundedIcon aria-hidden="true" />}
          className="public-download-23"
        >
          {buttonLabel}
        </Button>
      ) : (
        <Button disabled type="default" className="public-download-25">
          正在准备中
        </Button>
      )}
    </div>
  );
}
