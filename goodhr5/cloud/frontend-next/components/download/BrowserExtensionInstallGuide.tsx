/** 本文件负责在浏览器插件下载前展示统一的安装与使用步骤。 */
"use client";

import DownloadOutlined from "@ant-design/icons/lib/icons/DownloadOutlined";
import { Alert, Button, Flex, Modal, Typography } from "antd";
import { useState, type MouseEvent, type ReactNode } from "react";

type BrowserExtensionInstallGuideProps = {
  href: string;
  label: string;
  browser?: "Chrome" | "Firefox";
  className?: string;
  type?: "default" | "primary";
  icon?: ReactNode;
  finalLabel?: string;
};

/** BrowserExtensionInstallGuide 先显示教程，再保留真实下载链接作为无脚本和最终下载入口。 */
export default function BrowserExtensionInstallGuide({
  href,
  label,
  browser = "Chrome",
  className,
  type = "default",
  icon = <DownloadOutlined aria-hidden="true" />,
  finalLabel,
}: BrowserExtensionInstallGuideProps) {
  const [open, setOpen] = useState(false);
  const isDownloadPage = href.startsWith("/");

  /** showGuide 拦下正常点击并先展示教程；没有 JavaScript 时链接仍可直接打开。 */
  function showGuide(event: MouseEvent<HTMLElement>) {
    event.preventDefault();
    setOpen(true);
  }

  return (
    <>
      <Button
        href={href}
        type={type}
        icon={icon}
        className={className}
        onClick={showGuide}
      >
        {label}
      </Button>
      <Modal
        open={open}
        title="安装浏览器插件"
        width={620}
        onCancel={() => setOpen(false)}
        footer={
          <Flex justify="flex-end" gap={8} wrap>
            <Button onClick={() => setOpen(false)}>先不安装</Button>
            <Button
              type="primary"
              href={href}
              target={isDownloadPage ? "_self" : undefined}
              rel={isDownloadPage ? undefined : "noopener noreferrer"}
              icon={<DownloadOutlined aria-hidden="true" />}
            >
              {finalLabel || (isDownloadPage ? "前往插件下载" : `下载 ${browser} 插件`)}
            </Button>
          </Flex>
        }
      >
        <Flex vertical gap={14}>
          <section aria-labelledby="browser-extension-install-heading">
            <Typography.Title id="browser-extension-install-heading" level={5}>
              安装方法
            </Typography.Title>
            {browser === "Chrome" ? (
              <ol>
                <li>下载插件压缩包，并在下载记录里打开文件所在位置。</li>
                <li>右键压缩包，选择“全部解压”。</li>
                <li>
                  在 Chrome 地址栏打开 <Typography.Text code>chrome://extensions</Typography.Text>，打开右上角“开发者模式”。
                </li>
                <li>点击“加载已解压的扩展程序”，选择刚才解压出来的插件文件夹。</li>
              </ol>
            ) : (
              <ol>
                <li>下载 Firefox 插件安装包。</li>
                <li>打开下载记录，点击安装包并按 Firefox 提示确认权限。</li>
              </ol>
            )}
          </section>
          <section aria-labelledby="browser-extension-usage-heading">
            <Typography.Title id="browser-extension-usage-heading" level={5}>
              使用方法
            </Typography.Title>
            <ol>
              <li>点击浏览器工具栏里的 GoodHR 图标，登录你的 GoodHR 账号。</li>
              <li>打开猎聘企业端、猎聘猎头端、智联或前程无忧的候选人详情，助手会自动识别并打开。</li>
              <li>手机号是入库必填项。没有识别到时先补上，再一键添加到简历库。</li>
            </ol>
          </section>
          <Alert
            type="info"
            showIcon
            title={browser === "Chrome" ? "Chrome 在 Windows 上不允许直接安装本站下载的插件包，需要解压后加载一次。" : "插件只读取当前打开的候选人资料，不会替你点击招聘页面。"}
          />
        </Flex>
      </Modal>
    </>
  );
}
