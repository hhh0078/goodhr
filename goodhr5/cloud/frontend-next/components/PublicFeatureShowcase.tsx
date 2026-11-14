/** 本文件将官网功能展示为可切换的手绘场景和折叠说明，原文及链接保持可抓取。 */
"use client";

import { useState } from "react";
import Link from "next/link";
import PlusOutlined from "@ant-design/icons/lib/icons/PlusOutlined";
import PublicIllustration, { type PublicIllustrationKind } from "./PublicIllustration";

type FeatureScene = { title: string; description: string; href: string };
const illustrations: PublicIllustrationKind[] = ["analysis", "greeting", "followup", "analysis"];

/** PublicFeatureShowcase 根据展开的功能联动插画，复用原生问答的鼠标、触屏和键盘交互。 */
export default function PublicFeatureShowcase({ scenes }: { scenes: FeatureScene[] }) {
  const [active, setActive] = useState(0);
  if (!scenes.length) return null;
  return <div className="public-feature-showcase">
    <div className="public-feature-stage" aria-hidden="true">
      {scenes.map((scene, index) => <div className="public-feature-picture" key={scene.title} hidden={active !== index}>
        <PublicIllustration kind={illustrations[index] || "analysis"} />
      </div>)}
      <div className="public-feature-progress">{scenes.map((scene, index) => <span key={scene.title} data-active={index === active || undefined} />)}</div>
    </div>
    <div className="public-feature-list">
      {scenes.map((scene, index) => <details className="public-feature-item" name="public-feature-scenes" key={scene.title} open={active === index}
        onToggle={(event) => { if (event.currentTarget.open) setActive(index); }}>
        <summary><h3>{scene.title}</h3><PlusOutlined aria-hidden="true" /></summary>
        <div className="public-feature-copy">
          <p>{scene.description}</p>
          <Link href={scene.href} className="public-topic-link">了解{scene.title.replace("（开发中）", "")} →</Link>
        </div>
      </details>)}
    </div>
  </div>;
}
