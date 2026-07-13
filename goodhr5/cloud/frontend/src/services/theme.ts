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
    name: "淡粉",
    summary: "柔和浅粉色，界面更轻盈，适合喜欢温柔风格的用户。",
    colors: ["#fff1f6", "#ffffff", "#e86f9a", "#5f3345"],
  },
  {
    id: "copper",
    name: "奶茶",
    summary: "浅奶茶色，温暖但不厚重，适合白天办公。",
    colors: ["#fbf0e2", "#fffaf3", "#b87a45", "#4b3423"],
  },
  {
    id: "paper",
    name: "纸白",
    summary: "温和浅色，适合白天办公和投屏演示。",
    colors: ["#f7f4ee", "#fffdf8", "#3f7f68", "#24322c"],
  },
  {
    id: "morning",
    name: "海盐",
    summary: "浅青绿色，清爽干净，和淡粉、奶茶明显不同。",
    colors: ["#edf9f7", "#ffffff", "#2f9b91", "#243d3b"],
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
