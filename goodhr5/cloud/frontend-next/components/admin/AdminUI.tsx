/** 本文件提供 Ant Design 后台复用的标题、内容卡片、空状态和操作行。 */
"use client";
import { Card, Empty, Flex, Typography } from "antd";
import type { CSSProperties, ReactNode } from "react";
/** PageHeader 展示后台页面标题和可换行的操作按钮。 */
export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <Flex
      className="admin-page-header"
      justify="space-between"
      gap={16}
      wrap
      align="center"
    >
      <div style={{ minWidth: 0 }}>
        <Typography.Title level={2} style={{ margin: 0 }}>
          {title}
        </Typography.Title>
        {description ? (
          <Typography.Paragraph type="secondary" style={{ margin: "4px 0 0" }}>
            {description}
          </Typography.Paragraph>
        ) : null}
      </div>
      {actions ? (
        <Flex className="goodhr-toolbar-actions" gap={8} wrap>
          {actions}
        </Flex>
      ) : null}
    </Flex>
  );
}
/** SectionPanel 用标准卡片展示后台内容，接受 CSS 样式调整布局。 */
export function SectionPanel({
  children,
  style,
  className,
}: {
  children: ReactNode;
  style?: CSSProperties;
  className?: string;
}) {
  return (
    <Card
      className={`goodhr-section-panel ${className || ""}`}
      style={style}
      styles={{ body: { display: "contents" } }}
    >
      {children}
    </Card>
  );
}
/** EmptyState 展示空数据说明和下一步操作。 */
export function EmptyState({
  text = "暂无数据",
  action,
}: {
  text?: string;
  action?: ReactNode;
}) {
  return (
    <Empty
      image={Empty.PRESENTED_IMAGE_SIMPLE}
      description={text}
      style={{ paddingBlock: 32 }}
    >
      {action}
    </Empty>
  );
}
/** FormActionRow 保证输入框与操作按钮在窄屏下仍可使用。 */
export function FormActionRow({
  field,
  action,
  maxWidth = 620,
}: {
  field: ReactNode;
  action: ReactNode;
  maxWidth?: number | string;
}) {
  return (
    <Flex
      className="admin-form-action-row"
      gap={8}
      align="start"
      wrap
      style={{ width: "100%", maxWidth }}
    >
      <div style={{ flex: "1 1 220px", minWidth: 0 }}>{field}</div>
      <div style={{ flexShrink: 0 }}>{action}</div>
    </Flex>
  );
}
