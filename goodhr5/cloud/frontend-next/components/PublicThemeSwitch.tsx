/** 本文件提供官网两套配色的无障碍切换按钮，不增加页面宣传文案。 */
"use client";

import { usePublicAppearance } from "@/app/providers";
import { PUBLIC_APPEARANCES, type PublicAppearance } from "@/lib/public-appearance";

/** PublicThemeSwitch 通过两个配色样本切换主题，并让键盘和读屏可辨认选中项。 */
export default function PublicThemeSwitch() {
  const { appearance, setAppearance } = usePublicAppearance();
  return <div className="public-theme-switch" role="group" aria-label="官网配色">
    {(Object.keys(PUBLIC_APPEARANCES) as PublicAppearance[]).map((value) => {
      const palette = PUBLIC_APPEARANCES[value];
      return <button key={value} type="button" className="public-theme-choice"
        aria-label={`切换为${palette.name}配色`} title={palette.name}
        aria-pressed={appearance === value} onClick={() => setAppearance(value)}>
        <span className="public-theme-swatch" aria-hidden="true">
          <span style={{ backgroundColor: palette.brand }} />
          <span style={{ backgroundColor: palette.accent }} />
        </span>
      </button>;
    })}
  </div>;
}
