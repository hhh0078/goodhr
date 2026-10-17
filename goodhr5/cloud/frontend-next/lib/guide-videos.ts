/** 本文件统一整理官网视频教程配置，过滤坏数据并保留可直接播放的视频地址。 */

export type GuideVideo = {
  id: string;
  title: string;
  description: string;
  src: string;
  enabled: boolean;
  poster?: string;
};

/** normalizeGuideVideos 兼容现有接口包裹与视频字段，忽略无效或关闭的教程。 */
export function normalizeGuideVideos(value: unknown): GuideVideo[] {
  const data = asRecord(value);
  const guide = asRecord(data?.guide);
  const source = Array.isArray(value)
    ? value
    : Array.isArray(guide?.videos)
      ? guide.videos
      : Array.isArray(data?.videos)
        ? data.videos
        : [];
  return source.flatMap((item): GuideVideo[] => {
    const record = asRecord(item);
    if (!record || record.enabled === false) return [];
    const title = readText(record.title);
    const src = [record.src, record.url, record.iframe_url, record.video_url]
      .map(directVideoURL).find(Boolean);
    if (!title || !src) return [];
    const poster = directVideoURL(record.poster);
    return [{
      id: readText(record.id) || title,
      title,
      description: readText(record.description),
      src,
      enabled: true,
      ...(poster ? { poster } : {}),
    }];
  });
}

/** isHLSVideo 判断带参数的视频地址是否指向 HLS 播放列表。 */
export function isHLSVideo(src: string): boolean {
  try {
    return new URL(src).pathname.toLowerCase().endsWith(".m3u8");
  } catch {
    return false;
  }
}

/** directVideoURL 仅接受 HTTP(S) 地址，避免把脚本或无效地址交给播放器。 */
function directVideoURL(value: unknown): string {
  const text = readText(value);
  if (!text) return "";
  try {
    const url = new URL(text);
    return ["http:", "https:"].includes(url.protocol) ? url.href : "";
  } catch {
    return "";
  }
}

/** asRecord 安全识别接口中的普通对象，排除空值和数组。 */
function asRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

/** readText 读取简单文本字段，避免把对象显示成无意义的字符串。 */
function readText(value: unknown): string {
  return typeof value === "string" || typeof value === "number" ? String(value).trim() : "";
}
