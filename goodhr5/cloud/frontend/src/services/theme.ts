/** GoodHR 后台主题选择与本地缓存管理。 */

export type ThemeID = "pine" | "mist" | "copper" | "paper" | "morning";

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
    summary: "深墨绿色，保留终端感，适合夜间盯任务。",
    colors: ["#07130f", "#0d1f18", "#66d19e", "#d7efe2"],
  },
  {
    id: "mist",
    name: "雾青",
    summary: "钢青深灰，更冷静，和绿色主题明显区分。",
    colors: ["#0a1118", "#101b24", "#7cc7c0", "#d2e5e8"],
  },
  {
    id: "copper",
    name: "赤铜",
    summary: "暖棕铜色，界面更像工作台，视觉更温和。",
    colors: ["#170d08", "#23150d", "#e39b5f", "#f0d1b8"],
  },
  {
    id: "paper",
    name: "纸白",
    summary: "温和浅色，适合白天办公和投屏演示。",
    colors: ["#f7f4ee", "#fffdf8", "#3f7f68", "#24322c"],
  },
  {
    id: "morning",
    name: "岩灰",
    summary: "中性石墨灰，克制耐看，不和浅色主题重复。",
    colors: ["#101112", "#1b1d1f", "#d1b15f", "#e2e0d8"],
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
