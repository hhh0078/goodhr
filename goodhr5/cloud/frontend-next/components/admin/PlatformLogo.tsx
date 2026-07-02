/** 本文件负责在新版后台中统一展示招聘平台本地图标。 */
"use client";

import { Box } from "@mui/material";

export const PLATFORM_ICON_SRC: Record<string, string> = {
  boss: "/assets/platforms/boss.png",
  hliepin: "/assets/platforms/liepin.ico",
  liepin: "/assets/platforms/liepin.ico",
  zhaopin: "",
};

/** platformLabel 返回招聘平台中文名称。 */
export function platformLabel(platformID: string) {
  return platformID === "boss"
    ? "Boss直聘"
    : platformID === "hliepin"
      ? "猎聘猎头端"
    : platformID === "zhaopin"
      ? "智联招聘"
      : platformID === "liepin"
        ? "猎聘"
        : platformID || "未知平台";
}

/** platformIconSrc 返回招聘平台本地图标路径。 */
export function platformIconSrc(platformID: string) {
  return PLATFORM_ICON_SRC[String(platformID || "").toLowerCase()] || "";
}

/** PlatformLogo 渲染招聘平台图标，不存在图标时显示平台首字。 */
export default function PlatformLogo({
  platformID,
  size = 34,
}: {
  platformID: string;
  size?: number;
}) {
  const src = platformIconSrc(platformID);
  const label = platformLabel(platformID);
  return (
    <Box
      aria-label={label}
      component={src ? "img" : "span"}
      src={src || undefined}
      sx={{
        width: size,
        height: size,
        borderRadius: "8px",
        objectFit: "contain",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        bgcolor: src ? "transparent" : "#f4f7f5",
        color: "text.secondary",
        fontSize: Math.max(12, Math.round(size * 0.42)),
        fontWeight: 800,
        flex: "0 0 auto",
      }}
    >
      {src ? null : label.slice(0, 1)}
    </Box>
  );
}
