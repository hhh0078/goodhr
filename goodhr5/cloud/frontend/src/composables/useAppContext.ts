// 本文件负责在后台布局和各个页面之间共享应用状态。
import { inject, provide } from "vue";

const APP_CONTEXT_KEY = Symbol("goodhr5_app_context");

/**
 * 向子页面提供后台共享状态。
 * @param {any} context - 登录、任务、本地程序等共享状态。
 * @returns {void} 无返回值。
 */
export function provideAppContext(context: any) {
  provide(APP_CONTEXT_KEY, context);
}

/**
 * 读取后台共享状态。
 * @returns {any} 返回后台共享状态。
 */
export function useAppContext() {
  const context = inject<any>(APP_CONTEXT_KEY);
  if (!context) throw new Error("后台页面缺少共享状态");
  return context;
}
