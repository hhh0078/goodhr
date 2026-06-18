/** 本文件负责展示新版官网底部信息。 */
import { Box, Container, Stack, Typography } from "@mui/material";
import BrandMark from "./BrandMark";

/** SiteFooter 输出品牌、联系信息和版权说明。 */
export default function SiteFooter() {
  return (
    <Box component="footer" sx={{ py: 5, borderTop: "1px solid", borderColor: "divider", bgcolor: "#ffffff" }}>
      <Container maxWidth="lg">
        <Stack direction={{ xs: "column", sm: "row" }} spacing={2} sx={{ alignItems: { sm: "center" }, justifyContent: "space-between" }}>
          <BrandMark />
          <Typography sx={{ color: "text.secondary", fontSize: 14 }}>联系：17607080935 · GoodHR 招聘自动化工具</Typography>
        </Stack>
      </Container>
    </Box>
  );
}
