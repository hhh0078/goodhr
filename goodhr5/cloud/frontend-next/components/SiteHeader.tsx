/** 本文件负责官网顶部导航和移动端菜单。 */
"use client";

import CloseRoundedIcon from "@mui/icons-material/CloseRounded";
import MenuRoundedIcon from "@mui/icons-material/MenuRounded";
import { Box, Button, Container, Drawer, IconButton, Paper, Stack } from "@mui/material";
import Link from "next/link";
import { useState } from "react";
import BrandMark from "./BrandMark";

const navItems = [
  { label: "首页", href: "/" },
  { label: "功能介绍", href: "/features.html" },
  { label: "产品定价", href: "/pricing.html" },
  { label: "视频教程", href: "/videos.html" },
  { label: "联系我们", href: "/contact.html" },
];

/** SiteHeader 输出桌面导航和移动端抽屉菜单。 */
export default function SiteHeader() {
  const [open, setOpen] = useState(false);

  /** closeMenu 关闭移动端导航。 */
  function closeMenu() {
    setOpen(false);
  }

  return (
    <Box component="header" sx={{ position: "relative", zIndex: 10, pt: { xs: 1.5, md: 2.5 } }}>
      <Container maxWidth="lg">
        <Paper
          variant="outlined"
          sx={{
            minHeight: 72,
            px: { xs: 2, md: 2.5 },
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            borderColor: "divider",
            boxShadow: "0 16px 48px rgba(31, 55, 43, 0.08)",
          }}
        >
          <BrandMark />
          <Stack direction="row" spacing={0.5} sx={{ display: { xs: "none", md: "flex" }, alignItems: "center" }}>
            {navItems.map((item) => (
              <Button key={item.href} component={Link} href={item.href} color="secondary" sx={{ px: 1.5 }}>
                {item.label}
              </Button>
            ))}
          </Stack>
          <Button component={Link} href="/login" variant="contained" sx={{ display: { xs: "none", md: "inline-flex" }, px: 2.5 }}>
            进入控制台
          </Button>
          <IconButton aria-label="打开导航菜单" onClick={() => setOpen(true)} sx={{ display: { md: "none" } }}>
            <MenuRoundedIcon />
          </IconButton>
        </Paper>
      </Container>
      <Drawer anchor="right" open={open} onClose={closeMenu}>
        <Box sx={{ width: 288, p: 2.5 }}>
          <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", mb: 3 }}>
            <BrandMark />
            <IconButton aria-label="关闭导航菜单" onClick={closeMenu}>
              <CloseRoundedIcon />
            </IconButton>
          </Box>
          <Stack spacing={1}>
            {navItems.map((item) => (
              <Button key={item.href} component={Link} href={item.href} onClick={closeMenu} color="secondary" fullWidth sx={{ justifyContent: "flex-start" }}>
                {item.label}
              </Button>
            ))}
            <Button component={Link} href="/login" onClick={closeMenu} variant="contained" fullWidth>
              进入控制台
            </Button>
          </Stack>
        </Box>
      </Drawer>
    </Box>
  );
}
