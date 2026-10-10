/** 本文件登记后台已打开的弹框，让非必要关注提醒等待其他操作完成。 */
"use client";
import { Modal } from "antd";
import {
  createContext,
  useCallback,
  useContext,
  useId,
  useLayoutEffect,
  useMemo,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";

type OverlayRegistry = {
  busy: boolean;
  register: (id: string, open: boolean) => void;
};
const OverlayContext = createContext<OverlayRegistry | null>(null);

/** AdminOverlayProvider 将弹框登记限定在当前后台实例，避免不同账号页面互相影响。 */
export function AdminOverlayProvider({ children }: { children: ReactNode }) {
  const [openIDs, setOpenIDs] = useState<Set<string>>(() => new Set());
  /** register 登记或移除一个弹框，重复登记保持原状态以免多余渲染。 */
  const register = useCallback((id: string, open: boolean) => {
    setOpenIDs((current) => {
      if (current.has(id) === open) return current;
      const next = new Set(current);
      if (open) next.add(id);
      else next.delete(id);
      return next;
    });
  }, []);
  const value = useMemo(
    () => ({ busy: openIDs.size > 0, register }),
    [openIDs, register],
  );
  return (
    <OverlayContext.Provider value={value}>{children}</OverlayContext.Provider>
  );
}

/** useAdminOverlay 在绘制前登记普通弹框；离开页面或关闭时自动解除，公开页面无需提供登记器。 */
export function useAdminOverlay(open: boolean) {
  const registry = useContext(OverlayContext);
  const id = useId();
  const register = registry?.register;
  useLayoutEffect(() => {
    if (!register || !open) return;
    register(id, true);
    return () => register(id, false);
  }, [id, open, register]);
}

/** useAdminOverlayBusy 返回其他业务弹框是否占用后台，关注提醒自身不登记。 */
export function useAdminOverlayBusy() {
  return useContext(OverlayContext)?.busy || false;
}

/** AdminOverlayActivity 登记菜单抽屉等由后台父组件直接管理的遮罩。 */
export function AdminOverlayActivity({ open }: { open: boolean }) {
  useAdminOverlay(open);
  return null;
}

/** AdminOverlayModal 保留原生 Ant Design Modal 的全部行为，并登记已有直接使用的业务弹框。 */
export function AdminOverlayModal(props: ComponentProps<typeof Modal>) {
  useAdminOverlay(Boolean(props.open));
  return <Modal {...props} />;
}
