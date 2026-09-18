// 文件作用说明：串行执行影响页面输入状态的完整动作，避免旧请求尚未松开鼠标时新请求开始移动或输入。

/** ActionQueue 确保一次只执行一个页面操作，失败不会阻断后续清理。 */
export class ActionQueue {
  private tail: Promise<void> = Promise.resolve();

  /** run 排队执行完整动作，并保留本次动作的真实成功或失败结果。 */
  run<T>(operation: () => Promise<T>): Promise<T> {
    const result = this.tail.then(operation);
    this.tail = result.then(() => undefined, () => undefined);
    return result;
  }
}
