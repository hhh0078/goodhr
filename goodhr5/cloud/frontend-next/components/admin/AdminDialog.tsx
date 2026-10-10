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
  onClose: () => void;
  onConfirm?: () => void;
  extraActions?: ReactNode;
  priority?: "normal" | "promotion";
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
  onClose,
  onConfirm,
  extraActions,
  priority = "normal",
}: AdminDialogProps) {
  useAdminOverlay(open && priority === "normal");
  return (
    <Modal
      open={open}
      title={title}
      width={dialogWidths[maxWidth]}
      className="goodhr-admin-modal"
      closable={!hideClose && !loading}
      keyboard={!loading}
      focusable={{ focusTriggerAfterClose: priority !== "promotion" }}
      mask={{ closable: !loading }}
      onCancel={() => {
        if (!loading) onClose();
      }}
      styles={{
        body: {
          maxHeight: "calc(100dvh - 200px)",
          overflowY: "auto",
          overflowX: "hidden",
          paddingBlock: 8,
        },
      }}
      footer={
        <Flex justify="flex-end" gap={8} wrap>
          {extraActions}
          {showCancel ? (
            <Button onClick={onClose} disabled={loading}>
              {cancelText}
            </Button>
          ) : null}
          {onConfirm ? (
            <Button
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
