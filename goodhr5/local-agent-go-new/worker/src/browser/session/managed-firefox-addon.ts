// 文件作用说明：核验 Go 准备的 Firefox 插件；临时包保留给 Camoufox 加载，签名原包按旧规则安装到 Profile。
import { createHash, randomUUID } from "node:crypto";
import { createReadStream } from "node:fs";
import fs from "node:fs/promises";
import path from "node:path";
import type { ActionContext } from "../../contracts/common.js";
import { WorkerError } from "../../errors/worker-error.js";

const managedDirectoryName = "goodhr-resume-assistant";
const signedXPIFilename = ".goodhr-signed.xpi";
const extensionID = "resume-assistant@goodhr.58it.cn";
const releaseReceiptFilename = ".goodhr-release.json";
const loadRequiredFilename = ".goodhr-load-required";

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

/** directoryTreeSHA256 按 Go 安装器的 UTF8 路径顺序和文件字节计算摘要，拒绝链接、特殊文件及读取期间变化。 */
async function directoryTreeSHA256(directory: string): Promise<string> {
  const files: string[] = [];
  let entryCount = 0, totalSize = 0;
  /** visit 收集插件目录中的普通文件，只排除根级安装收据与待加载标记。 */
  async function visit(relativeDirectory: string): Promise<void> {
    for (const entry of await fs.readdir(path.join(directory, relativeDirectory), { withFileTypes: true })) {
      if (++entryCount > 5000) throw new Error("临时插件目录文件数量过多");
      const relative = relativeDirectory ? `${relativeDirectory}/${entry.name}` : entry.name;
      if (entry.isSymbolicLink()) throw new Error("临时插件目录包含符号链接");
      if (relative === releaseReceiptFilename || relative === loadRequiredFilename) continue;
      if (entry.isDirectory()) await visit(relative);
      else if (entry.isFile()) files.push(relative);
      else throw new Error("临时插件目录包含特殊文件");
    }
  }
  await visit("");
  files.sort((left, right) => Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8")));
  const hash = createHash("sha256");
  for (const relative of files) {
    const fullPath = path.join(directory, ...relative.split("/")), before = await fs.lstat(fullPath);
    if (!before.isFile() || before.isSymbolicLink() || !Number.isSafeInteger(before.size) || (totalSize += before.size) > 256 * 1024 * 1024) throw new Error("临时插件文件类型或大小不正确");
    hash.update(`${Buffer.byteLength(relative, "utf8")}:${relative}:${before.size}:`, "utf8");
    let written = 0;
    for await (const chunk of createReadStream(fullPath)) { written += chunk.length; if (written > before.size) throw new Error("核验临时插件时文件发生变化"); hash.update(chunk); }
    const after = await fs.lstat(fullPath);
    if (written !== before.size || !after.isFile() || after.isSymbolicLink() || before.size !== after.size || before.ino !== after.ino || before.dev !== after.dev || before.mtimeMs !== after.mtimeMs) throw new Error("核验临时插件时文件发生变化");
  }
  return hash.digest("hex");
}

/** validateTemporaryAddon 复核固定产品、版本、后台入口与完整目录收据，不凭模式字段跳过身份或内容校验。 */
async function validateTemporaryAddon(directory: string, manifest: Record<string, unknown>, receipt: Record<string, unknown>): Promise<void> {
  const info = await fs.lstat(directory), background = manifest.background;
  if (!path.isAbsolute(directory) || !info.isDirectory() || info.isSymbolicLink() || manifest.name !== "GoodHR 简历助手" || manifest.manifest_version !== 3 || typeof manifest.version !== "string" || !manifest.version || manifest.version !== receipt.version || !isRecord(background) || !Array.isArray(background.scripts) || !background.scripts.length || background.service_worker !== undefined || typeof receipt.tree_sha256 !== "string" || !/^[a-f0-9]{64}$/.test(receipt.tree_sha256)) throw new Error("临时插件身份、版本或目录收据不完整");
  const signedSource = await fs.lstat(path.join(directory, signedXPIFilename)).catch((error: NodeJS.ErrnoException) => { if (error.code === "ENOENT") return null; throw error; });
  if (signedSource) throw new Error("临时插件不能混用保留的签名原包");
  for (const script of background.scripts) {
    if (typeof script !== "string" || !script || script.trim() !== script || script.includes("\\") || script.split("/").some(segment => !segment || segment === "." || segment === ".." || segment.includes(":") || /[\u0000-\u001f\u007f]/.test(segment))) throw new Error("临时插件后台入口路径不正确");
    const file = await fs.lstat(path.join(directory, ...script.split("/")));
    if (!file.isFile() || file.isSymbolicLink() || file.size <= 0) throw new Error("临时插件后台入口不存在或为空");
  }
  if (await directoryTreeSHA256(directory) !== receipt.tree_sha256) throw new Error("临时插件目录 SHA256 校验不通过");
}

/** installManagedFirefoxAddon 临时包核验后保留原生加载路径；签名 XPI 仍原子进入持久 Profile，不卸载或清理登录数据。 */
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
    const [manifestContent, receiptContent] = await Promise.all([
      fs.readFile(path.join(managedDir, "manifest.json"), "utf8"),
      fs.readFile(path.join(managedDir, releaseReceiptFilename), "utf8"),
    ]);
    const manifest: unknown = JSON.parse(manifestContent);
    const receipt: unknown = JSON.parse(receiptContent);
    if (!isRecord(manifest) || !isRecord(manifest.browser_specific_settings) ||
        !isRecord(manifest.browser_specific_settings.gecko) ||
        manifest.browser_specific_settings.gecko.id !== extensionID ||
        manifest.version !== (isRecord(receipt) ? receipt.version : undefined) ||
        !isRecord(receipt) || typeof receipt.sha256 !== "string" ||
        !/^[a-f0-9]{64}$/.test(receipt.sha256)) {
      throw new Error("已下载的插件身份或 SHA256 收据校验不通过");
    }
    if (receipt.channel === "temporary") {
      await validateTemporaryAddon(managedDir, manifest, receipt);
      return null;
    }
    if (receipt.channel !== undefined && receipt.channel !== "") throw new Error("插件发布渠道不正确");
    const sourceInfo = await fs.lstat(source);
    if (!sourceInfo.isFile() ||
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
