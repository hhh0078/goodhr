/** 文件作用说明：统一简历库列表、详情和弹框的紧凑字号与控件高度，继承当前会员配色。 */
"use client";

import { ConfigProvider } from "antd";
import type { ReactNode } from "react";

/** ResumeLayout 为简历库路由提供局部排版配置，正文与弹框共用相同尺寸。 */
export default function ResumeLayout({ children }: { children: ReactNode }) {
  return <ConfigProvider theme={{ token: {
    fontSize: 13, fontSizeSM: 12, fontSizeLG: 14,
    fontSizeHeading3: 20, fontSizeHeading4: 16, fontSizeHeading5: 14,
    lineHeight: 1.5, controlHeight: 32, controlHeightLG: 36, controlHeightSM: 26,
  } }}>{children}</ConfigProvider>;
}
