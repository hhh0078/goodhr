// 文件作用说明：在原生下载点击前记录当前标签，并用标准页面创建和导航事件等候本次文件的查看器。
import { fileURLToPath } from "node:url";
import type { BrowserContext, Frame, Page } from "playwright-core";

/** DownloadViewerWatch 只观察当前会话标签，不访问网页脚本，也不在后台自动关闭页面。 */
export class DownloadViewerWatch {
  readonly startedAt = Date.now();
  readonly before: Set<Page>;
  private readonly registered = new Map<Page, (frame: Frame) => void>();
  private wake: (() => void) | undefined;
  private readonly expire: NodeJS.Timeout;
  private closed = false;
  events = 0;

  /** 先登记已有标签，再监听新页与主框架地址变化，覆盖先出现 about:blank 再显示文件的过程。 */
  constructor(readonly origin: Page, private readonly context: BrowserContext) {
    this.before = new Set(context.pages());
    context.on("page", this.register);
    for (const page of this.before) this.register(page);
    this.events = 0;
    this.expire = setTimeout(() => this.dispose(), 120_000);
    this.expire.unref();
  }

  /** register 为新页安装标准主框架导航监听；事件只唤醒正等待的调用方。 */
  private register = (page: Page): void => {
    if (this.registered.has(page)) return;
    const navigation = (frame: Frame): void => { if (frame === page.mainFrame()) this.changed(); };
    this.registered.set(page, navigation);
    page.on("framenavigated", navigation);
    this.changed();
  };

  /** changed 记录创建或导航事件，异步事件本身不操作浏览器。 */
  private changed(): void {
    this.events++;
    this.wake?.();
  }

  /** matching 逐次按当前实际的本地绝对路径识别文件页，避免文件名相同而目录不同。 */
  private matching(filePath: string): Page[] {
    return this.context.pages().filter(page => {
      if (page.isClosed() || !page.url().startsWith("file:")) return false;
      try {
        const url = new URL(page.url());
        return !url.search && !url.hash && fileURLToPath(url) === filePath;
      } catch { return false; }
    });
  }

  /** wait 在原生回执已知后立即检查已打开的页面，仍为空才有界等待创建或导航，取消后绝不产生页面动作。 */
  async wait(filePath: string, signal?: AbortSignal, timeoutMS = 5_000): Promise<Page[]> {
    const deadline = Date.now() + timeoutMS;
    for (;;) {
      if (signal?.aborted || this.closed) throw new Error("下载标签监听已取消");
      const matched = this.matching(filePath);
      if (matched.length || Date.now() >= deadline) return matched;
      await new Promise<void>(resolve => {
        const wake = (): void => {
          clearTimeout(timer);
          signal?.removeEventListener("abort", wake);
          this.wake = undefined;
          resolve();
        };
        const timer = setTimeout(wake, deadline - Date.now());
        this.wake = wake;
        signal?.addEventListener("abort", wake, { once: true });
      });
    }
  }

  /** dispose 撤销此次点击登记的全部监听，旧请求即使稍后收到文件也无法再操作标签。 */
  dispose(): void {
    if (this.closed) return;
    this.closed = true;
    clearTimeout(this.expire);
    this.context.off("page", this.register);
    for (const [page, navigation] of this.registered) page.off("framenavigated", navigation);
    this.registered.clear();
    this.wake?.();
  }
}
