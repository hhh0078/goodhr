// 文件作用说明：只加载 Go 准备好的本地扩展，关闭 Camoufox 默认扩展联网下载并记录无效路径。
import fs from "node:fs/promises";
import path from "node:path";
import type { LaunchOptions } from "camoufox-js";
import type { ActionContext } from "../../contracts/common.js";
import { WorkerError } from "../../errors/worker-error.js";

/** offlineAddonOptions 校验扩展清单并生成离线启动配置，不改变调用方的扩展数组。 */
export async function offlineAddonOptions(
  extensionPaths: string[] | undefined,
  context: ActionContext,
): Promise<Pick<LaunchOptions, "addons" | "exclude_addons">> {
  const bundledPath = process.env.GOODHR_UBO_EXTENSION_PATH?.trim();
  if (!bundledPath || !path.isAbsolute(bundledPath)) {
    throw new WorkerError({
      code: "BROWSER_ADDON_INVALID",
      message: "内置扩展路径没准备好，请重启 GoodHR 再试一次",
      action: context.action,
      step: "prepare_addons",
      trace_id: context.trace_id,
      retryable: true,
    });
  }
  const addons = [...new Set([...(extensionPaths ?? []), bundledPath])];
  for (const addonPath of addons) {
    try {
      const manifest: unknown = JSON.parse(
        await fs.readFile(path.join(addonPath, "manifest.json"), "utf8"),
      );
      if (
        !manifest || typeof manifest !== "object" ||
        !("manifest_version" in manifest) ||
        (manifest.manifest_version !== 2 && manifest.manifest_version !== 3) ||
        !("name" in manifest) || typeof manifest.name !== "string" || !manifest.name.trim() ||
        !("version" in manifest) || typeof manifest.version !== "string" || !manifest.version.trim()
      ) {
        throw new Error("扩展 manifest.json 内容不完整");
      }
    } catch (error) {
      throw new WorkerError({
        code: "BROWSER_ADDON_INVALID",
        message: "浏览器扩展文件不完整，请重启 GoodHR 后再试",
        action: context.action,
        step: "prepare_addons",
        trace_id: context.trace_id,
        retryable: true,
        details: {
          addon_path: addonPath,
          bundled: addonPath === bundledPath,
          original_error: error instanceof Error ? error.message : String(error),
        },
        cause: error,
      });
    }
  }
  return { addons, exclude_addons: ["UBO"] };
}
