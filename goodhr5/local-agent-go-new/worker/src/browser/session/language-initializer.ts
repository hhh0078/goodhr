// 文件作用说明：在首次显示浏览器前完成中文语言包注册，避免第一次英文、第二次中文。

import fs from "node:fs/promises";
import path from "node:path";
import { firefox, type LaunchOptions } from "playwright-core";

const languagePackID = "langpack-zh-CN@firefox.mozilla.org";

/** hasRegisteredChineseLanguagePack 检查当前配置是否已启用中文语言包。 */
export async function hasRegisteredChineseLanguagePack(profileDir: string): Promise<boolean> {
  try {
    const registry: unknown = JSON.parse(await fs.readFile(path.join(profileDir, "extensions.json"), "utf8"));
    if (!registry || typeof registry !== "object" || !("addons" in registry) || !Array.isArray(registry.addons)) return false;
    return registry.addons.some((addon: unknown) => {
      if (!addon || typeof addon !== "object") return false;
      return "id" in addon && addon.id === languagePackID && "active" in addon && addon.active === true;
    });
  } catch {
    return false;
  }
}

/** initializeChineseLanguagePack 使用同一配置隐藏启动一次，让语言包在正式显示前完成注册。 */
export async function initializeChineseLanguagePack(
  profileDir: string,
  options: LaunchOptions,
  launch = firefox.launchPersistentContext.bind(firefox),
): Promise<void> {
  if (!options.executablePath || await hasRegisteredChineseLanguagePack(profileDir)) return;
  // 未安装中文语言包时保持原有回退行为，避免每次启动都进行无效初始化。
  const installDir = process.env.CAMOUFOX_INSTALL_DIR || path.dirname(options.executablePath);
  const appContents = path.dirname(path.dirname(options.executablePath));
  const resourceDir = process.platform === "darwin" &&
    path.basename(appContents) === "Contents" && path.basename(path.dirname(appContents)) === "Camoufox.app"
    ? path.join(appContents, "Resources") : installDir;
  const pack = path.join(resourceDir, "browser", "extensions", `${languagePackID}.xpi`);
  try {
    await fs.access(pack);
  } catch {
    return;
  }
  const context = await launch(profileDir, { ...options, headless: true, viewport: null, timeout: 60_000 });
  // 正常关闭才能保存扩展注册缓存；正式启动在关闭完成后执行，避免配置目录被占用。
  await context.close();
  if (!await hasRegisteredChineseLanguagePack(profileDir)) {
    throw new Error("中文语言包初始化没有完成，请重试启动浏览器");
  }
}
