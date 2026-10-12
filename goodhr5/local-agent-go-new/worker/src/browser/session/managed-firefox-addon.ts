// 文件作用说明：把 Go 已校验的 Mozilla 签名 XPI 原包安全安装到 GoodHR 管理的 Firefox Profile。
import { createHash, randomUUID } from "node:crypto";
import { createReadStream } from "node:fs";
import fs from "node:fs/promises";
import path from "node:path";
import type { ActionContext } from "../../contracts/common.js";
import { WorkerError } from "../../errors/worker-error.js";

const managedDirectoryName = "goodhr-resume-assistant";
const signedXPIFilename = ".goodhr-signed.xpi";
const extensionID = "resume-assistant@goodhr.58it.cn";

/** fileSHA256 流式计算 XPI 摘要，避免在内存中读取整个插件包。 */
async function fileSHA256(filePath: string): Promise<string> {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(filePath)) hash.update(chunk);
  return hash.digest("hex");
}

/** isRecord 判断插件清单或发布收据是否为可安全读取的对象。 */
function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** installManagedFirefoxAddon 将原始签名 XPI 原子放入当前持久 Profile，并保留同版本幂等行为。 */
export async function installManagedFirefoxAddon(
  profileDir: string,
  extensionPaths: string[] | undefined,
  context: ActionContext,
): Promise<string | null> {
  const managedDir = extensionPaths?.find(item => path.basename(item) === managedDirectoryName);
  if (!managedDir) return null;
  const source = path.join(managedDir, signedXPIFilename);
  const destinationDir = path.join(profileDir, "extensions");
  const destination = path.join(destinationDir, `${extensionID}.xpi`);
  let temporary = "";
  try {
    const [manifestContent, receiptContent, sourceInfo] = await Promise.all([
      fs.readFile(path.join(managedDir, "manifest.json"), "utf8"),
      fs.readFile(path.join(managedDir, ".goodhr-release.json"), "utf8"),
      fs.lstat(source),
    ]);
    const manifest: unknown = JSON.parse(manifestContent);
    const receipt: unknown = JSON.parse(receiptContent);
    if (!isRecord(manifest) || !isRecord(manifest.browser_specific_settings) ||
        !isRecord(manifest.browser_specific_settings.gecko) ||
        manifest.browser_specific_settings.gecko.id !== extensionID ||
        manifest.version !== (isRecord(receipt) ? receipt.version : undefined) ||
        !isRecord(receipt) || typeof receipt.sha256 !== "string" ||
        !/^[a-f0-9]{64}$/.test(receipt.sha256) || !sourceInfo.isFile() ||
        sourceInfo.isSymbolicLink() || await fileSHA256(source) !== receipt.sha256) {
      throw new Error("已下载的签名插件身份或 SHA256 校验不通过");
    }
    await fs.mkdir(destinationDir, { recursive: true });
    const directoryInfo = await fs.lstat(destinationDir);
    if (!directoryInfo.isDirectory() || directoryInfo.isSymbolicLink()) {
      throw new Error("Firefox Profile 扩展目录不是普通目录");
    }
    const currentInfo = await fs.lstat(destination).catch((error: NodeJS.ErrnoException) => {
      if (error.code === "ENOENT") return null;
      throw error;
    });
    if (currentInfo?.isDirectory()) throw new Error("Firefox Profile 扩展路径被目录占用");
    if (currentInfo?.isFile() && !currentInfo.isSymbolicLink() &&
        await fileSHA256(destination) === receipt.sha256) return managedDir;

    temporary = path.join(destinationDir, `.${extensionID}.${randomUUID()}.tmp`);
    await fs.copyFile(source, temporary, fs.constants.COPYFILE_EXCL);
    await fs.chmod(temporary, 0o600);
    if (await fileSHA256(temporary) !== receipt.sha256) {
      throw new Error("复制到 Firefox Profile 后的 XPI 校验不通过");
    }
    await fs.rename(temporary, destination);
    temporary = "";
    return managedDir;
  } catch (error) {
    throw new WorkerError({
      code: "BROWSER_ADDON_INVALID",
      message: "简历插件没装好，请联网重启 GoodHR 后再试",
      action: context.action,
      step: "prepare_firefox_extension",
      trace_id: context.trace_id,
      retryable: true,
      details: { addon_path: managedDir, original_error: error instanceof Error ? error.message : String(error) },
      cause: error,
    });
  } finally {
    if (temporary) await fs.rm(temporary, { force: true });
  }
}
