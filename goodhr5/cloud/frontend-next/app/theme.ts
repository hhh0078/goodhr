/** 本文件负责定义 GoodHR 新版前端的 MUI 明亮主题。 */
"use client";

import { createTheme } from "@mui/material/styles";

export type MembershipTheme = "free" | "plus" | "max";

type MembershipPalette = {
  main: string;
  dark: string;
  soft: string;
  secondary: string;
  background: string;
  textPrimary: string;
  textSecondary: string;
  divider: string;
  hover: string;
};

const membershipPalettes: Record<MembershipTheme, MembershipPalette> = {
  free: {
    main: "#159a62",
    dark: "#0f754a",
    soft: "#edf7f1",
    secondary: "#17211c",
    background: "#f6f9f7",
    textPrimary: "#17211c",
    textSecondary: "#637069",
    divider: "#dce5e0",
    hover: "#f2f7f4",
  },
  plus: {
    main: "#242424",
    dark: "#111111",
    soft: "#f1f1ef",
    secondary: "#4a4a46",
    background: "#f7f7f5",
    textPrimary: "#1b1b1a",
    textSecondary: "#686865",
    divider: "#e3e3df",
    hover: "#f4f4f1",
  },
  max: {
    main: "#8a6518",
    dark: "#674a10",
    soft: "#f8f3e6",
    secondary: "#1b1812",
    background: "#f8f7f2",
    textPrimary: "#1f1c16",
    textSecondary: "#6e685b",
    divider: "#e5dfd0",
    hover: "#f3eee1",
  },
};

/** resolveMembershipTheme 根据有效会员类型返回对应后台主题。 */
export function resolveMembershipTheme(
  active: boolean,
  memberType: unknown,
): MembershipTheme {
  if (!active) return "free";
  const normalized = String(memberType || "").trim().toLowerCase();
  return normalized === "plus" || normalized === "max" ? normalized : "free";
}

/** createGoodHRTheme 根据会员等级生成统一浅色主题。 */
export function createGoodHRTheme(membershipTheme: MembershipTheme = "free") {
  const accent = membershipPalettes[membershipTheme] || membershipPalettes.free;
  return createTheme({
    palette: {
      mode: "light",
      primary: {
        main: accent.main,
        dark: accent.dark,
        contrastText: "#ffffff",
      },
      secondary: { main: accent.secondary },
      background: { default: accent.background, paper: "#ffffff" },
      text: { primary: accent.textPrimary, secondary: accent.textSecondary },
      divider: accent.divider,
      action: { hover: accent.hover, selected: accent.soft },
      success: { main: "#238653" },
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
