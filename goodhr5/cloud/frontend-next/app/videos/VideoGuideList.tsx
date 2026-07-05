/** 本文件负责展示服务端传入的视频教程列表。 */
import PlayCircleOutlineRoundedIcon from "@mui/icons-material/PlayCircleOutlineRounded";
import { Box, Typography } from "@mui/material";
import type { GuideVideo } from "@/lib/public-data";

/** VideoGuideList 渲染视频教程配置。 */
export default function VideoGuideList({ videos }: { videos: GuideVideo[] }) {
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
