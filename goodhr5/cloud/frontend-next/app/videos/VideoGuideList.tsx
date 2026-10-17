/** 本文件负责展示服务端传入的视频教程列表。 */
"use client";

import PlayCircleOutlineRoundedIcon from "@ant-design/icons/lib/icons/PlayCircleOutlined";

import { useEffect, useState } from "react";
import { normalizeGuideVideos, type GuideVideo } from "@/lib/guide-videos";
import VideoGuidePlayer from "./VideoGuidePlayer";

/** VideoGuideList 渲染视频教程配置。 */
export default function VideoGuideList({ videos }: { videos: GuideVideo[] }) {
  const [clientVideos, setClientVideos] = useState<GuideVideo[]>(() => normalizeGuideVideos(videos));
  const [loading, setLoading] = useState(!clientVideos.length);
  useEffect(() => {
    const configured = normalizeGuideVideos(videos);
    setClientVideos(configured);
    if (configured.length) {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    const timeout = window.setTimeout(() => {
      controller.abort();
      setLoading(false);
    }, 10_000);
    void loadGuideVideos(controller.signal).then((items) => {
      if (controller.signal.aborted) return;
      window.clearTimeout(timeout);
      setClientVideos(items);
      setLoading(false);
    });
    return () => {
      window.clearTimeout(timeout);
      controller.abort();
    };
  }, [videos]);
  if (!clientVideos.length) {
    return (
      <EmptyVideoState text={loading ? "我正在找视频教程，稍等一下。" : "视频教程暂时还没准备好。你可以先查看下载说明，稍后再来。"} />
    );
  }

  return (
    <div className={`public-videos-videoguidelist-2${clientVideos.length === 1 ? " public-videos-single" : ""}`}>
      {clientVideos.map((video, index) => (
        <article key={`${video.id}:${video.src}:${index}`}>
          <VideoGuidePlayer video={video} />
          <h2 className="public-text public-videos-videoguidelist-6">
            <PlayCircleOutlineRoundedIcon
              className="public-videos-videoguidelist-7"
              aria-hidden="true"
            />
            {video.title}
          </h2>
          <p className="public-text public-videos-videoguidelist-8">
            {video.description}
          </p>
        </article>
      ))}
    </div>
  );
}

/** loadGuideVideos 在浏览器端兜底读取云端视频教程配置。 */
async function loadGuideVideos(signal: AbortSignal): Promise<GuideVideo[]> {
  const fallback =
    process.env.NODE_ENV === "production"
      ? "https://goodhr.58it.cn"
      : "http://127.0.0.1:8084";
  const baseURL = (process.env.NEXT_PUBLIC_CLOUD_API_BASE || fallback).replace(
    /\/$/,
    "",
  );
  try {
    const response = await fetch(`${baseURL}/api/help/guide`, {
      cache: "no-store",
      signal,
    });
    if (!response.ok) return [];
    const data = await response.json();
    return normalizeGuideVideos(data);
  } catch {
    return [];
  }
}

/** EmptyVideoState 展示视频教程空状态。 */
function EmptyVideoState({ text }: { text: string }) {
  return (
    <div className="public-videos-videoguidelist-9">
      <p className="public-text public-videos-videoguidelist-10">{text}</p>
      <a href="/download">查看下载说明</a>
    </div>
  );
}
