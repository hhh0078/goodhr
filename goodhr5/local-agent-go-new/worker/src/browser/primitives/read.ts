// 文件作用说明：提供只供封装能力调用的元素文本、HTML、属性和值读取原子操作。

import type { Locator } from "playwright-core";

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

  /** editableValue 兼容读取普通输入框和富文本编辑器的当前文字。 */
  async editableValue(locator: Locator): Promise<string> {
    return this.inputValue(locator).catch(() => this.text(locator));
  }
}
