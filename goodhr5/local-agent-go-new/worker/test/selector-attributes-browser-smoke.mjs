// 文件作用：用独立无头浏览器和本机静态合成页面验证属性过滤的真实Locator语义，不访问招聘平台或用户Profile。
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { chromium } from 'playwright-core';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';

const value = '{"thread":"synthetic-target", "text":"引号\"、反斜线\\、换行\n第二行"}';
let duplicate = false;
/** htmlAttribute 为本机合成HTML转义属性内容，不向页面注入任何脚本。 */
function htmlAttribute(text) { return text.replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;'); }
const server = createServer((_request, response) => {
  response.setHeader('Content-Type', 'text/html; charset=utf-8');
  response.end('<main id="scope">' + Array.from({ length: 68 }, (_, index) => {
    const identity = index === 5 || duplicate && index === 8 ? value : `synthetic-${index}`;
    return `<div class="row" data-meta="${htmlAttribute(identity)}"><span class="name">合成人选${index}</span></div>`;
  }).join('') + '</main>');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
let browser;
try {
  browser = await chromium.launch({ channel: 'chrome', headless: true });
  const page = await browser.newPage();
  const primitive = new LocatorPrimitive();
  const context = { trace_id: 'synthetic-attribute-browser', action: 'element.find', step: 'resolve', require_unique: true };
  const spec = { parents: [{ selectors: [{ type: 'css', value: '#scope' }] }, { selectors: [{ type: 'css', value: '.row' }], attributes: { 'data-meta': value } }], target: { selectors: [{ type: 'css', value: '.name' }] }, state: 'visible', description: '合成姓名', timeout_ms: 0 };
  await page.goto(`http://127.0.0.1:${server.address().port}`);
  const result = await primitive.resolve(page, spec, context);
  assert.equal(await result.locator.innerText(), '合成人选5');
  assert.equal(result.attempts[1].matches, 1);
  spec.parents[1].index = 5;
  assert.equal(await (await primitive.resolve(page, spec, context)).locator.innerText(), '合成人选5');
  spec.parents[1].index = 0;
  await assert.rejects(primitive.resolve(page, spec, context), error => error.code === 'ELEMENT_NOT_FOUND');
  delete spec.parents[1].index;
  duplicate = true;
  await page.reload();
  await assert.rejects(primitive.resolve(page, spec, context), error => error.code === 'ELEMENT_AMBIGUOUS');
  console.log('真实浏览器回归通过：68行唯一身份、特殊属性字符、原序号语义、重复身份拒绝；仅访问本机合成页面。');
} finally {
  await browser?.close();
  await new Promise(resolve => server.close(resolve));
}
