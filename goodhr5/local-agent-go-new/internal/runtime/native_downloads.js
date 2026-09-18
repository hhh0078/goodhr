// 文件作用说明：观察 Firefox 原生下载状态并写入本机会话快照，文件保存和历史管理始终由 Firefox 完成。

const { Downloads } = ChromeUtils.importESModule("resource://gre/modules/Downloads.sys.mjs");
const { setInterval, clearInterval, setTimeout, clearTimeout } = ChromeUtils.importESModule("resource://gre/modules/Timer.sys.mjs");
const { AsyncShutdown } = ChromeUtils.importESModule("resource://gre/modules/AsyncShutdown.sys.mjs");

/** startGoodHRNativeDownloads 注册浏览器原生下载观察器，不读取或修改网页。 */
export async function startGoodHRNativeDownloads() {
  const settingsPath = Services.prefs.getStringPref("goodhr.downloads.settings", "");
  const snapshotPath = Services.prefs.getStringPref("goodhr.downloads.snapshot", "");
  const sessionID = Services.prefs.getStringPref("goodhr.downloads.session", "");
  if (!settingsPath || !snapshotPath || !sessionID) throw new Error("GoodHR 原生下载会话配置不完整");
  const list = await Downloads.getList(Downloads.ALL);
  const records = new Map();
  let revision = -1;
  let directory = "";
  let closing = false;
  let writeTimer = null;
  let writes = Promise.resolve();
  let settingsBusy = false;

  /** flushSnapshot 串行、原子写入最多一百条本地下载状态。 */
  function flushSnapshot() {
    const downloads = [...records.values()].sort((a, b) => b.created_at.localeCompare(a.created_at)).slice(0, 100);
    const snapshot = { version: 1, session_id: sessionID, revision, directory, downloads };
    writes = writes.catch(Cu.reportError).then(() => IOUtils.writeJSON(snapshotPath, snapshot, { tmpPath: snapshotPath + ".tmp" }));
    return writes;
  }

  /** scheduleSnapshot 合并频繁进度通知，避免每个数据块都写入磁盘。 */
  function scheduleSnapshot() {
    if (closing || writeTimer !== null) return;
    writeTimer = setTimeout(() => {
      writeTimer = null;
      flushSnapshot().catch(Cu.reportError);
    }, 100);
  }

  /** observeDownload 只记录 Firefox 实际文件路径与终态，不另存或重命名文件。 */
  function observeDownload(download) {
    const filePath = download.target.path;
    if (!filePath) return;
    const createdAt = download.startTime?.toISOString() || new Date().toISOString();
    const key = JSON.stringify([filePath, download.source.url, createdAt]);
    const status = download.succeeded ? "saved" : download.error || download.canceled ? "failed" : "pending";
    records.set(key, {
      file_path: filePath,
      url: download.source.url || "",
      page_url: download.source.referrerInfo?.originalReferrer?.spec || "",
      size: download.succeeded ? download.target.size : download.currentBytes,
      status,
      error: download.canceled ? "下载已取消" : download.error ? "浏览器没有完成这次下载，可以在下载列表里重试" : "",
      created_at: createdAt,
    });
    if (records.size > 100) {
      const oldest = [...records.entries()].sort((a, b) => a[1].created_at.localeCompare(b[1].created_at))[0];
      records.delete(oldest[0]);
    }
    scheduleSnapshot();
  }

  /** applySettings 读取当前会话目录设置，浏览器确认生效后才回写版本号。 */
  async function applySettings() {
    if (closing || settingsBusy) return;
    settingsBusy = true;
    try {
      const settings = await IOUtils.readJSON(settingsPath);
      if (settings.session_id !== sessionID || settings.version !== 1 || !Number.isSafeInteger(settings.revision) || typeof settings.directory !== "string" || !PathUtils.isAbsolute(settings.directory)) {
        throw new Error("GoodHR 原生下载目录配置无法识别");
      }
      if (settings.revision <= revision) return;
      Services.prefs.setStringPref("browser.download.dir", settings.directory);
      Services.prefs.setIntPref("browser.download.folderList", 2);
      Services.prefs.setBoolPref("browser.download.useDownloadDir", true);
      directory = settings.directory;
      revision = settings.revision;
      await flushSnapshot();
    } finally {
      settingsBusy = false;
    }
  }

  await list.addView({
    onDownloadAdded: observeDownload,
    onDownloadChanged: observeDownload,
    onDownloadRemoved: observeDownload,
  });
  await applySettings();
  const settingsTimer = setInterval(() => applySettings().catch(Cu.reportError), 500);
  AsyncShutdown.profileBeforeChange.addBlocker("GoodHR 原生下载状态落盘", async () => {
    closing = true;
    clearInterval(settingsTimer);
    if (writeTimer !== null) clearTimeout(writeTimer);
    await flushSnapshot();
  });
}
