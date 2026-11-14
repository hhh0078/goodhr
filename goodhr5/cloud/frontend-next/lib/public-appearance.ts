/** 本文件集中定义官网双配色、公开路由范围和独立的浏览器偏好，避免影响后台主题。 */

export type PublicAppearance = "green" | "rose";
export const PUBLIC_APPEARANCE_KEY = "goodhr_public_appearance_v1";

export const PUBLIC_APPEARANCES = {
  green: { name: "蜜桃绿", brand: "#349b26", accent: "#ffcb7a", action: "#28751f", paper: "#fffaf1", surface: "#fffdf8", soft: "#f0f3e3", ink: "#203526", muted: "#606858", border: "#e2e4d4" },
  rose: { name: "莓红奶油", brand: "#ff2742", accent: "#fff6f0", action: "#c51c36", paper: "#fff6f0", surface: "#fffcfa", soft: "#fce8e7", ink: "#49252d", muted: "#795d63", border: "#ecd9d5" },
} as const;

/** normalizePublicAppearance 将空值、旧值和损坏的存储值回退为蜜桃绿。 */
export function normalizePublicAppearance(value: unknown): PublicAppearance {
  return value === "rose" ? "rose" : "green";
}

/** isPublicAppearancePath 只让官网及登录页使用新样式，排除后台和私人资料页面。 */
export function isPublicAppearancePath(pathname: string | null): boolean {
  if (!pathname) return false;
  const path = pathname.replace(/\/$/, "") || "/";
  return ["/", "/features", "/pricing", "/videos", "/articles", "/download", "/contact", "/login", "/recruitment-automation", "/recruitment-auto-reply", "/hr-tools"].includes(path)
    || path.startsWith("/articles/") || /^\/platforms\/(boss|zhilian|liepin|liepin-headhunter|51job)$/.test(path);
}

/** createPublicAppearanceVariables 为官网正文及其浮层输出同一套局部颜色变量。 */
export function createPublicAppearanceVariables(appearance: PublicAppearance): string {
  const p = PUBLIC_APPEARANCES[normalizePublicAppearance(appearance)];
  return `.public-theme-root,.public-theme-portal{--public-brand:${p.brand};--public-accent:${p.accent};--public-action:${p.action};--public-paper:${p.paper};--public-surface:${p.surface};--public-soft:${p.soft};--public-ink:${p.ink};--public-muted:${p.muted};--public-border:${p.border};}
  .public-theme-root .public-marketing,.public-theme-root .public-login-1,.public-theme-portal{--goodhr-primary:${p.brand};--goodhr-primary-dark:${p.action};--goodhr-primary-soft:${p.soft};--goodhr-bg:${p.paper};--goodhr-text:${p.ink};--goodhr-text-secondary:${p.muted};--goodhr-border:${p.border};--goodhr-hover:${p.soft};}`;
}
