/** 本文件提供可点击放大的通用图片预览组件。 */
"use client";

import CloseRoundedIcon from "@mui/icons-material/CloseRounded";
import ZoomInRoundedIcon from "@mui/icons-material/ZoomInRounded";
import { Box, Dialog, IconButton, Typography } from "@mui/material";
import { useState } from "react";

type ClickableImagePreviewProps = {
  src: string;
  alt: string;
  hint?: string;
};

/** ClickableImagePreview 展示图片缩略图，并在点击后打开大图预览。 */
export default function ClickableImagePreview({
  src,
  alt,
  hint = "点击图片放大查看",
}: ClickableImagePreviewProps) {
  const [previewOpen, setPreviewOpen] = useState(false);

  return (
    <>
      <Box>
        <Box
          component="button"
          type="button"
          aria-label={`放大查看：${alt}`}
          onClick={() => setPreviewOpen(true)}
          sx={{
            position: "relative",
            display: "block",
            width: "100%",
            p: 0,
            overflow: "hidden",
            border: "1px solid",
            borderColor: "divider",
            borderRadius: "8px",
            bgcolor: "action.hover",
            cursor: "zoom-in",
            lineHeight: 0,
            "&:focus-visible": {
              outline: "3px solid",
              outlineColor: "primary.light",
              outlineOffset: 2,
            },
          }}
        >
          <Box
            component="img"
            src={src}
            alt={alt}
            sx={{
              display: "block",
              width: "100%",
              maxHeight: 300,
              objectFit: "contain",
            }}
          />
          <Box
            sx={{
              position: "absolute",
              right: 10,
              bottom: 10,
              display: "flex",
              alignItems: "center",
              gap: 0.5,
              px: 1,
              py: 0.75,
              borderRadius: "6px",
              bgcolor: "rgba(20, 20, 20, .78)",
              color: "#fff",
              fontSize: 12,
              lineHeight: 1,
            }}
          >
            <ZoomInRoundedIcon sx={{ fontSize: 17 }} />
            放大查看
          </Box>
        </Box>
        <Typography sx={{ mt: 0.75, color: "text.secondary", fontSize: 12 }}>
          {hint}
        </Typography>
      </Box>

      <Dialog
        open={previewOpen}
        onClose={() => setPreviewOpen(false)}
        fullScreen
        slotProps={{
          paper: {
            sx: {
              bgcolor: "rgba(14, 14, 14, .94)",
            },
          },
        }}
      >
        <IconButton
          aria-label="关闭大图预览"
          onClick={() => setPreviewOpen(false)}
          sx={{
            position: "fixed",
            zIndex: 1,
            top: 16,
            right: 16,
            color: "#fff",
            bgcolor: "rgba(255, 255, 255, .12)",
            "&:hover": { bgcolor: "rgba(255, 255, 255, .2)" },
          }}
        >
          <CloseRoundedIcon />
        </IconButton>
        <Box
          role="button"
          tabIndex={0}
          aria-label="关闭大图预览"
          onClick={() => setPreviewOpen(false)}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              setPreviewOpen(false);
            }
          }}
          sx={{
            display: "flex",
            width: "100%",
            height: "100%",
            p: { xs: 2, sm: 5 },
            alignItems: "center",
            justifyContent: "center",
            cursor: "zoom-out",
          }}
        >
          <Box
            component="img"
            src={src}
            alt={alt}
            onClick={(event) => event.stopPropagation()}
            sx={{
              display: "block",
              maxWidth: "100%",
              maxHeight: "100%",
              objectFit: "contain",
              borderRadius: "8px",
              boxShadow: "0 20px 60px rgba(0, 0, 0, .35)",
              cursor: "default",
            }}
          />
        </Box>
      </Dialog>
    </>
  );
}
