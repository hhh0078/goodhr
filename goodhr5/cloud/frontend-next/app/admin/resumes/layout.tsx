/** 文件作用说明：简历库继承后台共享主题，保持路由布局入口且不再重复定义控件尺寸。 */
import type { ReactNode } from "react";

/** ResumeLayout 直接复用所有后台路由共同的紧凑排版配置。 */
export default function ResumeLayout({ children }: { children: ReactNode }) {
  return children;
}
