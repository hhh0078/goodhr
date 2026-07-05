/** 本文件负责从云端 system.guide 配置读取并展示视频教程列表。 */
"use client";

import PlayCircleOutlineRoundedIcon from "@mui/icons-material/PlayCircleOutlineRounded";
import { Box, CircularProgress, Typography } from "@mui/material";
import { useEffect, useState } from "react";

type GuideVideo = {
  id: string;
  title: string;
  description: string;
  src: string;
  enabled: boolean;
};

const CLOUD_API_BASE = (
  process.env.NEXT_PUBLIC_CLOUD_API_BASE || "https://goodhr5.58it.cn"
).replace(/\/$/, "");

/** VideoGuideList 从云端读取视频教程配置并渲染。 */
export default function VideoGuideList() {
  const [videos, setVideos] = useState<GuideVideo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;

    loadGuideVideos()
      .then((items) => {
        if (alive) {
          setVideos(items);
        }
      })
      .catch((loadError) => {
        if (alive) {
          setError(
            loadError instanceof Error ? loadError.message : "视频教程读取失败",
          );
        }
      })
      .finally(() => {
        if (alive) {
          setLoading(false);
        }
      });

    return () => {
      alive = false;
    };
  }, []);

  if (loading) {
    return (
      <Box sx={{ py: 8, display: "grid", placeItems: "center" }}>
        <CircularProgress />
      </Box>
    );
  }

  if (error) {
    return <EmptyVideoState text={`视频教程没读出来：${error}`} />;
  }

  if (!videos.length) {
    return (
      <EmptyVideoState text="这里暂时还没有视频教程。配置好 system.guide.videos 后，我刷新一下就能看见。" />
    );
  }

  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: { xs: "1fr", md: "repeat(2, 1fr)" },
        gap: 4,
      }}
    >
      {videos.map((video) => (
        <Box component="article" key={video.id || video.title}>
          <Box
            sx={{
              overflow: "hidden",
              aspectRatio: "16 / 9",
              border: "1px solid",
              borderColor: "divider",
              borderRadius: "8px",
              bgcolor: "#17211c",
            }}
          >
            <Box
              component="iframe"
              src={video.src}
              title={video.title}
              loading="lazy"
              allowFullScreen
              sx={{ width: "100%", height: "100%", border: 0 }}
            />
          </Box>
          <Typography
            component="h2"
            sx={{ mt: 2.5, fontSize: 22, fontWeight: 760 }}
          >
            <PlayCircleOutlineRoundedIcon
              color="primary"
              sx={{ mr: 1, verticalAlign: "middle" }}
            />
            {video.title}
          </Typography>
          <Typography sx={{ mt: 1, color: "text.secondary", lineHeight: 1.8 }}>
            {video.description}
          </Typography>
        </Box>
      ))}
    </Box>
  );
}

/** loadGuideVideos 每次从云端无缓存读取视频教程配置。 */
async function loadGuideVideos(): Promise<GuideVideo[]> {
  const response = await fetch(
    `${CLOUD_API_BASE}/api/help/guide?t=${Date.now()}`,
    {
      cache: "no-store",
      headers: {
        "Cache-Control": "no-cache",
        Pragma: "no-cache",
      },
    },
  );

  if (!response.ok) {
    throw new Error(`接口状态 ${response.status}`);
  }

  const data = await response.json();
  const source = Array.isArray(data?.guide?.videos) ? data.guide.videos : [];

  return source
    .map(normalizeVideo)
    .filter((item: GuideVideo) => item.enabled && item.title && item.src);
}

/** normalizeVideo 兼容常见视频地址字段，统一成页面可渲染格式。 */
function normalizeVideo(value: Record<string, unknown>): GuideVideo {
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
    <Box
      sx={{
        py: 5,
        px: 2,
        border: "1px solid",
        borderColor: "divider",
        borderRadius: "8px",
        bgcolor: "#f7faf8",
      }}
    >
      <Typography sx={{ color: "text.secondary", lineHeight: 1.8 }}>
        {text}
      </Typography>
    </Box>
  );
}
