/** 本文件提供基于 Ant Design Modal 的后台表单和确认弹窗。 */
"use client";
import { Button, Flex, Modal, Typography } from "antd";
import { LoadingOutlined } from "@ant-design/icons";
import type { ReactNode } from "react";
import { useAdminOverlay } from "./AdminOverlay";
type AdminDialogProps = {
  open: boolean;
  title: ReactNode;
  description?: string;
  children: ReactNode;
  confirmText?: string;
  cancelText?: string;
  loading?: boolean;
  loadingText?: string;
  confirmDisabled?: boolean;
  /** 仅让确认按钮等待请求，允许非阻塞提醒在生成期间继续取消。 */
  confirmLoading?: boolean;
  hideClose?: boolean;
  showCancel?: boolean;
  maxWidth?: "xs" | "sm" | "md" | "lg" | "xl";
  /** 详情使用接近全屏的双栏容器，表单继续沿用默认布局。 */
  variant?: "form" | "detail";
  onClose: () => void;
  onConfirm?: () => void;
  extraActions?: ReactNode;
  priority?: "normal" | "promotion";
  /** 给真实确认按钮提供稳定的新手引导定位，不模拟点击或提交。 */
  confirmDataOnboarding?: string;
};
const dialogWidths = { xs: 444, sm: 600, md: 900, lg: 1200, xl: 1536 };
/** AdminDialog 在提交期间阻止关闭和重复提交，并让长表单只滚动内容区。 */
export default function AdminDialog({
  open,
  title,
  description,
  children,
  confirmText = "保存",
  cancelText = "取消",
  loading = false,
  loadingText,
  confirmDisabled = false,
  confirmLoading = false,
  hideClose = false,
  showCancel = true,
  maxWidth = "sm",
  variant = "form",
  onClose,
  onConfirm,
  extraActions,
  priority = "normal",
  confirmDataOnboarding,
}: AdminDialogProps) {
  useAdminOverlay(open && priority === "normal");
  return (
    <Modal
      open={open}
      title={title}
      width={variant === "detail" ? "min(1480px, calc(100vw - 32px))" : dialogWidths[maxWidth]}
      className={`goodhr-admin-modal${variant === "detail" ? " goodhr-detail-dialog" : ""}`}
      style={variant === "detail" ? { top: 16, paddingBottom: 0 } : undefined}
      destroyOnHidden={variant === "detail"}
      zIndex={/* 详情低于编辑与确认弹框，避免复用的子弹框被详情门户盖住。 */ variant === "detail" ? 900 : undefined}
      closable={!hideClose && !loading}
      keyboard={!loading}
      focusable={{ focusTriggerAfterClose: priority !== "promotion" }}
      mask={{ closable: !loading }}
      onCancel={() => {
        if (!loading) onClose();
      }}
      styles={{
        body: variant === "detail" ? {
          height: "calc(100dvh - 32px)",
          maxHeight: "calc(100dvh - 32px)",
          overflow: "hidden",
          padding: 0,
        } : {
          maxHeight: "min(calc(100dvh - 200px), var(--goodhr-onboarding-modal-body-height, 100dvh))",
          overflowY: "auto",
          overflowX: "hidden",
          paddingBlock: 8,
        },
      }}
      footer={
        !showCancel && !onConfirm && !extraActions ? null : <Flex justify="flex-end" gap={8} wrap>
          {extraActions}
          {showCancel ? (
            <Button onClick={onClose} disabled={loading}>
              {cancelText}
            </Button>
          ) : null}
          {onConfirm ? (
            <Button
              data-onboarding={confirmDataOnboarding}
              type="primary"
              onClick={onConfirm}
              loading={
                loading || confirmLoading
                  ? { icon: <LoadingOutlined aria-hidden="true" /> }
                  : false
              }
              disabled={loading || confirmLoading || confirmDisabled}
            >
              {loading ? loadingText || "处理中" : confirmText}
            </Button>
          ) : null}
        </Flex>
      }
    >
      {description ? (
        <Typography.Paragraph type="secondary">
          {description}
        </Typography.Paragraph>
      ) : null}
      {children}
    </Modal>
  );
}
