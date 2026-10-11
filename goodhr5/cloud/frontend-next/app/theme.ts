/** 本文件负责定义 GoodHR 可自由选择的浅色主题、账号偏好键和通用颜色变量。 */
"use client";

import type { ThemeConfig } from "antd";

export type ThemePreset = "green" | "ink" | "gold";

type ThemePalette = {
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

const themePalettes: Record<ThemePreset, ThemePalette> = {
  green: {
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
  ink: {
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
  gold: {
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

/** normalizeThemePreset 校验用户主题，未选择或损坏的偏好统一回到松绿色。 */
export function normalizeThemePreset(value: unknown): ThemePreset {
  return value === "ink" || value === "gold" ? value : "green";
}

/** themePreferenceKey 生成账号独立的浏览器保存键，匿名状态不保存主题。 */
export function themePreferenceKey(account: string): string {
  const identity = account.trim().toLowerCase();
  return identity ? `goodhr_admin_theme_v1:${encodeURIComponent(identity)}` : "";
}

/** createGoodHRTheme 根据用户选择生成统一浅色主题，默认使用松绿色。 */
export function createGoodHRTheme(
  themePreset: ThemePreset = "green",
): ThemeConfig {
  const accent = themePalettes[normalizeThemePreset(themePreset)];
  return {
    token: {
      colorPrimary: accent.main,
      colorInfo: accent.main,
      colorSuccess: "#238653",
      colorWarning: "#c47a1a",
      colorError: "#c83f49",
      colorBgLayout: accent.background,
      colorBgContainer: "#ffffff",
      colorText: accent.textPrimary,
      colorTextSecondary: accent.textSecondary,
      colorBorder: accent.divider,
      colorBorderSecondary: accent.divider,
      borderRadius: 8,
      controlHeight: 38,
      controlHeightLG: 44,
      controlHeightSM: 30,
      fontFamily:
        'Inter, "SF Pro Display", "PingFang SC", "Microsoft YaHei", Arial, sans-serif',
    },
    components: {
      Button: {
        fontWeight: 600,
        primaryShadow: "none",
        borderRadius: 999,
        borderRadiusLG: 999,
        borderRadiusSM: 999,
      },
      Card: { borderRadiusLG: 8 },
      Modal: { borderRadiusLG: 8 },
      Menu: { itemSelectedBg: accent.soft, itemSelectedColor: accent.main },
    },
  };
}

/** createGoodHRVariables 生成覆盖浮层和正文的全局主题变量。 */
export function createGoodHRVariables(
  themePreset: ThemePreset = "green",
) {
  const accent = themePalettes[normalizeThemePreset(themePreset)];
  return `:root{--goodhr-primary:${accent.main};--goodhr-primary-dark:${accent.dark};--goodhr-primary-soft:${accent.soft};--goodhr-bg:${accent.background};--goodhr-background:${accent.background};--goodhr-text:${accent.textPrimary};--goodhr-text-secondary:${accent.textSecondary};--goodhr-muted:${accent.textSecondary};--goodhr-border:${accent.divider};--goodhr-divider:${accent.divider};--goodhr-hover:${accent.hover};}`;
}

export default createGoodHRTheme();
