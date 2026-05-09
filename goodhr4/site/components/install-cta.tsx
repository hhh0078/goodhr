"use client";

import { useMemo, useState } from "react";

type InstallCTAProps = {
  downloadUrl: string;
  chromeUrl?: string;
};

function isChromeBrowser(): boolean {
  if (typeof navigator === "undefined") return true;
  const ua = navigator.userAgent;
  if (/Edg\//.test(ua)) return true;
  return /Chrome\//.test(ua) && /Google Inc/.test(navigator.vendor);
}

function pickPlatformDownload(url: string) {
  if (typeof window === "undefined") return url;
  const isMac =
    /(Mac|iPhone|iPad|iPod)/i.test(navigator.platform) ||
    /Macintosh|MacIntel|MacPPC|Mac68K|Mac OS/i.test(navigator.userAgent);
  return `${url}${url.includes("?") ? "&" : "?"}platform=${isMac ? "mac" : "windows"}&v=${Date.now()}`;
}

export function InstallCTA({ downloadUrl, chromeUrl }: InstallCTAProps) {
  const [open, setOpen] = useState(false);
  const [message, setMessage] = useState("");
  const [actionLabel, setActionLabel] = useState("确定");
  const [action, setAction] = useState<() => void>(() => () => setOpen(false));

  const fallbackChromeUrl = useMemo(
    () => chromeUrl || "https://www.google.cn/intl/zh-CN_ALL/chrome/fallback/",
    [chromeUrl],
  );

  function showModal(text: string, label: string, cb: () => void) {
    setMessage(text);
    setActionLabel(label);
    setAction(() => cb);
    setOpen(true);
  }

  function handleInstall() {
    if (!isChromeBrowser()) {
      showModal(
        "检测到你当前不是 Chrome/Edge 浏览器，插件仅支持 Chrome 内核浏览器。是否前往下载 Chrome？",
        "下载 Chrome",
        () => {
          window.location.href = fallbackChromeUrl;
        },
      );
      return;
    }
    showModal("检测到当前浏览器可直接安装，点击下方按钮开始下载插件。", "下载插件", () => {
      window.location.href = pickPlatformDownload(downloadUrl);
    });
  }

  return (
    <>
      <button type="button" className="btn btn-primary install-main-btn" onClick={handleInstall}>
        立即安装
      </button>
      <a className="btn btn-outline" href={downloadUrl}>
        下载失败点这里
      </a>
      {open ? (
        <div className="install-modal-overlay" onClick={() => setOpen(false)}>
          <div className="install-modal" onClick={(e) => e.stopPropagation()}>
            <h3>安装提示</h3>
            <p>{message}</p>
            <div className="install-modal-actions">
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => {
                  setOpen(false);
                  action();
                }}
              >
                {actionLabel}
              </button>
              <button type="button" className="btn btn-outline" onClick={() => setOpen(false)}>
                取消
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}
