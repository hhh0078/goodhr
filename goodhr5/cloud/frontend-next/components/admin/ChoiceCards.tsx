/** 本文件提供后台表单中带说明的卡片单选组件。 */
"use client";

import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import LockRoundedIcon from "@mui/icons-material/LockRounded";
import { Box, ButtonBase, Typography } from "@mui/material";

export type ChoiceOption = { value: string | boolean; label: string; description: string; disabled?: boolean; memberOnly?: boolean };

/** ChoiceCards 将选项展示为可复用的响应式单选卡片。 */
export default function ChoiceCards({ label, value, options, columns = 2, onChange }: { label: string; value: string | boolean; options: ChoiceOption[]; columns?: number; onChange: (value: string | boolean) => void }) {
  return <Box><Typography sx={{ mb: 1, color: "text.secondary", fontSize: 13, fontWeight: 700 }}>{label}</Typography><Box role="radiogroup" aria-label={label} sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: `repeat(${Math.min(columns, 2)}, minmax(0, 1fr))`, lg: `repeat(${columns}, minmax(0, 1fr))` }, gap: 1 }}>
    {options.map((option) => { const selected = value === option.value; return <ButtonBase key={String(option.value)} role="radio" aria-checked={selected} disabled={option.disabled} onClick={() => onChange(option.value)} sx={{ position: "relative", display: "block", minHeight: 98, p: 1.75, border: "1px solid", borderColor: selected ? "primary.main" : "divider", borderRadius: "8px", textAlign: "left", bgcolor: selected ? "#edf7f1" : "#fff", opacity: option.disabled ? 0.48 : 1, transition: "border-color .18s ease, background-color .18s ease", "&:hover": { borderColor: option.disabled ? "divider" : "primary.main" } }}>
      <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}><Typography sx={{ flex: 1, fontWeight: 760 }}>{option.label}</Typography>{option.memberOnly ? <LockRoundedIcon sx={{ color: "warning.main", fontSize: 18 }} /> : null}{selected ? <CheckCircleRoundedIcon color="primary" sx={{ fontSize: 20 }} /> : null}</Box><Typography sx={{ mt: 0.75, color: "text.secondary", fontSize: 12.5, lineHeight: 1.55 }}>{option.description}</Typography>
    </ButtonBase>; })}
  </Box></Box>;
}
