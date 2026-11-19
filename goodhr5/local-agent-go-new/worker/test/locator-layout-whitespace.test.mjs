// 文件作用说明：验证排版换行兼容仅在显式开启时生效，保留完整文字顺序、正则转义和唯一性保护。
import assert from 'node:assert/strict';
import test from 'node:test';
import { LocatorPrimitive } from '../dist/browser/primitives/locator.js';
import { parseSelectorSpec } from '../dist/validation/selector.js';

/** fixture 用标准Locator查询语义模拟节点文字，不模拟浏览器的innerText排版。 */
function fixture(texts) {
  /** query 每次按全部条件筛选当前集合，保留唯一定位的实际判定过程。 */
  function query(values) {
    return { filter({hasText}) { return query(values.filter(value => hasText instanceof RegExp ? hasText.test(value) : value.toLowerCase().includes(hasText.toLowerCase()))); },
      count: async () => values.length, nth: index => query(values.slice(index,index+1)),
      isVisible: async () => values.length > 0, isEnabled: async () => true, getAttribute: async () => null,
      boundingBox: async () => ({x:0,y:0,width:400,height:100}) };
  }
  return {locator:()=>query(texts),viewportSize:()=>({width:1200,height:900}),url:()=> 'https://synthetic.invalid/'};
}
const context = {trace_id:'layout-test',action:'element.find',step:'find',require_unique:true};
const spec = {target:{selectors:[{type:'css',value:'.card'}],texts:['合成人选','30岁 5年经验','学校[A](B)+ 本科'],text_layout_whitespace:true},timeout_ms:0,description:'合成排版'};

test('兼容换行、空格及节点间零空白，不接受改变文字或颠倒经历',async()=>{
  const locator = new LocatorPrimitive();
  for(const space of ['', ' ', '\n', '\u00a0']) {
    await locator.resolve(fixture([`合成人选30岁${space}5年经验学校[A](B)+${space}本科`]),spec,context);
  }
  for(const text of ['合成人选31岁5年经验学校[A](B)+本科','合成人选5年经验30岁学校[A](B)+本科','合成人选30岁5年经验学校AB本科']) {
    await assert.rejects(locator.resolve(fixture([text]),spec,context),{code:'ELEMENT_NOT_FOUND'});
  }
});
test('关闭或不传开关保持旧匹配，完全重复身份仍拒绝选择第一位',async()=>{
  const locator = new LocatorPrimitive();const text='合成人选30岁5年经验学校[A](B)+本科';
  for(const flag of [undefined,false]) await assert.rejects(locator.resolve(fixture([text]),{...spec,target:{...spec.target,text_layout_whitespace:flag}},context),{code:'ELEMENT_NOT_FOUND'});
  await assert.rejects(locator.resolve(fixture([text,text]),spec,context),{code:'ELEMENT_AMBIGUOUS'});
});
test('参数经正式协议保留，旧请求无新增字段，非法类型拒绝',()=>{
  assert.equal(parseSelectorSpec(spec,'layout','element.find').target.text_layout_whitespace,true);
  const legacy={...spec,target:{selectors:spec.target.selectors,texts:spec.target.texts}};
  assert.equal(Object.hasOwn(parseSelectorSpec(legacy,'legacy','element.find').target,'text_layout_whitespace'),false);
  for(const flag of ['true',1,null,{}]) assert.throws(()=>parseSelectorSpec({...spec,target:{...spec.target,text_layout_whitespace:flag}},'invalid','element.find'),{code:'INVALID_REQUEST'});
});
