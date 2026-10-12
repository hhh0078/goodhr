/** 本文件负责在 Windows 或 macOS 本地程序下载前展示对应的安装提醒。 */
"use client";

import DownloadOutlined from "@ant-design/icons/lib/icons/DownloadOutlined";
import { Alert, Button, Flex, Modal, Typography } from "antd";
import { useState, type MouseEvent } from "react";

type LocalAgentPlatform = "Windows" | "macOS";

type LocalAgentInstallGuideProps = {
  href: string;
  label: string;
  platform: LocalAgentPlatform;
  className?: string;
};

/** LocalAgentInstallGuide 保留真实下载链接，并在正常点击时先展示对应系统的安装步骤。 */
export default function LocalAgentInstallGuide({
  href,
  label,
  platform,
  className,
}: LocalAgentInstallGuideProps) {
  const [open, setOpen] = useState(false);
  const isWindows = platform === "Windows";

  /** showGuide 拦下正常点击并打开教程；禁用 JavaScript 时原下载链接仍然可用。 */
  function showGuide(event: MouseEvent<HTMLElement>) {
    event.preventDefault();
    setOpen(true);
  }

  return (
    <>
      <Button
        href={href}
        type="primary"
        icon={<DownloadOutlined aria-hidden="true" />}
        className={className}
        onClick={showGuide}
      >
        {label}
      </Button>
      <Modal
        open={open}
        title={`安装 ${platform} 版 GoodHR`}
        width={620}
        onCancel={() => setOpen(false)}
        footer={
          <Flex justify="flex-end" gap={8} wrap>
            <Button onClick={() => setOpen(false)}>先不下载</Button>
            <Button
              type="primary"
              href={href}
              icon={<DownloadOutlined aria-hidden="true" />}
            >
              下载 {platform} 版
            </Button>
          </Flex>
        }
      >
        <Flex vertical gap={14}>
          {isWindows ? (
            <>
              <Alert
                type="warning"
                showIcon
                title="下载和安装前，请先暂时退出 360、安全管家等可能拦截安装包的安全软件。装好后再重新打开就行。"
              />
              <section aria-labelledby="windows-local-agent-install-heading">
                <Typography.Title id="windows-local-agent-install-heading" level={5}>
                  安装方法
                </Typography.Title>
                <ol>
                  <li>点击下方“下载 Windows 版”，等待压缩包下载完成。</li>
                  <li>直接打开下载好的压缩包，在里面双击安装程序运行即可，无需先解压。</li>
                  <li>按安装提示完成操作，首次启动时 GoodHR 会检查所需组件。</li>
                </ol>
              </section>
            </>
          ) : (
            <section aria-labelledby="mac-local-agent-install-heading">
              <Typography.Title id="mac-local-agent-install-heading" level={5}>
                首次打开方法
              </Typography.Title>
              <ol>
                <li>下载后，首次打开 macOS 安装包或程序。</li>
                <li>如果提示“移到废纸篓”，先取消弹窗。</li>
                <li>打开“系统设置 → 隐私与安全性”，滑到页面底部，点击“仍要打开”并按提示确认。</li>
                <li>暂时没看到“仍要打开”时，再尝试打开一次安装包或程序，然后回到这里查看。</li>
              </ol>
            </section>
          )}
          <Alert
            type="info"
            showIcon
            title="如果安装失败，请联系作者协助。别和电脑较劲，我们一起看看卡在哪一步。"
          />
        </Flex>
      </Modal>
    </>
  );
}
