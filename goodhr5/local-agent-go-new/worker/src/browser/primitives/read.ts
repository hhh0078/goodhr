// 文件作用说明：提供只供封装能力调用的元素文本、HTML、属性和值读取原子操作。

import type { Locator, Page } from "playwright-core";
import type { JsonObject } from "../../contracts/common.js";

/** ScrollState 表示页面或最近可滚动父级的只读状态。 */
export interface ScrollState extends JsonObject {
  source: string;
  scroll_left: number;
  scroll_top: number;
  scroll_width: number;
  scroll_height: number;
  client_width: number;
  client_height: number;
}

/** ReadPrimitive 封装元素只读原子操作。 */
export class ReadPrimitive {
  /** text 读取元素可见文本。 */
  async text(locator: Locator): Promise<string> {
    return (await locator.innerText()).trim();
  }

  /** html 读取元素内部 HTML。 */
  async html(locator: Locator): Promise<string> {
    return locator.innerHTML();
  }

  /** attribute 读取指定元素属性。 */
  async attribute(locator: Locator, name: string): Promise<string> {
    return (await locator.getAttribute(name)) ?? "";
  }

  /** inputValue 读取输入框当前值。 */
  async inputValue(locator: Locator): Promise<string> {
    return locator.inputValue();
  }

  /** scrollState 读取滚轮落点最近可滚动父级的状态，不修改页面位置。 */
  async scrollState(page: Page, anchor?: Locator): Promise<ScrollState> {
    if (!anchor) {
      return page.evaluate(() => {
        const root = document.scrollingElement ?? document.documentElement;
        return {
          source: "page",
          scroll_left: root.scrollLeft,
          scroll_top: root.scrollTop,
          scroll_width: root.scrollWidth,
          scroll_height: root.scrollHeight,
          client_width: root.clientWidth,
          client_height: root.clientHeight,
        };
      });
    }
    return anchor.evaluate((element) => {
      let current: Element | null = element;
      while (current) {
        const html = current as HTMLElement;
        const style = window.getComputedStyle(current);
        const overflowY = style.overflowY.toLowerCase();
        const overflowX = style.overflowX.toLowerCase();
        const scrollableY =
          /(auto|scroll|overlay)/.test(overflowY) &&
          html.scrollHeight > html.clientHeight;
        const scrollableX =
          /(auto|scroll|overlay)/.test(overflowX) &&
          html.scrollWidth > html.clientWidth;
        if (scrollableY || scrollableX) {
          return {
            source: "anchor_parent",
            scroll_left: html.scrollLeft,
            scroll_top: html.scrollTop,
            scroll_width: html.scrollWidth,
            scroll_height: html.scrollHeight,
            client_width: html.clientWidth,
            client_height: html.clientHeight,
          };
        }
        current = current.parentElement;
      }
      const root = document.scrollingElement ?? document.documentElement;
      return {
        source: "page",
        scroll_left: root.scrollLeft,
        scroll_top: root.scrollTop,
        scroll_width: root.scrollWidth,
        scroll_height: root.scrollHeight,
        client_width: root.clientWidth,
        client_height: root.clientHeight,
      };
    });
  }
}
