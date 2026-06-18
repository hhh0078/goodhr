/** 本文件负责定义 GoodHR 新版前端的 MUI 明亮主题。 */
"use client";

import { createTheme } from "@mui/material/styles";

const theme = createTheme({
  palette: {
    mode: "light",
    primary: { main: "#159a62", dark: "#0f754a", contrastText: "#ffffff" },
    secondary: { main: "#17211c" },
    background: { default: "#f6f9f7", paper: "#ffffff" },
    text: { primary: "#17211c", secondary: "#637069" },
    divider: "#dce5e0",
    success: { main: "#159a62" },
    warning: { main: "#c47a1a" },
    error: { main: "#c83f49" },
  },
  shape: { borderRadius: 8 },
  typography: {
    fontFamily:
      'Inter, "SF Pro Display", "PingFang SC", "Microsoft YaHei", Arial, sans-serif',
    button: { textTransform: "none", fontWeight: 700, letterSpacing: 0 },
    h1: { fontWeight: 760, letterSpacing: 0 },
    h2: { fontWeight: 720, letterSpacing: 0 },
    h3: { fontWeight: 700, letterSpacing: 0 },
  },
  components: {
    MuiButton: {
      styleOverrides: {
        root: { minHeight: 44, borderRadius: 6, boxShadow: "none" },
      },
    },
    MuiPaper: {
      styleOverrides: { root: { backgroundImage: "none" } },
    },
    MuiTextField: {
      defaultProps: { variant: "outlined" },
    },
  },
});

export default theme;
