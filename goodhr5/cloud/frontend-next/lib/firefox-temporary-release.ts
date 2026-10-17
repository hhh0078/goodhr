/** 本文件负责校验官网与本地程序共用的 Firefox 临时插件发布清单。 */

/** FirefoxTemporaryRelease 表示已核对渠道、扩展身份和同站下载地址的临时插件包。 */
export type FirefoxTemporaryRelease = {
  version: string;
  url: string;
  sha256: string;
  extension_id: "resume-assistant@goodhr.58it.cn";
  channel: "temporary";
};

/** normalizeFirefoxTemporaryRelease 拒绝错误渠道、身份和版本，以及跳往站外的下载链接。 */
export function normalizeFirefoxTemporaryRelease(value: unknown, siteURL: string): FirefoxTemporaryRelease | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const source = value as Record<string, unknown>;
  if (Object.keys(source).length !== 5
    || typeof source.version !== "string"
    || !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(source.version)
    || source.version.length > 64
    || typeof source.url !== "string"
    || typeof source.sha256 !== "string"
    || !/^[a-fA-F0-9]{64}$/.test(source.sha256)
    || source.extension_id !== "resume-assistant@goodhr.58it.cn"
    || source.channel !== "temporary") return null;
  try {
    const site = new URL(siteURL);
    const download = new URL(source.url, site.origin);
    if ((site.protocol !== "https:" && !(site.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(site.hostname)))
      || download.origin !== site.origin || download.username || download.password || download.search || download.hash
      || download.pathname !== `/zip/goodhr-resume-firefox-unsigned-${source.version}.xpi`) return null;
    return {
      version: source.version,
      url: download.href,
      sha256: source.sha256.toLowerCase(),
      extension_id: "resume-assistant@goodhr.58it.cn",
      channel: "temporary",
    };
  } catch {
    return null;
  }
}
