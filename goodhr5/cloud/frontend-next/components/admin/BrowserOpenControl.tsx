/** 本文件提供顶部浏览器打开入口、等待反馈和只需确认一次的浏览器图标提示。 */
"use client";

import { Button, Flex, Image, Spin, Tooltip, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import { detectLocalAgent, openLocalPage } from "@/lib/admin-api";
import { getOnboardingGuideContext, publishOnboardingEvent, subscribeOnboardingGuideContext } from "@/lib/onboarding-events";
import AdminDialog from "./AdminDialog";

const BROWSER_ICON = "/assets/platforms/camoufox.png";
const NOTICE_KEY = "goodhr_browser_open_notice_ack_v1";

type Props = {
  agentBase: string;
  onAgentDetected: (base: string) => void;
  notify: (
    message: string,
    severity?: "success" | "error" | "warning" | "info",
  ) => void;
};

/** BrowserOpenControl 在探测和启动阶段显示转圈，并说明从任务栏打开浏览器的方法。 */
export default function BrowserOpenControl({
  agentBase,
  onAgentDetected,
  notify,
}: Props) {
  const [opening, setOpening] = useState(false);
  const [noticeOpen, setNoticeOpen] = useState(false);
  const [guide, setGuide] = useState(getOnboardingGuideContext);
  const inFlight = useRef(false);
  const acknowledged = useRef(false);
  const guidedOpening = guide.active && ["platform-open", "platform-login"].includes(guide.step || "") && Boolean(guide.browserOpening);
  const showingOpening = opening || guidedOpening;

  useEffect(() => subscribeOnboardingGuideContext(setGuide), []);
  useEffect(() => { if (guide.active) setNoticeOpen(false); }, [guide.active]);

  /** openBrowser 等待本地程序真正打开页面后再提示成功，失败时允许重新点击。 */
  async function openBrowser() {
    const currentGuide = getOnboardingGuideContext();
    if (currentGuide.active && ["platform-open", "platform-login"].includes(currentGuide.step || "")) {
      if (!currentGuide.browserOpening) publishOnboardingEvent({ type: "platform-browser-open-requested", platformId: currentGuide.platformId });
      return;
    }
    if (inFlight.current) return;
    inFlight.current = true;
    setOpening(true);
    try {
      const base = agentBase || (await detectLocalAgent());
      if (!base) {
        notify("我没叫醒本地程序，你先确认它开着，再点我一次。", "warning");
        return;
      }
      onAgentDetected(base);
      await openLocalPage(base, {
        persistent: true,
        user_data_dir: "default",
        headless: false,
        humanize: true,
        url: "https://www.bing.com",
      });
      if (getOnboardingGuideContext().active) return;
      let seen = acknowledged.current;
      try {
        seen = seen || localStorage.getItem(NOTICE_KEY) === "1";
      } catch {
        /* 缓存不可用时仍正常打开浏览器。 */
      }
      if (seen) notify("浏览器已经打开，可以过去操作了。", "success");
      else setNoticeOpen(true);
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "浏览器没打开成功，可以再点一次试试。",
        "error",
      );
    } finally {
      inFlight.current = false;
      setOpening(false);
    }
  }

  /** acknowledgeNotice 仅在点击“知道了”后记录确认，关闭弹框不会提前永久隐藏。 */
  function acknowledgeNotice() {
    acknowledged.current = true;
    try {
      localStorage.setItem(NOTICE_KEY, "1");
    } catch {
      /* 当前页面内仍记住确认，避免重复打扰。 */
    }
    setNoticeOpen(false);
  }

  const icon = (
    // 教程加载时保留图标占位和按钮文案，避免转圈结束后按钮变宽、遮罩沿用中途尺寸。
    <span style={{ width: 22, height: 22, display: "inline-flex", alignItems: "center", justifyContent: "center" }}>
      {guidedOpening ? <Spin size="small" /> : <Image src={BROWSER_ICON} alt="" width={22} height={22} preview={false} />}
    </span>
  );
  return (
    <>
      <Button
        className="goodhr-desktop-sm"
        data-onboarding="browser-open"
        icon={icon}
        loading={opening}
        aria-busy={showingOpening}
        onClick={() => void openBrowser()}
      >
        {opening ? "正在打开" : "打开浏览器"}
      </Button>
      <Tooltip title={showingOpening ? "正在打开浏览器" : "打开浏览器"}>
        <Button
          className="goodhr-mobile-sm"
          data-onboarding="browser-open"
          aria-label="打开浏览器"
          aria-busy={showingOpening}
          icon={icon}
          loading={opening}
          onClick={() => void openBrowser()}
        />
      </Tooltip>
      <AdminDialog
        open={noticeOpen}
        title="浏览器已经打开啦"
        confirmText="知道了"
        showCancel={false}
        maxWidth="xs"
        onClose={() => setNoticeOpen(false)}
        onConfirm={acknowledgeNotice}
      >
        <Flex vertical gap={16} align="center" style={{ textAlign: "center" }}>
          <Image
            src={BROWSER_ICON}
            alt="橙红色狐狸浏览器图标"
            width={80}
            height={80}
            preview={false}
          />
          <Typography.Paragraph>
            请点开这个图标对应的浏览器，登录招聘平台或继续操作。
          </Typography.Paragraph>
          <Typography.Paragraph type="secondary">
            如果没看到窗口，请在底部任务栏或程序坞找这个图标。点“知道了”后，下次就不打扰你了。
          </Typography.Paragraph>
        </Flex>
      </AdminDialog>
    </>
  );
}
