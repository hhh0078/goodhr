// 文件作用说明：定位 Camoufox 浏览器可执行文件并读取安装版本，供运行状态展示和浏览器启动参数使用。

import fs from "node:fs";
import os from "node:os";
import path from "node:path";

/** CamoufoxBinaryInfo 描述解析出的浏览器可执行文件路径与安装版本。 */
export interface CamoufoxBinaryInfo {
  binaryPath: string;
  version: string;
}

/** resolveCamoufoxBinary 优先使用环境变量指定路径，否则在默认安装目录中递归查找启动文件。 */
export function resolveCamoufoxBinary(): CamoufoxBinaryInfo {
  const installDir =
    process.env.CAMOUFOX_INSTALL_DIR?.trim() || defaultInstallDir();
  const configured = process.env.CAMOUFOX_BINARY_PATH?.trim() ?? "";
  return {
    binaryPath: configured || findLaunchBinary(installDir),
    version: readInstallVersion(installDir),
  };
}

/** defaultInstallDir 返回 camoufox-js 约定的默认安装目录，与官方 fetch 行为保持一致。 */
function defaultInstallDir(): string {
  const home = os.homedir();
  switch (process.platform) {
    case "win32":
      return path.join(home, "AppData", "Local", "camoufox", "camoufox", "Cache");
    case "darwin":
      return path.join(home, "Library", "Caches", "camoufox");
    default:
      return path.join(home, ".cache", "camoufox");
  }
}

/** findLaunchBinary 在安装目录中递归查找当前平台的 Camoufox 启动文件。 */
function findLaunchBinary(installDir: string): string {
  const binaryName = launchBinaryName();
  if (!binaryName) {
    return "";
  }
  return findFileByName(installDir, binaryName, 0);
}

/** launchBinaryName 返回当前平台的 Camoufox 启动文件名。 */
function launchBinaryName(): string | null {
  switch (process.platform) {
    case "win32":
      return "camoufox.exe";
    case "darwin":
      // macOS 启动文件位于 Camoufox.app/Contents/MacOS/camoufox。
      return "camoufox";
    case "linux":
      return "camoufox-bin";
    default:
      return null;
  }
}

/** findFileByName 按文件名递归查找，限制层级避免在超大目录上耗时过久。 */
function findFileByName(directory: string, name: string, depth: number): string {
  if (depth > 6) {
    return "";
  }
  try {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const entryPath = path.join(directory, entry.name);
      if (entry.isFile() && entry.name === name) {
        return entryPath;
      }
      if (entry.isDirectory()) {
        const found = findFileByName(entryPath, name, depth + 1);
        if (found) {
          return found;
        }
      }
    }
  } catch {
    // 安装目录不存在或不可读时按未安装处理。
  }
  return "";
}

/** readInstallVersion 读取安装目录的 version.json，缺失或损坏时返回空字符串。 */
function readInstallVersion(installDir: string): string {
  try {
    const content = fs.readFileSync(
      path.join(installDir, "version.json"),
      "utf8",
    );
    const parsed: unknown = JSON.parse(content);
    if (!parsed || typeof parsed !== "object") {
      return "";
    }
    const record = parsed as { version?: unknown; release?: unknown };
    const version = typeof record.version === "string" ? record.version : "";
    const release = typeof record.release === "string" ? record.release : "";
    if (version && release) {
      return `${version}-${release}`;
    }
    return version || release;
  } catch {
    return "";
  }
}
