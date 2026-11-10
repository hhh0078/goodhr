// 文件作用：验证PNG判重优化保留全部过滤器、RGB/RGBA通道、像素容差和压缩兜底阈值，不读取真实候选人资料。
import assert from "node:assert/strict";
import test from "node:test";
import { deflateSync } from "node:zlib";
import { screenshotsAreDuplicate } from "../dist/browser/actions/screenshot.js";

/** pngCRC 生成标准PNG块校验，不让测试依赖损坏图片或第三方图像包。 */
function pngCRC(buffer) {
  let crc = 0xffffffff;
  for (const byte of buffer) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

/** pngChunk 创建带长度和CRC的标准PNG数据块。 */
function pngChunk(type, data) {
  const name = Buffer.from(type), chunk = Buffer.alloc(data.length + 12);
  chunk.writeUInt32BE(data.length, 0); name.copy(chunk, 4); data.copy(chunk, 8);
  chunk.writeUInt32BE(pngCRC(Buffer.concat([name, data])), data.length + 8);
  return chunk;
}

/** predictor 返回PNG标准Paeth预测值，供独立编码测试图使用。 */
function predictor(left, up, upperLeft) {
  const value = left + up - upperLeft;
  const distances = [Math.abs(value - left), Math.abs(value - up), Math.abs(value - upperLeft)];
  return distances[0] <= distances[1] && distances[0] <= distances[2] ? left : distances[1] <= distances[2] ? up : upperLeft;
}

/** pngFixture 用已知像素编码五种标准行过滤器，只生成合成内存PNG，不写图片文件。 */
function pngFixture({ width = 100, height = 100, channels = 3, filter = 0, level = 6, pixel = (x, y) => [(x * 7 + y * 11) & 255, (x * 13 + y * 3) & 255, (x * 5 + y * 17) & 255, 255] } = {}) {
  const stride = width * channels, rows = Buffer.alloc((stride + 1) * height), raw = Buffer.alloc(stride * height);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const values = pixel(x, y);
      for (let channel = 0; channel < channels; channel++) raw[y * stride + x * channels + channel] = values[channel];
    }
    const mode = Array.isArray(filter) ? filter[y % filter.length] : filter;
    rows[y * (stride + 1)] = mode;
    for (let index = 0; index < stride; index++) {
      const offset = y * stride + index, left = index >= channels ? raw[offset - channels] : 0;
      const up = y > 0 ? raw[offset - stride] : 0, upperLeft = y > 0 && index >= channels ? raw[offset - stride - channels] : 0;
      const predicted = mode === 1 ? left : mode === 2 ? up : mode === 3 ? Math.floor((left + up) / 2) : mode === 4 ? predictor(left, up, upperLeft) : 0;
      rows[y * (stride + 1) + index + 1] = (raw[offset] - predicted) & 255;
    }
  }
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0); header.writeUInt32BE(height, 4); header[8] = 8; header[9] = channels === 4 ? 6 : 2;
  return Buffer.concat([Buffer.from("89504e470d0a1a0a", "hex"), pngChunk("IHDR", header), pngChunk("IDAT", deflateSync(rows, { level })), pngChunk("IEND", Buffer.alloc(0))]);
}

test("五种PNG过滤器和RGB/RGBA编码同一画面时判为重复", () => {
  const reference = pngFixture({ filter: 0 });
  for (const channels of [3, 4]) {
    for (const filter of [0, 1, 2, 3, 4, [0, 1, 2, 3, 4]]) {
      const encoded = pngFixture({ channels, filter, level: 1 });
      assert.equal(screenshotsAreDuplicate(reference, encoded), true);
    }
  }
});

test("RGB总差24仍沿用原容差，25及明显变动画面不判重复，alpha不参与旧RGB规则", () => {
  const base = pngFixture({ pixel: () => [20, 30, 40, 255], filter: 4 });
  assert.equal(screenshotsAreDuplicate(base, pngFixture({ pixel: () => [28, 38, 48, 255], filter: 3 })), true);
  assert.equal(screenshotsAreDuplicate(base, pngFixture({ pixel: () => [28, 38, 49, 255], filter: 2 })), false);
  assert.equal(screenshotsAreDuplicate(base, pngFixture({ channels: 4, pixel: () => [20, 30, 40, 0], filter: 1 })), true);
  assert.equal(screenshotsAreDuplicate(base, pngFixture({ width: 101 })), false);
});

test("像素样本98%门槛保持不变，不因性能优化提前判断到底", () => {
  const base = pngFixture({ pixel: () => [0, 0, 0, 255] });
  /** changedPixel 只改变原算法实际采样区的前若干点，精确验证边界。 */
  const changedPixel = count => (x, y) => x >= 10 && x < 90 && y >= 5 && y < 95 && (y - 5) * 80 + x - 10 < count ? [255, 255, 255, 255] : [0, 0, 0, 255];
  assert.equal(screenshotsAreDuplicate(base, pngFixture({ pixel: changedPixel(144), filter: 4 })), true);
  assert.equal(screenshotsAreDuplicate(base, pngFixture({ pixel: changedPixel(145), filter: 4 })), false);
});

test("非PNG压缩字节兜底及完全相同字节语义保持不变", () => {
  const base = Buffer.alloc(600, 50), within = Buffer.from(base), outside = Buffer.from(base);
  within.fill(100, 72, 78); outside.fill(100, 72, 79);
  assert.equal(screenshotsAreDuplicate(base, Buffer.from(base)), true);
  assert.equal(screenshotsAreDuplicate(base, within), true);
  assert.equal(screenshotsAreDuplicate(base, outside), false);
});
