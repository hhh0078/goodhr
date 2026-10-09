/** 本文件负责展示服务端传入的视频教程列表。 */
"use client";

import PlayCircleOutlineRoundedIcon from "@ant-design/icons/lib/icons/PlayCircleOutlined";

import { useEffect, useState } from "react";
import type { GuideVideo } from "@/lib/public-data";

/** VideoGuideList 渲染视频教程配置。 */
export default function VideoGuideList({ videos }: { videos: GuideVideo[] }) {
  const [clientVideos, setClientVideos] = useState<GuideVideo[]>(videos);
  useEffect(() => {
    if (videos.length) return;
    void loadGuideVideos().then(setClientVideos);
  }, [videos]);
  if (!clientVideos.length) {
    return (
      <EmptyVideoState text="这里暂时还没有视频教程。配置好 system.guide.videos 后，我刷新一下就能看见。" />
    );
  }

  return (
    <div className="public-videos-videoguidelist-2">
      {clientVideos.map((video) => (
        <article key={video.id || video.title}>
          <div className="public-videos-videoguidelist-4">
            <iframe
              src={video.src}
              title={video.title}
              loading="lazy"
              allowFullScreen
              className="public-videos-videoguidelist-5"
            />
          </div>
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
async function loadGuideVideos() {
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
    });
    if (!response.ok) return [];
    const data = await response.json();
    const source = Array.isArray(data?.guide?.videos)
      ? data.guide.videos
      : Array.isArray(data?.videos)
        ? data.videos
        : [];
    return source
      .map(normalizeGuideVideo)
      .filter((item: GuideVideo) => item.enabled && item.title && item.src);
  } catch {
    return [];
  }
}

/** normalizeGuideVideo 将接口视频字段整理为页面展示结构。 */
function normalizeGuideVideo(value: Record<string, unknown>): GuideVideo {
  return {
    id: String(value?.id || value?.title || ""),
    title: String(value?.title || ""),
    description: String(value?.description || ""),
    src: String(
      value?.src || value?.url || value?.iframe_url || value?.video_url || "",
    ),
    enabled: value?.enabled !== false,
  };
}

/** EmptyVideoState 展示视频教程空状态。 */
function EmptyVideoState({ text }: { text: string }) {
  return (
    <div className="public-videos-videoguidelist-9">
      <p className="public-text public-videos-videoguidelist-10">{text}</p>
    </div>
  );
}
