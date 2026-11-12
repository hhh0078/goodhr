// 文件作用说明：提供只供封装能力调用的元素文本、HTML、属性和值读取原子操作。

import type { Locator } from "playwright-core";
import { readWhileActive } from "./request-lifecycle.js";

/** elementReadTimeoutMS 是每次 Playwright 原生取值实际使用的预算。 */
export const elementReadTimeoutMS = 1500;

/** ReadPrimitive 封装元素只读原子操作。 */
export class ReadPrimitive {
  /** focused 通过标准 CSS 焦点伪类确认当前 Locator 获得键盘焦点，不执行页面脚本。 */
  async focused(locator: Locator): Promise<boolean> {
    return (await readWhileActive(() => locator.locator(":scope:focus").count())) === 1;
  }

  /** text 读取元素可见文本。 */
  async text(locator: Locator, timeoutMS = elementReadTimeoutMS): Promise<string> {
    return (await readWhileActive(() => locator.innerText({ timeout: timeoutMS }))).trim();
  }

  /** html 读取元素内部 HTML。 */
  async html(locator: Locator, timeoutMS = elementReadTimeoutMS): Promise<string> {
    return readWhileActive(() => locator.innerHTML({ timeout: timeoutMS }));
  }

  /** attribute 读取指定元素属性。 */
  async attribute(locator: Locator, name: string, timeoutMS = elementReadTimeoutMS): Promise<string> {
    return (await readWhileActive(() => locator.getAttribute(name, { timeout: timeoutMS }))) ?? "";
  }

  /** inputValue 读取输入框当前值。 */
  async inputValue(locator: Locator, timeoutMS = elementReadTimeoutMS): Promise<string> {
    return readWhileActive(() => locator.inputValue({ timeout: timeoutMS }));
  }

  /** editableValue 读取原始草稿，富文本保留换行和空格，缺失、超时和取消仍报错。 */
  async editableValue(locator: Locator, timeoutMS = elementReadTimeoutMS): Promise<string> {
    try {
      return await this.inputValue(locator, timeoutMS);
    } catch (error) {
      // 复用 Playwright 的明确类型错误，不能把读取失败或断线误当成已清空的草稿。
      if (!(error instanceof Error) || error.name === "TimeoutError" || !error.message.includes("Node is not an <input>, <textarea> or <select> element")) throw error;
      const value = await readWhileActive(() => locator.textContent({ timeout: timeoutMS }));
      if (value === null) throw new Error("输入框原始草稿尚未读到，暂不继续发送");
      return value;
    }
  }
}
