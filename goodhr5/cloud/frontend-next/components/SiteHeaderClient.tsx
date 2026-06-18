/** 本文件负责官网顶部导航和移动端菜单的客户端交互。 */
"use client";

import CloseRoundedIcon from "@mui/icons-material/CloseRounded";
import MenuRoundedIcon from "@mui/icons-material/MenuRounded";
import { Box, Button, Container, Drawer, IconButton, Paper, Stack } from "@mui/material";
import Link from "next/link";
import { useState } from "react";
import type { PublicStatsData } from "@/lib/public-data";
import BrandMark from "./BrandMark";
import PublicStats from "./PublicStats";

const navItems = [
  { label: "首页", href: "/" },
  { label: "功能介绍", href: "/features" },
  { label: "产品定价", href: "/pricing" },
  { label: "视频教程", href: "/videos" },
  { label: "下载", href: "/download" },
  { label: "联系我们", href: "/contact" },
];

/** SiteHeaderClient 输出带交互的桌面导航和移动端菜单。 */
export default function SiteHeaderClient({ stats }: { stats: PublicStatsData }) {
  const [open, setOpen] = useState(false);

  /** closeMenu 关闭移动端导航。 */
  function closeMenu() {
    setOpen(false);
  }

  return (
    <Box component="header" sx={{ position: "relative", zIndex: 10, pt: { xs: 1.5, md: 2.5 } }}>
      <Container maxWidth="lg">
        <Paper variant="outlined" sx={{ minHeight: 72, px: { xs: 2, md: 2.5 }, display: "flex", borderRadius: "999px", alignItems: "center", justifyContent: "space-between", borderColor: "divider", boxShadow: "0 16px 48px rgba(31, 55, 43, 0.08)" }}>
          <BrandMark />
          <Stack direction="row" spacing={0.25} sx={{ display: { xs: "none", md: "flex" }, alignItems: "center" }}>
            {navItems.map((item) => <Button key={item.href} component={Link} href={item.href} color="secondary" sx={{ minWidth: 0, px: 1.25, "&:hover": { bgcolor: "#edf5f0" } }}>{item.label}</Button>)}
          </Stack>
          <Stack direction="row" spacing={1.5} sx={{ display: { xs: "none", xl: "flex" }, alignItems: "center" }}>
            <PublicStats stats={stats} compact />
            <Button component={Link} href="/login" variant="contained">进入控制台</Button>
          </Stack>
          <Button component={Link} href="/login" variant="contained" sx={{ display: { xs: "none", md: "inline-flex", xl: "none" } }}>进入控制台</Button>
          <IconButton aria-label="打开导航菜单" onClick={() => setOpen(true)} sx={{ display: { md: "none" } }}><MenuRoundedIcon /></IconButton>
        </Paper>
      </Container>
      <Drawer anchor="right" open={open} onClose={closeMenu}>
        <Box sx={{ width: 288, p: 2.5 }}>
          <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", mb: 3 }}><BrandMark /><IconButton aria-label="关闭导航菜单" onClick={closeMenu}><CloseRoundedIcon /></IconButton></Box>
          <Stack spacing={1}>
            <Box sx={{ px: 1, pb: 1.5 }}><PublicStats stats={stats} compact /></Box>
            {navItems.map((item) => <Button key={item.href} component={Link} href={item.href} onClick={closeMenu} color="secondary" fullWidth sx={{ justifyContent: "flex-start" }}>{item.label}</Button>)}
            <Button component={Link} href="/login" onClick={closeMenu} variant="contained" fullWidth>进入控制台</Button>
          </Stack>
        </Box>
      </Drawer>
    </Box>
  );
}
