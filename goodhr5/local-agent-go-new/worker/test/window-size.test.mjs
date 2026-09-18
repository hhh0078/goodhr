// 文件作用说明：验证清除窗口宽高时保留其他指纹和插件配置，正确处理长配置、Unicode 和旧分段。

import assert from "node:assert/strict";
import test from "node:test";
import { useAutomaticWindowSize } from "../dist/browser/session/window-size.js";

/** decodeConfig 重组输出环境变量以读取浏览器实际启动配置。 */
function decodeConfig(env) {
  return JSON.parse(Object.keys(env).filter((key) => /^CAMOU_CONFIG_\d+$/.test(key))
    .sort((a, b) => Number(a.slice(13)) - Number(b.slice(13)))
    .map((key) => env[key]).join(""));
}

/** 验证六个尺寸覆盖全部移除，其余指纹与启动参数保留，原始配置没有被修改。 */
test("取消窗口和页面尺寸覆盖，保留其他账号配置", () => {
  const retained = {
    "navigator.userAgent": "Firefox/Test", "screen.width": 1440, "screen.height": 900,
    timezone: "Asia/Shanghai", addons: ["D:/招聘助手/插件😀"], "fonts:spacing_seed": 123,
  };
  const config = { ...retained,
    "window.outerWidth": 1440, "window.outerHeight": 744,
    "window.innerWidth": 1280, "window.innerHeight": 650,
    "document.body.clientWidth": 1280, "document.body.clientHeight": 650,
  };
  const options = { headless: false, executablePath: "browser.exe", env: {
    CAMOU_CONFIG_1: JSON.stringify(config), OTHER_SETTING: "keep",
  } };
  const result = useAutomaticWindowSize(options);
  assert.deepEqual(decodeConfig(result.env), retained);
  assert.equal(result.env.OTHER_SETTING, "keep");
  assert.equal(result.headless, false);
  assert.equal(result.executablePath, "browser.exe");
  assert.deepEqual(JSON.parse(options.env.CAMOU_CONFIG_1), config);
  assert.deepEqual(useAutomaticWindowSize(result), result);
});

/** 验证分段按数值排序、长配置可读取，原分段缩短后不残留多余内容。 */
test("正确重组超过十段的配置并清除旧分段", () => {
  const config = { "window.outerWidth": 2000, addons: ["😀".repeat(1500)] };
  const serialized = JSON.stringify(config);
  const entries = [];
  for (let start = 0; start < serialized.length; start += 120) {
    entries.push([`CAMOU_CONFIG_${Math.floor(start / 120) + 1}`, serialized.slice(start, start + 120)]);
  }
  const result = useAutomaticWindowSize({ env: Object.fromEntries(entries.reverse()) });
  assert.deepEqual(decodeConfig(result.env), { addons: config.addons });
  assert.ok(Object.values(result.env).every((value) => value.length <= (process.platform === "win32" ? 2047 : 32767)));
});

/** 验证无尺寸覆盖时无需改写，没有 Camoufox 配置时正常返回。 */
test("没有固定尺寸时保留启动选项", () => {
  const options = { env: { CAMOU_CONFIG_1: '{"timezone":"Asia/Shanghai"}' } };
  assert.equal(useAutomaticWindowSize(options), options);
  const empty = { headless: true };
  assert.equal(useAutomaticWindowSize(empty), empty);
});

/** 验证缺失或损坏分段不会静默保留尺寸覆盖，也不会在错误信息中暴露原始配置。 */
test("损坏的启动配置返回脱敏错误", () => {
  for (const env of [{ CAMOU_CONFIG_2: '{}' }, { CAMOU_CONFIG_1: '{"secret":"sensitive-test"' }, { CAMOU_CONFIG_1: '[]' }]) {
    assert.throws(() => useAutomaticWindowSize({ env }), (error) =>
      error.message.includes("浏览器启动配置") && !error.message.includes("sensitive-test"));
  }
});
