// 文件作用说明：验证输入焦点、异常松开鼠标与并发操作串行化，防止拖选页面和误输入。
import assert from "node:assert/strict";
import test from "node:test";
import { ActionQueue } from "../dist/browser/actions/action-queue.js";
import { InputAction } from "../dist/browser/actions/input.js";
import { MousePrimitive } from "../dist/browser/primitives/mouse.js";
import { waitForStablePosition } from "../dist/browser/actions/stable-position.js";

test("前一次操作失败并松开后才运行下一次操作", async () => {
  const queue = new ActionQueue();
  const events = [];
  let release;
  const held = new Promise(resolve => { release = resolve; });
  const first = queue.run(async () => {
    events.push("down");
    try { await held; throw new Error("原操作失败"); }
    finally { events.push("up"); }
  });
  const firstChecked = assert.rejects(first, /原操作失败/);
  const second = queue.run(async () => { events.push("move"); });
  await Promise.resolve();
  assert.deepEqual(events, ["down"]);
  release();
  await Promise.all([firstChecked, second]);
  assert.deepEqual(events, ["down", "up", "move"]);
});

test("鼠标定位移动前释放遗留左键", async () => {
  const events = [];
  const mouse = new MousePrimitive();
  const page = { mouse: {
    async down() { events.push("down"); },
    async up({ button }) { events.push(`up:${button}`); },
    async move() { events.push("move"); },
  } };
  await mouse.move(page, 10, 20, 4);
  await mouse.down(page, "left");
  await mouse.move(page, 20, 30, 4);
  await mouse.up(page, "left");
  assert.deepEqual(events, ["move", "down", "up:left", "move"]);
});

/** 创建仅记录键鼠顺序的输入测试对象。 */
function inputHarness({ focused, downFails = false }) {
  const events = [];
  const action = new InputAction(
    { async one() { return { result: { element_ref: "input" }, resolved: {
      view: { box: { x: 0, y: 0, width: 100, height: 40 } },
      page: {}, locator: { async boundingBox() { return { x: 0, y: 0, width: 100, height: 40 }; }, locator(css) {
        assert.equal(css, ":scope:focus");
        return { async count() { return focused ? 1 : 0; } };
      } },
    } }; } },
    { async ensureVisible() {} }, { async toElement() { events.push("move"); } },
    { async down() { events.push("down"); if (downFails) throw new Error("按下失败"); }, async up() { events.push("up"); } },
    { async press() { events.push("key"); }, async insertText() { events.push("type"); } },
    { info() {}, failure() {} },
  );
  return { action, events };
}

test("弹框短暂停住后继续移动时，必须等最终位置连续稳定", async () => {
  const found = { resolved: { locator: {}, view: { box: { x: 10, y: 10, width: 100, height: 40 } } } };
  const positions = [10, 10, 80, 80, 80, 80];
  let reads = 0;
  await waitForStablePosition(found, { async box() { return { x: positions[reads++], y: 10, width: 100, height: 40 }; } });
  assert.equal(reads, 6);
  assert.equal(found.resolved.view.box.x, 80);
});

for (const downFails of [false, true]) {
  test(`焦点未获得或按下失败时必须释放鼠标且不能全选页面：${downFails}`, async () => {
    const { action, events } = inputHarness({ focused: false, downFails });
    await assert.rejects(action.execute({ selector: { description: "输入框" }, text: "测试" }, {
      action: "element.input", trace_id: "input-safety", started_at: Date.now(),
    }));
    assert.deepEqual(events, ["move", "down", "up"]);
  });
}
