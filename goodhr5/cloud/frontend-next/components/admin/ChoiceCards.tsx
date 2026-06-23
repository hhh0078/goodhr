/** 本文件提供后台表单中带说明的卡片单选组件。 */
"use client";

import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import LockRoundedIcon from "@mui/icons-material/LockRounded";
import { Box, ButtonBase, Typography } from "@mui/material";

export type ChoiceOption = {
  value: string | boolean;
  label: string;
  description: string;
  disabled?: boolean;
  memberOnly?: boolean;
  iconSrc?: string;
  iconOnly?: boolean;
};

/** ChoiceCards 将选项展示为可复用的响应式单选卡片。 */
export default function ChoiceCards({ label, value, options, columns = 2, onChange }: { label: string; value: string | boolean; options: ChoiceOption[]; columns?: number; onChange: (value: string | boolean) => void }) {
  return <Box><Typography sx={{ mb: 1, color: "text.secondary", fontSize: 13, fontWeight: 700 }}>{label}</Typography><Box role="radiogroup" aria-label={label} sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: `repeat(${Math.min(columns, 2)}, minmax(0, 1fr))`, lg: `repeat(${columns}, minmax(0, 1fr))` }, gap: 1 }}>
    {options.map((option) => { const selected = value === option.value; const memberStyle = option.memberOnly; return <ButtonBase key={String(option.value)} role="radio" aria-label={option.label} aria-checked={selected} disabled={option.disabled} onClick={() => onChange(option.value)} sx={{ position: "relative", display: "block", minHeight: option.iconOnly ? 76 : 98, p: option.iconOnly ? 1.25 : 1.75, border: "1px solid", borderColor: memberStyle ? (selected ? "#d6ad54" : "#8b713a") : selected ? "primary.main" : "divider", borderRadius: "8px", textAlign: "left", color: memberStyle ? "#f7e6b2" : "text.primary", bgcolor: memberStyle ? (selected ? "#242017" : "#181713") : selected ? "#edf7f1" : "#fff", boxShadow: memberStyle && selected ? "0 0 0 2px rgba(214,173,84,.18)" : "none", opacity: option.disabled ? 0.48 : 1, transition: "border-color .18s ease, background-color .18s ease, box-shadow .18s ease", "&:hover": { borderColor: option.disabled ? "divider" : memberStyle ? "#e2bd68" : "primary.main", bgcolor: memberStyle ? "#242017" : selected ? "#edf7f1" : "#fbfdfc" } }}>
      {option.iconOnly ? <Box sx={{ display: "flex", minHeight: 48, alignItems: "center", justifyContent: "center" }}>{option.iconSrc ? <Box component="img" src={option.iconSrc} alt={option.label} sx={{ maxWidth: "100%", maxHeight: 42, objectFit: "contain" }} /> : <Typography sx={{ fontWeight: 780 }}>{option.label}</Typography>}{selected ? <CheckCircleRoundedIcon sx={{ position: "absolute", right: 8, top: 8, color: "primary.main", fontSize: 20 }} /> : null}</Box> : <><Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>{option.iconSrc ? <Box component="img" src={option.iconSrc} alt="" sx={{ width: 30, height: 30, objectFit: "contain", flex: "0 0 auto" }} /> : null}<Typography sx={{ flex: 1, fontWeight: 760 }}>{option.label}</Typography>{option.memberOnly ? <LockRoundedIcon sx={{ color: "#d6ad54", fontSize: 18 }} /> : null}{selected ? <CheckCircleRoundedIcon sx={{ color: memberStyle ? "#d6ad54" : "primary.main", fontSize: 20 }} /> : null}</Box><Typography sx={{ mt: 0.75, color: memberStyle ? "#cbbd97" : "text.secondary", fontSize: 12.5, lineHeight: 1.55 }}>{option.description}</Typography></>}
    </ButtonBase>; })}
  </Box></Box>;
}
