// 文件作用说明：提供只供封装能力调用的鼠标移动、点击和自然滚轮基础操作。

import type { Page } from "playwright-core";

/** NaturalWheelResult 表示自然滚轮实际发送的事件和耗时。 */
export interface NaturalWheelResult {
  events: number;
  distance: number;
  corrected: boolean;
  duration_ms: number;
}

/** MousePrimitive 封装 Playwright 鼠标基础操作，不查找元素也不决定流程。 */
export class MousePrimitive {
  /** move 把鼠标移动到指定坐标。 */
  async move(
    page: Page,
    x: number,
    y: number,
  ): Promise<void> {
    await page.mouse.move(x, y);
  }

  /** click 使用 CloakBrowser 增强后的坐标单击并返回完整点击耗时。 */
  async click(page: Page, x: number, y: number): Promise<number> {
    const startedAt = Date.now();
    await page.mouse.click(x, y);
    return Date.now() - startedAt;
  }

  /** down 按下指定鼠标按键。 */
  async down(
    page: Page,
    button: "left" | "right" | "middle",
  ): Promise<void> {
    await page.mouse.down({ button });
  }

  /** up 松开指定鼠标按键。 */
  async up(
    page: Page,
    button: "left" | "right" | "middle",
  ): Promise<void> {
    await page.mouse.up({ button });
  }

  /** wheel 发送真实鼠标滚轮事件。 */
  async wheel(page: Page, deltaX: number, deltaY: number): Promise<void> {
    await page.mouse.wheel(deltaX, deltaY);
  }

  /** wheelNaturally 把目标距离拆成带加速、减速和少量回调修正的真实滚轮事件。 */
  async wheelNaturally(
    page: Page,
    distance: number,
  ): Promise<NaturalWheelResult> {
    const startedAt = Date.now();
    const deltas = naturalWheelDeltas(distance);
    for (const [index, delta] of deltas.entries()) {
      await this.wheel(page, 0, delta);
      if (index + 1 < deltas.length) {
        await delay(naturalWheelPause(index, deltas.length));
      }
    }
    const direction = Math.sign(distance);
    return {
      events: deltas.length,
      distance: deltas.reduce((sum, delta) => sum + delta, 0),
      corrected: deltas.some((delta) => Math.sign(delta) === -direction),
      duration_ms: Date.now() - startedAt,
    };
  }
}

/** naturalWheelDeltas 生成总距离不变的自然滚轮脉冲序列。 */
export function naturalWheelDeltas(distance: number): number[] {
  const target = Math.trunc(distance);
  if (target === 0) {
    return [];
  }
  const direction = Math.sign(target);
  const total = Math.abs(target);
  const deltas: number[] = [];
  let sent = 0;
  while (sent < total) {
    const progress = sent / total;
    const speed = 0.45 + Math.sin(Math.PI * progress) * 0.55;
    const randomFactor = randomBetween(0.85, 1.15);
    const desired = Math.max(8, Math.round((18 + 26 * speed) * randomFactor));
    const pulse = Math.min(total - sent, desired);
    deltas.push(pulse * direction);
    sent += pulse;
  }
  if (total >= 240 && Math.random() < 0.1) {
    const overshoot = Math.min(
      70,
      Math.max(30, Math.round(total * randomBetween(0.04, 0.09))),
    );
    deltas.push(overshoot * direction);
    let remaining = overshoot;
    while (remaining > 0) {
      const correction = Math.min(remaining, randomInteger(20, 40));
      deltas.push(-correction * direction);
      remaining -= correction;
    }
  }
  return deltas;
}

/** naturalWheelPause 根据脉冲位置生成起步和收尾稍慢的停顿。 */
function naturalWheelPause(index: number, total: number): number {
  if (total <= 1) {
    return 0;
  }
  const progress = index / (total - 1);
  const edge = Math.abs(progress - 0.5) * 2;
  return randomInteger(8 + Math.round(edge * 14), 18 + Math.round(edge * 24));
}

/** delay 使用 Node 定时器等待相邻真实滚轮事件。 */
function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

/** randomBetween 返回两个边界之间的随机小数。 */
function randomBetween(minimum: number, maximum: number): number {
  return minimum + Math.random() * (maximum - minimum);
}

/** randomInteger 返回包含边界的随机整数。 */
function randomInteger(minimum: number, maximum: number): number {
  return Math.round(randomBetween(minimum, maximum));
}
