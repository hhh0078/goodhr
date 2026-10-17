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
import LocalAgentInstallGuide from "@/components/download/LocalAgentInstallGuide";
import { BROWSER_EXTENSION_SUPPORTED_PLATFORM_TEXT } from "@/lib/browser-extension-platforms";
import styles from "@/components/download/download.module.css";
import { getLocalAgentUpdates, getBrowserExtensionRelease, getFirefoxTemporaryRelease, type BrowserExtensionRelease, type FirefoxTemporaryRelease } from "@/lib/public-data";
import { createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "下载GoodHR - HR与猎头招聘自动化工具",
  description:
    `下载 GoodHR Windows 或 macOS 本地程序，以及支持${BROWSER_EXTENSION_SUPPORTED_PLATFORM_TEXT}的 Chrome、Firefox 简历插件；用于招聘自动化、AI筛选、自动回复和候选人简历导入。`,
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
  const [updates, extension, firefox] = await Promise.all([getLocalAgentUpdates(), getBrowserExtensionRelease(), getFirefoxTemporaryRelease()]);
  const windows = updates.find((item) => item.urlWin);
  const mac = updates.find((item) => item.urlMac);

  return (
    <MarketingShell
      eyebrow="下载中心"
      title="下载并安装 GoodHR"
      description="本地程序帮你跑招聘任务，浏览器插件帮你把候选人记入简历库。按需要选择，也可以一起用。"
      showIntro={false}
    >
      <section className={`public-download-2 ${styles.content}`}>
        <div className="public-container">
          <section id="local-agent" className={styles.downloadSection} aria-labelledby="local-agent-heading">
            <h1 id="local-agent-heading" className="public-text public-download-11">本地程序下载</h1>
            <p className={`public-text public-download-18 ${styles.sectionDescription}`}>
              运行招聘任务、AI 筛选和自动回复。安装后会自动打开控制台。
            </p>
            <div className={`public-download-4 ${styles.cardGrid}`}>
              <DownloadCard
                icon={<WindowRoundedIcon aria-hidden="true" />}
                system="Windows"
                note={`支持 Windows 10 / 11，推荐 64 位系统。${windows?.version ? `当前版本 ${windows.version}。` : ""}`}
                href={windows?.urlWin || ""}
                available={Boolean(windows?.urlWin)}
                localPlatform="Windows"
              />
              <DownloadCard
                icon={<AppleIcon aria-hidden="true" />}
                system="macOS"
                note={`适用于 macOS 13 及以上的 Apple 芯片和 Intel 芯片电脑。${mac?.version ? `当前版本 ${mac.version}。` : ""}安装包暂未签名，首次打开如提示“移到废纸篓”，请查看下方安装前说明。`}
                href={mac?.urlMac || ""}
                available={Boolean(mac?.urlMac)}
                localPlatform="macOS"
              />
            </div>
          </section>
          <BrowserExtensionDownloads release={extension} firefox={firefox} />
          <DownloadUpdateRecords updates={updates} />
          <div className="public-download-10">
            <h2 className="public-text public-download-11">安装前说明</h2>
            <div className="public-download-12">
              {[
                "Windows 安装时可勾选桌面快捷方式；Mac 安装后从“应用程序”打开。",
                "Mac 首次打开安装包或程序时，若提示“移到废纸篓”，请先取消弹窗，再打开“系统设置 → 隐私与安全性”，滑到页面底部，点击“仍要打开”并按提示确认。若暂未看到该按钮，请重新尝试打开后再查看。",
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
/** BrowserExtensionDownloads 分别展示 Chrome 发布包和与本地程序一致的 Firefox 临时插件包。 */
function BrowserExtensionDownloads({release, firefox}: {release: BrowserExtensionRelease | null; firefox: FirefoxTemporaryRelease | null}) {
  const chromeVersion = release?.version_chrome || release?.version;
  return <section id="browser-extension" className={`public-download-10 ${styles.downloadSection}`} aria-labelledby="browser-extension-heading">
    <h2 id="browser-extension-heading" className="public-text public-download-11">浏览器插件下载</h2>
    <p className={`public-text public-download-18 ${styles.sectionDescription}`}><strong>当前支持：{BROWSER_EXTENSION_SUPPORTED_PLATFORM_TEXT}。</strong> 打开候选人详情后，插件会自动识别简历并打开助手。查重、补手机号、记备注和入库都在助手里完成。</p>
    <div className={`public-download-4 ${styles.cardGrid}`}>
      <DownloadCard icon={<DownloadRoundedIcon />} system="Chrome" buttonLabel="下载 Chrome 插件" note={`${chromeVersion ? `当前版本 ${chromeVersion}。` : ""}下载前会先展示安装教程。打开开发者模式后，把下载记录里的安装包直接拖进扩展管理页，无需解压。`} href={release?.url_chrome || ""} available={Boolean(release?.url_chrome)} browser="Chrome" />
      <DownloadCard icon={<DownloadRoundedIcon />} system="Firefox" buttonLabel="下载 Firefox 插件" note={firefox ? `当前版本 ${firefox.version}。GoodHR 自带火狐会自动更新并加载；普通 Firefox 请按教程临时加载。` : "暂时没读到最新插件，请稍后重试。GoodHR 自带火狐可在启动时检查更新。"} href={firefox?.url || ""} available={Boolean(firefox)} browser="Firefox" firefoxInstallMode="temporary" />
    </div>
    <p className="public-text public-download-18">各浏览器开放的版本以对应下载入口为准。插件仅读取可见候选人信息；悬浮界面独立显示，不自动点击或解锁手机号。</p>
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
  firefoxInstallMode,
  localPlatform,
}: {
  icon: ReactNode;
  system: string;
  note: string;
  href: string;
  available: boolean;
  buttonLabel?: string;
  browser?: "Chrome" | "Firefox";
  firefoxInstallMode?: "signed" | "temporary";
  localPlatform?: "Windows" | "macOS";
}) {
  return (
    <div className={`public-panel ${styles.downloadCard}`}>
      <div className={styles.cardHeading}>
        <span className={styles.cardIcon}>{icon}</span>
        <h3 className={`public-text ${styles.cardTitle}`}>{system}</h3>
      </div>
      <p className={`public-text ${styles.cardNote}`}>{note}</p>
      {available && browser ? (
        <BrowserExtensionInstallGuide
          href={href}
          label={buttonLabel}
          browser={browser}
          firefoxInstallMode={firefoxInstallMode}
          type="primary"
          className={styles.cardAction}
        />
      ) : available && localPlatform ? (
        <LocalAgentInstallGuide
          href={href}
          label={buttonLabel}
          platform={localPlatform}
          className={styles.cardAction}
        />
      ) : available ? (
        <Button
          href={href}
          type="primary"
          icon={<DownloadRoundedIcon aria-hidden="true" />}
          className={styles.cardAction}
        >
          {buttonLabel}
        </Button>
      ) : (
        <Button disabled type="default" className={styles.cardAction}>
          正在准备中
        </Button>
      )}
    </div>
  );
}
