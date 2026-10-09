/** 本文件使用 Ant Design Image 提供缩略图和可缩放的大图预览。 */
"use client";
import { Image, Typography } from "antd";
/** ClickableImagePreview 展示图片并支持标准预览操作。 */
export default function ClickableImagePreview({
  src,
  alt,
  hint = "点击图片放大查看",
}: {
  src: string;
  alt: string;
  hint?: string;
}) {
  return (
    <div className="goodhr-image-preview">
      <Image
        src={src}
        alt={alt}
        width="100%"
        style={{ maxHeight: 300, objectFit: "contain", borderRadius: 8 }}
        preview={{ mask: "放大查看" }}
      />
      <Typography.Paragraph
        type="secondary"
        style={{ margin: "6px 0 0", fontSize: 12 }}
      >
        {hint}
      </Typography.Paragraph>
    </div>
  );
}
