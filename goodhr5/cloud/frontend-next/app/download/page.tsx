/** 本文件负责官网本地程序下载页面。 */

import AppleIcon from "@ant-design/icons/lib/icons/AppleOutlined";
import CheckCircleRoundedIcon from "@ant-design/icons/lib/icons/CheckCircleOutlined";
import DownloadRoundedIcon from "@ant-design/icons/lib/icons/DownloadOutlined";
import WindowRoundedIcon from "@ant-design/icons/lib/icons/WindowsOutlined";
import Button from "antd/es/button";
import type { Metadata } from "next";
import type { ReactNode } from "react";
import MarketingShell from "@/components/MarketingShell";
import { getLocalAgentUpdates, getBrowserExtensionRelease, type BrowserExtensionRelease, type LocalAgentUpdate } from "@/lib/public-data";
import { createPageMetadata } from "@/lib/seo";

export const metadata: Metadata = createPageMetadata({
  title: "下载GoodHR - HR与猎头招聘自动化工具",
  description:
    "下载 GoodHR Windows 或 macOS 本地程序，用于招聘平台自动筛选、AI筛选、自动打招呼、AI自动回复、OCR和简历下载管理。",
  path: "/download",
  keywords: [
    "招聘软件免费下载",
    "BOSS自动打招呼软件下载",
    "猎聘自动化工具下载",
    "HR招聘助手下载",
  ],
});

/** DownloadPage 提供 Windows 和 macOS 本地程序下载入口。 */
export default async function DownloadPage() {
  const [updates, extension] = await Promise.all([getLocalAgentUpdates(), getBrowserExtensionRelease()]);
  const windows = updates.find((item) => item.urlWin);
  const mac = updates.find((item) => item.urlMac);

  return (
    <MarketingShell
      eyebrow="本地程序"
      title="下载并安装 GoodHR"
      description="本地程序负责招聘平台浏览器操作、自动打招呼、截图、OCR、AI筛选流程和本地简历数据管理。安装后会自动打开控制台。"
    >
      <section className="public-download-2">
        <div className="public-container">
          <div className="public-download-4">
            <DownloadCard
              icon={<WindowRoundedIcon aria-hidden="true" />}
              system="Windows"
              note="支持 Windows 10 / 11，推荐 64 位系统。"
              href={windows?.urlWin || ""}
              available={Boolean(windows?.urlWin)}
            />
            <DownloadCard
              icon={<AppleIcon aria-hidden="true" />}
              system="macOS"
              note="适用于 Apple 芯片和 Intel 芯片电脑。"
              href={mac?.urlMac || ""}
              available={Boolean(mac?.urlMac)}
            />
          </div>
          <UpdateRecords updates={updates} />
          <BrowserExtensionDownloads release={extension} />
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
  return <section id="browser-extension" className="public-download-10" style={{scrollMarginTop: 96}}>
    <h2 className="public-text public-download-11">浏览器插件{release?.version ? ` · v${release.version}` : ""}</h2>
    <p className="public-text public-download-18">打开猎聘企业端、猎头端、智联或前程无忧候选人详情，点击页面上的 GoodHR 悬浮入口添加简历、查重、记备注。手机号没认出来时可以手动补齐；也可从浏览器工具栏打开。</p>
    <div className="public-download-4">
      <DownloadCard icon={<DownloadRoundedIcon />} system="Chrome" buttonLabel="下载 Chrome 插件" note="下载后解压，在扩展管理页打开开发者模式，加载已解压的扩展程序。" href={release?.url_chrome || ""} available={Boolean(release?.url_chrome)} />
      <DownloadCard icon={<DownloadRoundedIcon />} system="Firefox" buttonLabel="下载 Firefox 插件" note={!release?.url_firefox ? "本轮先开放 Chrome，Firefox 等功能稳定后再提供。" : release.firefox_signed ? "已签名安装包，下载后按 Firefox 提示安装。" : "当前为开发包：解压后通过 about:debugging 临时加载 manifest.json，重启浏览器后需重新加载。"} href={release?.url_firefox || ""} available={Boolean(release?.url_firefox)} />
    </div>
    {release?.note ? <p className="public-text public-download-18">{release.note}</p> : null}
    <p className="public-text public-download-18">浏览器插件统一版本号，开放的版本以本页下载入口为准。插件仅读取可见候选人信息，保存前由你确认；悬浮界面独立显示，不自动点击或解锁手机号。</p>
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
}: {
  icon: ReactNode;
  system: string;
  note: string;
  href: string;
  available: boolean;
  buttonLabel?: string;
}) {
  return (
    <div className="public-panel public-download-19">
      <div className="public-download-20">{icon}</div>
      <h2 className="public-text public-download-21">{system}</h2>
      <p className="public-text public-download-22">{note}</p>
      {available ? (
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

/** UpdateRecords 展示本地程序历史更新记录。 */
function UpdateRecords({ updates }: { updates: LocalAgentUpdate[] }) {
  return (
    <div className="public-download-26">
      <h2 className="public-text public-download-27">更新记录</h2>
      {updates.length > 0 ? (
        <div className="public-download-28">
          {updates.map((item, index) => (
            <div
              key={`${item.version}-${index}`}
              className="public-download-29"
            >
              <div className="public-download-30">
                <p className="public-text public-download-31">
                  {item.version || "未标版本"}
                </p>
                {index === 0 ? (
                  <p className="public-text public-download-32">最新安装包</p>
                ) : null}
              </div>
              <p className="public-text public-download-33">
                {item.note || "这版比较低调，暂时没有写更新说明。"}
              </p>
            </div>
          ))}
        </div>
      ) : (
        <p className="public-text public-download-34">
          这里暂时空空的，安装包还在路上。
        </p>
      )}
    </div>
  );
}
