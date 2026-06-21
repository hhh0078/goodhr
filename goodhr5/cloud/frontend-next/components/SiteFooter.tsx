/** 本文件负责展示新版官网底部信息。 */
import { Box, Container, Stack, Typography } from "@mui/material";
import Link from "next/link";
import BrandMark from "./BrandMark";

const footerLinks = [{ label: "功能介绍", href: "/features" }, { label: "产品定价", href: "/pricing" }, { label: "视频教程", href: "/videos" }, { label: "下载", href: "/download" }, { label: "联系我们", href: "/contact" }];

/** SiteFooter 输出品牌、联系信息和版权说明。 */
export default function SiteFooter() {
  return (
    <Box component="footer" sx={{ py: 5, borderTop: "1px solid", borderColor: "divider", bgcolor: "#ffffff" }}>
      <Container maxWidth="lg">
        <Stack direction={{ xs: "column", md: "row" }} spacing={2} sx={{ alignItems: { md: "center" }, justifyContent: "space-between" }}>
          <Box><BrandMark /><Typography sx={{ mt: 1, color: "text.secondary", fontSize: 13 }}>AI筛选、自动打招呼、自动回复和招聘简历管理工具</Typography></Box>
          <Stack component="nav" direction="row" spacing={2} sx={{ flexWrap: "wrap", rowGap: 1 }}>{footerLinks.map((item) => <Link key={item.href} href={item.href} style={{ color: "inherit", textDecoration: "none" }}><Typography sx={{ color: "text.secondary", fontSize: 13, "&:hover": { color: "primary.main" } }}>{item.label}</Typography></Link>)}</Stack>
          <Typography sx={{ color: "text.secondary", fontSize: 14 }}>联系：17607080935</Typography>
        </Stack>
      </Container>
    </Box>
  );
}
