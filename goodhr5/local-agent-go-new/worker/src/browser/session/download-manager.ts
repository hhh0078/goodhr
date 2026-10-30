// 文件作用说明：读取 Firefox 原生下载状态、维护 GoodHR 记录和目录设置，不接管文件保存。

import { randomUUID } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import type { DownloadListResult, DownloadRecord } from "../../contracts/actions.js";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { WorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { parseNativeDownloadSnapshot, type NativeDownloadSnapshot } from "./native-download-snapshot.js";

/** DownloadManager 观察唯一浏览器会话的原生下载快照，Go 继续通过既有协议同步记录。 */
export class DownloadManager {
  private downloadsPath = path.join(os.homedir(), "Downloads");
  private readonly downloads = new Map<string, DownloadRecord>();
  private readonly cleared = new Set<string>();
  private bridgeDirectory = "";
  private sessionID = "";
  private revision = 0;
  private ready = false;
  private configuration: Promise<void> = Promise.resolve();

  /** 创建下载状态管理器。 */
  constructor(private readonly logger: WorkerLogger) {}

  /** prepare 创建初始目录，浏览器尚未启动时不宣称配置已生效。 */
  async prepare(directory?: string): Promise<void> {
    const next = path.resolve(directory?.trim() || path.join(os.homedir(), "Downloads"));
    await fs.mkdir(next, { recursive: true });
    this.downloadsPath = next;
  }

  /** prepareNativeSession 创建仅当前会话使用的状态目录，并返回浏览器自身的启动首选项。 */
  async prepareNativeSession(): Promise<Record<string, string | number | boolean>> {
    await this.reset();
    this.bridgeDirectory = await fs.mkdtemp(path.join(os.tmpdir(), "goodhr-native-downloads-"));
    await fs.chmod(this.bridgeDirectory, 0o700);
    this.sessionID = randomUUID();
    this.revision = 0;
    await this.writeSettings(this.downloadsPath);
    return {
      "goodhr.downloads.native": true,
      "goodhr.downloads.settings": path.join(this.bridgeDirectory, "settings.json"),
      "goodhr.downloads.snapshot": this.snapshotPath(),
      "goodhr.downloads.session": this.sessionID,
      "browser.download.dir": this.downloadsPath,
      "browser.download.folderList": 2,
      "browser.download.useDownloadDir": true,
    };
  }

  /** resetSnapshot 清除隐藏启动留下的快照，必须等待正式浏览器确认。 */
  async resetSnapshot(): Promise<void> {
    if (this.bridgeDirectory) await fs.rm(this.snapshotPath(), { force: true });
  }

  /** waitForReady 确认原生观察器已启动；缺少资源修正时明确报错。 */
  async waitForReady(): Promise<void> {
    this.merge(await this.waitForRevision(this.revision, this.downloadsPath));
    this.ready = true;
  }

  /** directory 返回浏览器确认过的 GoodHR 下载目录。 */
  directory(): string { return this.downloadsPath; }

  /** registerFetchedDocument 记录通过当前 Firefox 会话保存的文档页。 */
  registerFetchedDocument(record: DownloadRecord): void {
    if (this.cleared.has(record.id)) return;
    this.downloads.set(record.id, record);
    const sorted = [...this.downloads.values()].sort((a, b) => b.created_at.localeCompare(a.created_at));
    for (const old of sorted.slice(100)) this.downloads.delete(old.id);
  }

  /** list 同步原生下载状态并返回最近一百条 GoodHR 记录。 */
  async list(): Promise<DownloadListResult> {
    if (this.bridgeDirectory && this.ready) this.merge(await this.readSnapshot());
    const downloads = [...this.downloads.values()].sort((a, b) => b.created_at.localeCompare(a.created_at)).slice(0, 100);
    return {
      downloads, count: downloads.length,
      pending: downloads.filter((record) => record.status === "pending").length,
      directory: this.downloadsPath,
    };
  }

  /** configure 等浏览器确认新目录后返回成功，已有下载仍保留原路径。 */
  async configure(directory: string, actionContext: ActionContext): Promise<JsonObject> {
    if (!directory.trim()) throw new WorkerError({
      code: "INVALID_REQUEST", message: "下载目录不能为空", action: actionContext.action,
      step: "configure_downloads", trace_id: actionContext.trace_id,
    });
    const task = this.configuration.then(() => this.applyDirectory(directory, actionContext));
    this.configuration = task.then(() => undefined, () => undefined);
    return task;
  }

  /** applyDirectory 串行应用目录，避免并发请求覆盖尚未被浏览器确认的设置。 */
  private async applyDirectory(directory: string, actionContext: ActionContext): Promise<JsonObject> {
    const next = path.resolve(directory);
    await fs.mkdir(next, { recursive: true });
    if (this.bridgeDirectory) {
      const revision = ++this.revision;
      await this.writeSettings(next);
      this.merge(await this.waitForRevision(revision, next));
    }
    this.downloadsPath = next;
    this.logger.info(actionContext, "configure_downloads", "success", { directory: next });
    return { configured: true, directory: next };
  }

  /** clear 只清除 GoodHR 记录，浏览器原生历史与用户文件保持完整。 */
  async clear(): Promise<JsonObject> {
    await this.list();
    const cleared = this.downloads.size;
    for (const id of this.downloads.keys()) this.cleared.add(id);
    this.downloads.clear();
    return { cleared, files_deleted: false };
  }

  /** reset 删除本次随机会话目录，最近记录继续留给 Go 做最后同步。 */
  async reset(): Promise<void> {
    const directory = this.bridgeDirectory;
    this.bridgeDirectory = "";
    this.sessionID = "";
    this.ready = false;
    if (directory) await fs.rm(directory, { recursive: true, force: true });
  }

  /** snapshotPath 返回当前会话的浏览器状态文件位置。 */
  private snapshotPath(): string { return path.join(this.bridgeDirectory, "snapshot.json"); }

  /** writeSettings 原子替换本地目录设置，防止浏览器读到半份 JSON。 */
  private async writeSettings(directory: string): Promise<void> {
    const target = path.join(this.bridgeDirectory, "settings.json");
    const temporary = `${target}.${randomUUID()}.tmp`;
    try {
      await fs.writeFile(temporary, JSON.stringify({ version: 1, session_id: this.sessionID, revision: this.revision, directory }), { mode: 0o600 });
      await fs.rename(temporary, target);
    } finally { await fs.rm(temporary, { force: true }); }
  }

  /** readSnapshot 校验大小与会话来源，不打印文件来源 URL 等敏感内容。 */
  private async readSnapshot(): Promise<NativeDownloadSnapshot> {
    const sessionID = this.sessionID;
    const file = await fs.open(this.snapshotPath(), "r");
    try {
      if ((await file.stat()).size > 2_000_000) throw new Error("原生下载状态文件过大");
      const value: unknown = JSON.parse(await file.readFile("utf8"));
      return parseNativeDownloadSnapshot(value, sessionID);
    } finally { await file.close(); }
  }

  /** waitForRevision 有限等待浏览器确认，失败时提示重新启动组件。 */
  private async waitForRevision(revision: number, directory: string): Promise<NativeDownloadSnapshot> {
    const deadline = Date.now() + 10_000;
    while (Date.now() < deadline) {
      try {
        const snapshot = await this.readSnapshot();
        if (snapshot.revision >= revision && snapshot.directory === directory) return snapshot;
      } catch { /* 浏览器启动或原子替换期间稍后再读；超时统一返回明确错误。 */ }
      await delay(100);
    }
    throw new Error("浏览器原生下载通道没有准备好，请更新并重启本地程序和浏览器");
  }

  /** merge 更新浏览器实际终态，保留已从浏览器列表移除的 GoodHR 历史。 */
  private merge(snapshot: NativeDownloadSnapshot): void {
    for (const record of snapshot.downloads) {
      if (!this.cleared.has(record.id)) this.downloads.set(record.id, record);
    }
    const sorted = [...this.downloads.values()].sort((a, b) => b.created_at.localeCompare(a.created_at));
    for (const record of sorted.slice(100)) this.downloads.delete(record.id);
  }
}
