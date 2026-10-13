// 文件作用说明：验证 macOS 中文回退与绘制修正覆盖旧偏好，保留其他启动配置，并保持其他平台原样。
import assert from 'node:assert/strict';
import test from 'node:test';
import { useMacOSFontRendering } from '../dist/browser/session/font-rendering.js';

test('macOS 合并中文字体回退和 GPU 偏好，保留其他配置且不修改原对象', () => {
  const originalPrefs = {
    'intl.locale.requested': 'zh-CN',
    'gfx.bundled-fonts.activate': 1,
    'layers.gpu-process.enabled': true,
    'layers.gpu-process.force-enabled': true,
    'font.name.sans-serif.zh-CN': 'PingFang SC',
  };
  const options = {
    headless: false,
    args: ['-purgecaches'],
    env: { CAMOU_CONFIG_1: '{"navigator.platform":"Win32"}' },
    firefoxUserPrefs: originalPrefs,
  };
  const result = useMacOSFontRendering(options, 'darwin');
  assert.notEqual(result, options);
  assert.notEqual(result.firefoxUserPrefs, originalPrefs);
  assert.deepEqual(result.firefoxUserPrefs, {
    'intl.locale.requested': 'zh-CN',
    'gfx.bundled-fonts.activate': 1,
    'layers.gpu-process.enabled': false,
    'layers.gpu-process.force-enabled': false,
    'font.name.sans-serif.zh-CN': 'Noto Sans SC',
    'font.name.serif.zh-CN': 'Noto Sans SC',
    'font.name.monospace.zh-CN': 'Noto Sans SC',
  });
  assert.equal(result.headless, false);
  assert.equal(result.args, options.args);
  assert.equal(result.env, options.env);
  assert.equal(options.firefoxUserPrefs, originalPrefs);
  assert.equal(originalPrefs['layers.gpu-process.enabled'], true);
  assert.equal(originalPrefs['layers.gpu-process.force-enabled'], true);
  assert.equal(originalPrefs['font.name.sans-serif.zh-CN'], 'PingFang SC');
});

test('Windows 和 Linux 返回原对象，不改写或增加字体及 GPU 偏好', () => {
  for (const platform of ['win32', 'linux']) {
    const options = {
      args: ['-purgecaches'],
      env: { CAMOU_CONFIG_1: '{}' },
      firefoxUserPrefs: { 'layers.gpu-process.enabled': true },
    };
    assert.equal(useMacOSFontRendering(options, platform), options);
    assert.deepEqual(options.firefoxUserPrefs, { 'layers.gpu-process.enabled': true });
  }
});

test('macOS 没有既有偏好时创建绘制配置，原对象仍没有偏好', () => {
  const options = { headless: true };
  const result = useMacOSFontRendering(options, 'darwin');
  assert.deepEqual(result.firefoxUserPrefs, {
    'layers.gpu-process.enabled': false,
    'layers.gpu-process.force-enabled': false,
    'font.name.sans-serif.zh-CN': 'Noto Sans SC',
    'font.name.serif.zh-CN': 'Noto Sans SC',
    'font.name.monospace.zh-CN': 'Noto Sans SC',
  });
  assert.equal(result.headless, true);
  assert.equal(Object.hasOwn(options, 'firefoxUserPrefs'), false);
});
