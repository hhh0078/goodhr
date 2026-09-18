// 文件作用说明：清除 Camoufox 指纹中的窗口与页面宽高覆盖，让浏览器自行管理窗口大小。

import type { LaunchOptions } from "playwright-core";

const dimensionKeys = [
  "window.outerWidth", "window.outerHeight",
  "window.innerWidth", "window.innerHeight",
  "document.body.clientWidth", "document.body.clientHeight",
] as const;

/** useAutomaticWindowSize 重组 Camoufox 分段配置，只移除窗口及页面宽高，保留其他指纹、插件和环境变量。 */
export function useAutomaticWindowSize(options: LaunchOptions): LaunchOptions {
  if (!options.env) return options;
  const chunks = Object.keys(options.env)
    .filter((key) => /^CAMOU_CONFIG_[1-9]\d*$/.test(key))
    .sort((left, right) => Number(left.slice(13)) - Number(right.slice(13)));
  if (chunks.length === 0) return options;
  if (chunks.some((key, index) => key !== `CAMOU_CONFIG_${index + 1}`)) {
    throw new Error("浏览器启动配置没读完整，可以重新打开浏览器");
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(chunks.map((key) => options.env?.[key] ?? "").join(""));
  } catch {
    throw new Error("浏览器启动配置没读完整，可以重新打开浏览器");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("浏览器启动配置格式不对，可以重新打开浏览器");
  }
  const config = parsed as Record<string, unknown>;
  if (!dimensionKeys.some((key) => key in config)) return options;
  for (const key of dimensionKeys) delete config[key];
  const env = { ...options.env };
  for (const key of chunks) delete env[key];
  // 使用与当前 camoufox-js 相同的分段上限，Windows 单段不超过 2047 字符。
  const chunkSize = process.platform === "win32" ? 2047 : 32767;
  // 转义非 ASCII 字符，避免插件路径等 Unicode 内容在分段边界被截断。
  const serialized = JSON.stringify(config).replace(/[\u007f-\uffff]/g,
    (character) => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`);
  for (let start = 0; start < serialized.length; start += chunkSize) {
    env[`CAMOU_CONFIG_${Math.floor(start / chunkSize) + 1}`] = serialized.slice(start, start + chunkSize);
  }
  return { ...options, env };
}
