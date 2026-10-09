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
  readOnly = false,
  onChange,
}: {
  label: string;
  value: string | boolean;
  options: ChoiceOption[];
  columns?: number;
  autoWidth?: boolean;
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
        className={`goodhr-choice-grid${autoWidth ? " goodhr-choice-auto" : ""}`}
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
                <Typography.Text strong>{option.label}</Typography.Text>
              ) : null}
              {value === option.value ? (
                <CheckCircleFilled className="goodhr-choice-check" />
              ) : null}
            </Flex>
            {!option.iconOnly ? (
              <Typography.Paragraph
                type="secondary"
                style={{ margin: "6px 0 0", fontSize: 12.5 }}
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
