/** 本文件负责官网内页的统一页头、内容区和页脚结构。 */

import { Box, Container, Typography } from "@mui/material";
import type { ReactNode } from "react";
import SiteFooter from "./SiteFooter";
import SiteHeader from "./SiteHeader";

type MarketingShellProps = {
  eyebrow: string;
  title: string;
  description: string;
  children: ReactNode;
};

/** MarketingShell 输出官网内页统一布局。 */
export default function MarketingShell({ eyebrow, title, description, children }: MarketingShellProps) {
  return <Box sx={{ minHeight: "100vh", bgcolor: "background.default" }}>
    <SiteHeader />
    <Box component="main">
      <Box component="section" sx={{ pt: { xs: 8, md: 12 }, pb: { xs: 6, md: 8 } }}>
        <Container maxWidth="lg">
          <Typography sx={{ color: "primary.main", fontSize: 14, fontWeight: 800 }}>{eyebrow}</Typography>
          <Typography component="h1" sx={{ mt: 1.5, maxWidth: 880, color: "text.primary", fontSize: { xs: 42, md: 64 }, lineHeight: 1.1, fontWeight: 780 }}>{title}</Typography>
          <Typography sx={{ mt: 3, maxWidth: 760, color: "text.secondary", fontSize: { xs: 17, md: 19 }, lineHeight: 1.8 }}>{description}</Typography>
        </Container>
      </Box>
      {children}
    </Box>
    <SiteFooter />
  </Box>;
}
