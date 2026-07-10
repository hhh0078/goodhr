/** 本文件负责展示 GoodHR 品牌标识。 */
import { Box, Typography } from "@mui/material";

/** BrandMark 输出可点击的 GoodHR 品牌标识。 */
export default function BrandMark() {
  return (
    <Box component="a" href="/" sx={{ display: "inline-flex", alignItems: "center", gap: 1.25 }}>
      <Box
        component="img"
        src="/brand/goodhr-logo-transparent-512.png"
        alt="GoodHR"
        sx={{
          width: 38,
          height: 38,
          display: "block",
          objectFit: "contain",
          flexShrink: 0,
        }}
      />
      <Typography sx={{ fontSize: 21, fontWeight: 800, color: "text.primary" }}>GoodHR</Typography>
    </Box>
  );
}
