/** 本文件负责提供 Ant Design 中文配置、消息上下文和按账号保存的用户主题。 */
"use client";

import { App, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { usePathname } from "next/navigation";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  createGoodHRTheme,
  createGoodHRAdminTheme,
  createGoodHRVariables,
  normalizeThemePreset,
  themePreferenceKey,
  type ThemePreset,
} from "./theme";
import { PUBLIC_APPEARANCE_KEY, PUBLIC_APPEARANCES, createPublicAppearanceVariables, isPublicAppearancePath, normalizePublicAppearance, type PublicAppearance } from "@/lib/public-appearance";

type ProvidersProps = { children: ReactNode };

const UserThemeContext = createContext<{
  themePreset: ThemePreset;
  setThemePreset: (value: ThemePreset) => boolean;
  setThemeAccount: (account: string) => void;
}>({ themePreset: "green", setThemePreset: () => false, setThemeAccount: () => undefined });

const PublicAppearanceContext = createContext<{ appearance: PublicAppearance; setAppearance: (value: PublicAppearance) => void }>({ appearance: "green", setAppearance: () => undefined });

/** usePublicAppearance 返回官网专用配色，不读取或修改后台账号主题。 */
export function usePublicAppearance() {
  return useContext(PublicAppearanceContext);
}

/** useUserTheme 返回用户主题、主动选择方法和账号加载方法。 */
export function useUserTheme() {
  return useContext(UserThemeContext);
}

/** Providers 分别管理后台账号主题和官网双配色，保持正文与浮层一致。 */
export default function Providers({ children }: ProvidersProps) {
  const pathname = usePathname();
  const accountKeyRef = useRef("");
  const [themePreset, setThemePresetState] = useState<ThemePreset>("green");
  const [appearance, setAppearanceState] = useState<PublicAppearance>("green");
  const isAdminPage = pathname === "/admin" || Boolean(pathname?.startsWith("/admin/"));
  const isPublicSite = isPublicAppearancePath(pathname);
  const activeTheme = isAdminPage ? themePreset : "green";
  const theme = useMemo(
    () => {
      if (isAdminPage) return createGoodHRAdminTheme(activeTheme);
      const base = createGoodHRTheme(activeTheme);
      if (!isPublicSite) return base;
      const palette = PUBLIC_APPEARANCES[appearance];
      return { ...base, token: { ...base.token, colorPrimary: palette.action, colorInfo: palette.action, colorText: palette.ink, colorTextSecondary: palette.muted, colorBorder: palette.border, colorBgContainer: palette.surface } };
    },
    [activeTheme, isAdminPage, isPublicSite, appearance],
  );

  // 官网偏好独立保存；禁用存储时仍能切换，不阻断导航。
  useLayoutEffect(() => {
    try { setAppearanceState(normalizePublicAppearance(window.localStorage.getItem(PUBLIC_APPEARANCE_KEY))); } catch { /* 存储不可用时使用默认配色。 */ }
  }, []);

  /** setAppearance 立即切换官网颜色，并尽力保存浏览器偏好。 */
  const setAppearance = useCallback((value: PublicAppearance) => {
    const next = normalizePublicAppearance(value);
    setAppearanceState(next);
    try { window.localStorage.setItem(PUBLIC_APPEARANCE_KEY, next); } catch { /* 本次切换仍然有效。 */ }
  }, []);

  /** setThemeAccount 只在账号变化时读取偏好，会员刷新不会覆盖正在使用的颜色。 */
  const setThemeAccount = useCallback((account: string) => {
    const key = themePreferenceKey(account);
    if (accountKeyRef.current === key) return;
    accountKeyRef.current = key;
    let saved: ThemePreset = "green";
    if (key) {
      try {
        saved = normalizeThemePreset(window.localStorage.getItem(key));
      } catch {
        // 浏览器禁用存储时仍可正常使用默认绿色。
      }
    }
    setThemePresetState(saved);
  }, []);

  /** setThemePreset 立即应用主动选择，并尝试为当前账号保存到浏览器。 */
  const setThemePreset = useCallback((value: ThemePreset) => {
    if (!accountKeyRef.current) return false;
    const next = normalizeThemePreset(value);
    setThemePresetState(next);
    try {
      window.localStorage.setItem(accountKeyRef.current, next);
      return true;
    } catch {
      return false;
    }
  }, []);

  useEffect(() => {
    /** syncThemePreference 同步其它标签页对同账号的主题修改或清空操作。 */
    function syncThemePreference(event: StorageEvent) {
      if (event.key === PUBLIC_APPEARANCE_KEY || event.key === null) setAppearanceState(normalizePublicAppearance(event.newValue));
      if (accountKeyRef.current && (event.key === accountKeyRef.current || event.key === null)) {
        setThemePresetState(normalizeThemePreset(event.newValue));
      }
    }
    window.addEventListener("storage", syncThemePreference);
    return () => window.removeEventListener("storage", syncThemePreference);
  }, []);

  return (
    <ConfigProvider
      locale={zhCN}
      theme={theme}
      button={{ autoInsertSpace: false }}
      select={{
        showSearch: { optionFilterProp: ["label", "children", "value"] },
      }}
      pagination={isAdminPage ? { className: "goodhr-admin-pagination" } : undefined}
    >
      <UserThemeContext.Provider
        value={{ themePreset, setThemePreset, setThemeAccount }}
      >
        <PublicAppearanceContext.Provider value={{ appearance, setAppearance }}>
          <style>{createGoodHRVariables(activeTheme) + (isPublicSite ? createPublicAppearanceVariables(appearance) : "")}</style>
          <App className={isPublicSite ? `public-theme-root public-theme-${appearance}` : undefined}>{children}</App>
        </PublicAppearanceContext.Provider>
      </UserThemeContext.Provider>
    </ConfigProvider>
  );
}
