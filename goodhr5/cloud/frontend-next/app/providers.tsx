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
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  createGoodHRTheme,
  createGoodHRVariables,
  normalizeThemePreset,
  themePreferenceKey,
  type ThemePreset,
} from "./theme";

type ProvidersProps = { children: ReactNode };

const UserThemeContext = createContext<{
  themePreset: ThemePreset;
  setThemePreset: (value: ThemePreset) => boolean;
  setThemeAccount: (account: string) => void;
}>({ themePreset: "green", setThemePreset: () => false, setThemeAccount: () => undefined });

/** useUserTheme 返回用户主题、主动选择方法和账号加载方法。 */
export function useUserTheme() {
  return useContext(UserThemeContext);
}

/** Providers 提供正文与浮层一致的用户主题，公开页面始终使用默认绿色。 */
export default function Providers({ children }: ProvidersProps) {
  const pathname = usePathname();
  const accountKeyRef = useRef("");
  const [themePreset, setThemePresetState] = useState<ThemePreset>("green");
  const activeTheme = pathname === "/admin" || pathname?.startsWith("/admin/")
    ? themePreset
    : "green";
  const theme = useMemo(
    () => createGoodHRTheme(activeTheme),
    [activeTheme],
  );

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
    >
      <UserThemeContext.Provider
        value={{ themePreset, setThemePreset, setThemeAccount }}
      >
        <style>{createGoodHRVariables(activeTheme)}</style>
        <App>{children}</App>
      </UserThemeContext.Provider>
    </ConfigProvider>
  );
}
