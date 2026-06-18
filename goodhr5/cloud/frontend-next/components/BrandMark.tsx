/** 本文件负责展示 GoodHR 品牌标识。 */
import HubRoundedIcon from "@mui/icons-material/HubRounded";
import { Box, Typography } from "@mui/material";

/** BrandMark 输出可点击的 GoodHR 品牌标识。 */
export default function BrandMark() {
  return (
    <Box component="a" href="/" sx={{ display: "inline-flex", alignItems: "center", gap: 1.25 }}>
      <Box
        sx={{
          width: 38,
          height: 38,
          display: "grid",
          placeItems: "center",
          bgcolor: "primary.main",
          color: "primary.contrastText",
          borderRadius: "6px",
        }}
      >
        <HubRoundedIcon fontSize="small" />
      </Box>
      <Typography sx={{ fontSize: 21, fontWeight: 800, color: "text.primary" }}>GoodHR</Typography>
    </Box>
  );
}
