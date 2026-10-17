/** 本文件提供按点击加载的原生视频播放器，兼容 MP4、原生 HLS 和 HLS 轻量回退。 */
"use client";

import PlayCircleOutlined from "@ant-design/icons/lib/icons/PlayCircleOutlined";
import { useEffect, useRef, useState } from "react";
import type Hls from "hls.js";
import { isHLSVideo, type GuideVideo } from "@/lib/guide-videos";

/** VideoGuidePlayer 点击后才请求媒体；离开页面或更换来源时释放播放器及下载任务。 */
export default function VideoGuidePlayer({ video }: { video: GuideVideo }) {
  const element = useRef<HTMLVideoElement>(null);
  const [activated, setActivated] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState<"idle" | "loading" | "ready" | "error">("idle");
  const [error, setError] = useState("");

  useEffect(() => {
    const media = element.current;
    if (!activated || !media) return;
    let disposed = false;
    let failed = false;
    let player: Hls | null = null;
    setStatus("loading");
    setError("");

    /** fail 停止当前加载并显示可重试的简短提示。 */
    function fail(message = "视频没加载成功，点一下重试，我再努力一次。") {
      if (disposed) return;
      failed = true;
      player?.stopLoad();
      setError(message);
      setStatus("error");
    }

    /** play 尝试开始播放；浏览器限制自动播放时保留原生播放按钮。 */
    function play() {
      if (disposed || failed || !media) return;
      void media.play().catch((reason: unknown) => {
        if (disposed || failed) return;
        if (reason instanceof DOMException && reason.name === "NotAllowedError") {
          setStatus("ready");
        } else if (!(reason instanceof DOMException && reason.name === "AbortError")) {
          fail();
        }
      });
    }

    /** ready 媒体可以播放后移除加载提示，继续保留系统原生控件。 */
    function ready() {
      if (!disposed && !failed) setStatus("ready");
    }

    /** mediaError 把原生视频加载失败转换成统一的重试入口。 */
    function mediaError() {
      fail();
    }

    /** load 按浏览器能力选择原生播放或按需导入官方轻量 HLS 实现。 */
    async function load() {
      if (!media) return;
      media.preload = "auto";
      if (!isHLSVideo(video.src) || media.canPlayType("application/vnd.apple.mpegurl")) {
        media.src = video.src;
        media.load();
        play();
        return;
      }
      try {
        const { default: HlsPlayer } = await import("hls.js/dist/hls.light.mjs");
        if (disposed) return;
        if (!HlsPlayer.isSupported()) {
          fail("这个浏览器暂时播不了该视频，可以换个浏览器再试。");
          return;
        }
        player = new HlsPlayer({
          maxBufferLength: 20,
          maxMaxBufferLength: 40,
          backBufferLength: 15,
          startFragPrefetch: false,
        });
        player.on(HlsPlayer.Events.ERROR, (_event, data) => {
          if (data.fatal) fail();
        });
        player.on(HlsPlayer.Events.MANIFEST_PARSED, play);
        player.loadSource(video.src);
        player.attachMedia(media);
      } catch {
        fail();
      }
    }

    media.addEventListener("loadeddata", ready);
    media.addEventListener("playing", ready);
    media.addEventListener("error", mediaError);
    void load();
    return () => {
      disposed = true;
      media.removeEventListener("loadeddata", ready);
      media.removeEventListener("playing", ready);
      media.removeEventListener("error", mediaError);
      player?.destroy();
      media.pause();
      media.removeAttribute("src");
      media.load();
    };
  }, [activated, attempt, video.src]);

  /** start 在真实点击或键盘激活后加载视频，重试时完整重建播放连接。 */
  function start() {
    setStatus("loading");
    setActivated(true);
    setAttempt((value) => value + 1);
  }

  return (
    <div className="public-videos-videoguidelist-4" aria-busy={status === "loading"}>
      <video
        ref={element}
        controls
        playsInline
        preload="none"
        poster={video.poster}
        aria-label={video.title}
        className="public-videos-videoguidelist-5"
      />
      {!activated && (
        <button type="button" className="public-video-start" onClick={start} aria-label={`播放${video.title}`}>
          <PlayCircleOutlined aria-hidden="true" />
          <span>点击播放</span>
        </button>
      )}
      {status === "loading" && <span className="public-video-loading" role="status">正在加载视频…</span>}
      {status === "error" && (
        <div className="public-video-error" role="alert">
          <p>{error}</p>
          <button type="button" onClick={start}>重新播放</button>
        </div>
      )}
      <noscript><a href={video.src}>打开视频：{video.title}</a></noscript>
    </div>
  );
}
