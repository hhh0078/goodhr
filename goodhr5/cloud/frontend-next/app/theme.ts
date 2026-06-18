/** 本文件负责定义 GoodHR 新版前端的 MUI 明亮主题。 */
"use client";

import { createTheme } from "@mui/material/styles";

export type ThemePreference = "green" | "rose" | "amber";

const accents: Record<ThemePreference, { main: string; dark: string; soft: string }> = {
  green: { main: "#159a62", dark: "#0f754a", soft: "#edf7f1" },
  rose: { main: "#b24b67", dark: "#87354d", soft: "#fbf0f3" },
  amber: { main: "#a86b12", dark: "#7d4e0b", soft: "#fbf5e9" },
};

/** createGoodHRTheme 根据用户选择生成统一浅色主题。 */
export function createGoodHRTheme(preference: ThemePreference = "green") {
  const accent = accents[preference] || accents.green;
  return createTheme({
  palette: {
    mode: "light",
    primary: { main: accent.main, dark: accent.dark, contrastText: "#ffffff" },
    secondary: { main: "#17211c" },
    background: { default: "#f6f9f7", paper: "#ffffff" },
    text: { primary: "#17211c", secondary: "#637069" },
    divider: "#dce5e0",
    success: { main: accent.main },
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
        root: {
          minHeight: 44,
          borderRadius: 999,
          boxShadow: "none",
          paddingInline: 20,
        },
      },
    },
    MuiPaper: {
      styleOverrides: { root: { backgroundImage: "none" } },
    },
    MuiTextField: {
      defaultProps: { variant: "outlined" },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: { minHeight: 56, borderRadius: 18 },
      },
    },
  },
  });
}

export default createGoodHRTheme();
