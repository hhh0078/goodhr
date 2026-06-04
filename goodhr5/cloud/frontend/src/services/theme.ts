/** GoodHR 后台主题选择与本地缓存管理。 */

export type ThemeID = "pine" | "mist" | "copper";

export type AppTheme = {
  id: ThemeID;
  name: string;
  summary: string;
  colors: string[];
};

export const THEME_CACHE_KEY = "goodhr5_admin_theme";

export const APP_THEMES: AppTheme[] = [
  {
    id: "pine",
    name: "松墨",
    summary: "低亮度绿色，保留终端感但不刺眼。",
    colors: ["#0c1110", "#111817", "#6fbf9b", "#c8d6d0"],
  },
  {
    id: "mist",
    name: "雾青",
    summary: "冷静的青灰色，适合长时间盯任务列表。",
    colors: ["#0d1014", "#121720", "#7fb7d6", "#c8d3dc"],
  },
  {
    id: "copper",
    name: "赤铜",
    summary: "暖色深灰，界面更柔和，有一点工作台质感。",
    colors: ["#120f0d", "#1a1511", "#d49b6a", "#d8cec1"],
  },
];

/**
 * 判断主题标识是否存在。
 * @param value - 待检查的主题标识。
 * @returns 是否为内置主题。
 */
export function isThemeID(value: string): value is ThemeID {
  return APP_THEMES.some((theme) => theme.id === value);
}

/**
 * 读取本地缓存的后台主题。
 * @returns 已缓存的主题标识；不存在或无效时返回空字符串。
 */
export function loadCachedTheme(): ThemeID | "" {
  const cached = localStorage.getItem(THEME_CACHE_KEY) || "";
  return isThemeID(cached) ? cached : "";
}

/**
 * 应用主题到文档根节点。
 * @param themeID - 主题标识。
 * @returns void。
 */
export function applyTheme(themeID: ThemeID): void {
  document.documentElement.dataset.theme = themeID;
}

/**
 * 缓存并应用主题。
 * @param themeID - 主题标识。
 * @returns void。
 */
export function saveTheme(themeID: ThemeID): void {
  localStorage.setItem(THEME_CACHE_KEY, themeID);
  applyTheme(themeID);
}
