/** 本文件复用手绘切图；人物按官网主题切换，流程插画采用同一套水彩线稿。 */

import Image from "next/image";

export type PublicIllustrationKind = "hero" | "analysis" | "greeting" | "followup" | "interview";

/** PublicIllustration 输出不承载文字的装饰插画，并为图片预留固定比例以防页面跳动。 */
export default function PublicIllustration({ kind = "hero", className = "", priority = false }: {
  kind?: PublicIllustrationKind;
  className?: string;
  priority?: boolean;
}) {
  const filename = kind === "hero" ? "hero-green" : `workflow-${kind}`;
  const roseFilename = kind === "hero" ? "hero-rose" : `workflow-${kind}-rose`;
  return <div className={`public-illustration public-illustration-${kind} ${className}`} aria-hidden="true">
    <Image src={`/illustrations/${filename}.webp`}
      alt="" width={1254} height={1254} sizes={kind === "hero" ? "(max-width: 699px) 90vw, (max-width: 1100px) 45vw, 640px" : "(max-width: 699px) 150px, 240px"}
      priority={priority} loading={priority ? "eager" : undefined} />
    <Image src={`/illustrations/${roseFilename}.webp`} className="public-illustration-rose"
      alt="" width={1254} height={1254} sizes={kind === "hero" ? "(max-width: 699px) 90vw, (max-width: 1100px) 45vw, 640px" : "(max-width: 699px) 150px, 240px"}
      priority={priority} loading={priority ? "eager" : undefined} />
  </div>;
}
