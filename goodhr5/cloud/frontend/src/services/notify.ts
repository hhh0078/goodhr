/** 本文件负责提供全局轻提醒和确认弹框状态。 */
import { reactive } from "vue";

export type NotifyKind = "success" | "error";

export type ToastItem = {
  id: number;
  kind: NotifyKind;
  message: string;
};

export type ConfirmState = {
  visible: boolean;
  title: string;
  message: string;
  kind: NotifyKind;
  confirmText: string;
  cancelText: string;
  showCancel: boolean;
  resolver: ((value: boolean) => void) | null;
};

let nextToastID = 1;

export const notifyState = reactive({
  toasts: [] as ToastItem[],
  confirm: {
    visible: false,
    title: "",
    message: "",
    kind: "error" as NotifyKind,
    confirmText: "确认",
    cancelText: "取消",
    showCancel: true,
    resolver: null,
  } as ConfirmState,
});

/**
 * 显示右上角轻提醒。
 * @param {NotifyKind} kind - 提醒类型。
 * @param {string} message - 展示内容。
 * @returns {void} 无返回值。
 */
export function notify(kind: NotifyKind, message: string) {
  const text = String(message || "").trim();
  if (!text) return;
  const item = { id: nextToastID++, kind, message: text };
  notifyState.toasts.unshift(item);
  window.setTimeout(() => removeToast(item.id), 3000);
}

/**
 * 显示成功轻提醒。
 * @param {string} message - 展示内容。
 * @returns {void} 无返回值。
 */
export function notifySuccess(message: string) {
  notify("success", message);
}

/**
 * 显示错误轻提醒。
 * @param {string} message - 展示内容。
 * @returns {void} 无返回值。
 */
export function notifyError(message: string) {
  notify("error", message);
}

/**
 * 显示需要用户确认的居中弹框。
 * @param {string} message - 展示内容。
 * @param {Partial<Omit<ConfirmState, "visible" | "message" | "resolver">>} options - 弹框配置。
 * @returns {Promise<boolean>} 返回用户是否确认。
 */
export function confirmDialog(
  message: string,
  options: Partial<Omit<ConfirmState, "visible" | "message" | "resolver">> = {},
) {
  closeConfirm(false);
  return new Promise<boolean>((resolve) => {
    notifyState.confirm = {
      visible: true,
      title: options.title || "请确认",
      message,
      kind: options.kind || "error",
      confirmText: options.confirmText || "确认",
      cancelText: options.cancelText || "取消",
      showCancel: options.showCancel !== false,
      resolver: resolve,
    };
  });
}

/**
 * 显示只需要确认的错误弹框。
 * @param {string} message - 展示内容。
 * @returns {Promise<boolean>} 返回用户确认结果。
 */
export function alertError(message: string) {
  return confirmDialog(message, {
    title: "操作失败",
    kind: "error",
    confirmText: "知道了",
    showCancel: false,
  });
}

/**
 * 关闭右上角轻提醒。
 * @param {number} id - 提醒 ID。
 * @returns {void} 无返回值。
 */
export function removeToast(id: number) {
  notifyState.toasts = notifyState.toasts.filter((item) => item.id !== id);
}

/**
 * 关闭确认弹框。
 * @param {boolean} value - 用户选择结果。
 * @returns {void} 无返回值。
 */
export function closeConfirm(value: boolean) {
  const resolver = notifyState.confirm.resolver;
  notifyState.confirm.visible = false;
  notifyState.confirm.resolver = null;
  if (resolver) resolver(value);
}
