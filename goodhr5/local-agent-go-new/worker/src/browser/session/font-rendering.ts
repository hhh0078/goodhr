// 文件作用说明：为 macOS 补齐中文字体回退，并让启动偏好与浏览器资源的 GPU 绘制修正保持一致。
import type { LaunchOptions } from "playwright-core";

/** useMacOSFontRendering 合并 macOS 中文回退与绘制偏好，保留其他启动配置且不修改传入对象。 */
export function useMacOSFontRendering(
  options: LaunchOptions,
  platform: string = process.platform,
): LaunchOptions {
  if (platform !== "darwin") return options;
  // GPU 启动偏好由 Go 提前写入 camoufox.cfg；这里保持连接后的偏好一致。
  return {
    ...options,
    firefoxUserPrefs: {
      ...options.firefoxUserPrefs,
      "layers.gpu-process.enabled": false,
      "layers.gpu-process.force-enabled": false,
      // 默认 OS 字体可能无法完成中文回退，使用两架构组件已内置的 Noto Sans SC。
      "font.name.sans-serif.zh-CN": "Noto Sans SC",
      "font.name.serif.zh-CN": "Noto Sans SC",
      "font.name.monospace.zh-CN": "Noto Sans SC",
    },
  };
}
