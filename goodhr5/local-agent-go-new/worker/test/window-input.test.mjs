// 文件作用说明：验证 Windows 后台输入启动偏好只关闭遮挡跟踪，保留已有设置且不修改原对象。
import assert from 'node:assert/strict';
import test from 'node:test';
import { useBackgroundWindowInput } from '../dist/browser/session/window-input.js';

test('Windows 合并遮挡配置，保留语言、标题栏及其他启动偏好，不修改原对象', () => {
  const originalPrefs = {
    'intl.locale.requested': 'zh-CN',
    'browser.tabs.inTitlebar': 0,
    'widget.windows.window_occlusion_tracking.enabled': true,
  };
  const options = { headless: false, args: ['-purgecaches'], firefoxUserPrefs: originalPrefs };
  const result = useBackgroundWindowInput(options, 'win32');
  assert.notEqual(result, options);
  assert.notEqual(result.firefoxUserPrefs, originalPrefs);
  assert.deepEqual(result, {
    headless: false,
    args: ['-purgecaches'],
    firefoxUserPrefs: {
      'intl.locale.requested': 'zh-CN',
      'browser.tabs.inTitlebar': 0,
      'widget.windows.window_occlusion_tracking.enabled': false,
    },
  });
  assert.equal(options.firefoxUserPrefs, originalPrefs);
  assert.equal(originalPrefs['widget.windows.window_occlusion_tracking.enabled'], true);
  assert.deepEqual(options.args, ['-purgecaches']);
});

test('macOS 和 Linux 返回原启动对象，不增加或改写偏好', () => {
  for (const platform of ['darwin', 'linux']) {
    const options = { firefoxUserPrefs: { 'intl.locale.requested': 'zh-CN' }, headless: false };
    assert.equal(useBackgroundWindowInput(options, platform), options);
    assert.deepEqual(options.firefoxUserPrefs, { 'intl.locale.requested': 'zh-CN' });
  }
});

test('Windows 没有既有偏好时创建遮挡配置，原对象仍没有偏好', () => {
  const options = { headless: true };
  const result = useBackgroundWindowInput(options, 'win32');
  assert.deepEqual(result.firefoxUserPrefs, { 'widget.windows.window_occlusion_tracking.enabled': false });
  assert.equal(result.headless, true);
  assert.equal(Object.hasOwn(options, 'firefoxUserPrefs'), false);
});
