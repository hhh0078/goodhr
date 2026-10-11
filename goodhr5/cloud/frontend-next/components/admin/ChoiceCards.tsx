/** 本文件用 Ant Design Radio 提供后台响应式卡片单选。 */
"use client";
import { CheckCircleFilled } from "@ant-design/icons";
import { Flex, Image, Radio, Typography } from "antd";
import type { CSSProperties } from "react";
export type ChoiceOption = {
  value: string | boolean;
  label: string;
  description: string;
  disabled?: boolean;
  memberOnly?: boolean;
  iconSrc?: string;
  iconOnly?: boolean;
};
/** ChoiceCards 保留布尔和字符串选项，用原生单选支持键盘操作。 */
export default function ChoiceCards({
  label,
  value,
  options,
  columns = 2,
  autoWidth = false,
  compact = false,
  readOnly = false,
  onChange,
}: {
  label: string;
  value: string | boolean;
  options: ChoiceOption[];
  columns?: number;
  autoWidth?: boolean;
  compact?: boolean;
  readOnly?: boolean;
  onChange: (value: string | boolean) => void;
}) {
  return (
    <div>
      <Typography.Paragraph
        type="secondary"
        style={{ marginBottom: 8, fontSize: 13 }}
      >
        {label}
      </Typography.Paragraph>
      <Radio.Group
        aria-label={label}
        value={value}
        disabled={readOnly}
        onChange={(event) => onChange(event.target.value)}
        className={`goodhr-choice-grid${autoWidth ? " goodhr-choice-auto" : ""}${compact ? " goodhr-choice-compact" : ""}`}
        style={{ "--choice-columns": Math.max(1, columns) } as CSSProperties}
      >
        {options.map((option) => (
          <Radio
            key={String(option.value)}
            value={option.value}
            disabled={option.disabled}
            className={`goodhr-choice${option.memberOnly ? " goodhr-choice-member" : ""}${value === option.value ? " goodhr-choice-selected" : ""}${option.iconOnly ? " goodhr-choice-icon" : ""}`}
          >
            <Flex
              gap={8}
              align="center"
              justify={option.iconOnly ? "center" : undefined}
            >
              {option.iconSrc ? (
                <Image
                  preview={false}
                  src={option.iconSrc}
                  alt={option.iconOnly ? option.label : ""}
                  width={option.iconOnly ? undefined : 30}
                  height={option.iconOnly ? 42 : 30}
                  style={{ objectFit: "contain", maxWidth: "100%" }}
                />
              ) : null}
              {!option.iconOnly || !option.iconSrc ? (
                <Typography.Text strong style={compact ? { fontSize: 13 } : undefined}>{option.label}</Typography.Text>
              ) : null}
              {value === option.value && !compact ? (
                <CheckCircleFilled
                  className="goodhr-choice-check"
                  aria-hidden="true"
                />
              ) : null}
            </Flex>
            {!option.iconOnly ? (
              <Typography.Paragraph
                type="secondary"
                style={{ margin: compact ? "2px 0 0" : "6px 0 0", fontSize: compact ? 12 : 12.5 }}
              >
                {option.description}
              </Typography.Paragraph>
            ) : null}
          </Radio>
        ))}
      </Radio.Group>
    </div>
  );
}
