/** 本文件验证官网视频配置兼容旧字段、坏配置隔离和带参数 HLS 地址识别。 */
import assert from "node:assert/strict";
import test from "node:test";
import { isHLSVideo, normalizeGuideVideos } from "./guide-videos.ts";

test("现有接口包裹与多条视频字段保持兼容，封面可选", () => {
  const videos = [
    { id: "install", title: " 安装教程 ", src: "https://media.example.test/install/index.m3u8?key=abc", poster: "https://media.example.test/poster.jpg" },
    { title: "MP4 教程", video_url: "http://media.example.test/guide.mp4", description: "直接播放" },
    { title: "旧地址字段", iframe_url: "https://media.example.test/old.mp4" },
  ];
  for (const input of [videos, { videos }, { guide: { videos } }]) {
    const result = normalizeGuideVideos(input);
    assert.equal(result.length, 3);
    assert.equal(result[0].title, "安装教程");
    assert.equal(result[0].src, videos[0].src);
    assert.equal(result[0].poster, videos[0].poster);
    assert.equal(result[1].description, "直接播放");
    assert.equal(result[2].src, videos[2].iframe_url);
    assert.equal("poster" in result[1], false);
  }
});

test("坏条目和关闭的视频不影响其余可播放教程", () => {
  const source = [null, [], "oops", 3, {}, { title: "关闭", src: "https://media.example.test/a.mp4", enabled: false },
    { title: "脚本", src: "javascript:alert(1)" }, { title: "错误地址", src: "broken" },
    { title: { unexpected: true }, src: "https://media.example.test/a.mp4" },
    { title: "可用", src: "https://media.example.test/a.mp4", description: {}, poster: "data:image/png;base64,AA" }];
  const result = normalizeGuideVideos(source);
  assert.deepEqual(result, [{ id: "可用", title: "可用", src: "https://media.example.test/a.mp4", description: "", enabled: true }]);
  for (const input of [null, undefined, false, "broken", {}, { guide: null }, { guide: { videos: "bad" } }]) {
    assert.deepEqual(normalizeGuideVideos(input), []);
  }
});

test("视频地址支持旧字段兜底，不把无效 src 交给播放器", () => {
  const [video] = normalizeGuideVideos([{ title: "旧配置", src: {}, url: "https://media.example.test/guide.mp4" }]);
  assert.equal(video.src, "https://media.example.test/guide.mp4");
});

test("按路径识别带参数和大小写 HLS，MP4 参数不能冒充播放列表", () => {
  assert.equal(isHLSVideo("https://media.example.test/INDEX.M3U8?token=x#position"), true);
  assert.equal(isHLSVideo("https://media.example.test/clip.mp4?next=index.m3u8"), false);
  assert.equal(isHLSVideo("bad"), false);
});
