/** 本文件提供个人配置页的邮件通知画像收集弹框。 */
"use client";

import CheckCircleRoundedIcon from "@mui/icons-material/CheckCircleRounded";
import CloseRoundedIcon from "@mui/icons-material/CloseRounded";
import FavoriteBorderRoundedIcon from "@mui/icons-material/FavoriteBorderRounded";
import NotificationsActiveRoundedIcon from "@mui/icons-material/NotificationsActiveRounded";
import PersonSearchRoundedIcon from "@mui/icons-material/PersonSearchRounded";
import { Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, IconButton, Stack, Typography } from "@mui/material";
import type { ReactNode } from "react";
import { useEffect, useMemo, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import { useAdmin } from "@/components/admin/AdminApp";

const userTypes = [
  { value: "headhunter", label: "猎头" },
  { value: "hr", label: "企业HR" },
  { value: "recruiting_manager", label: "招聘负责人" },
  { value: "owner", label: "老板/管理者" },
];

const genders = [
  { value: "female", label: "女" },
  { value: "male", label: "男" },
  { value: "unknown", label: "不方便说" },
];

const platforms = ["BOSS直聘", "智联招聘", "智联猎头端", "前程无忧", "猎聘", "脉脉", "LinkedIn"];

type NotificationProfile = {
  completed?: boolean;
  dismissed_at?: string | null;
  user_type?: string;
  gender?: string;
  platforms?: string[];
  os?: string;
  browser?: string;
};

/** NotificationProfileDialog 渲染轻量邮件通知画像收集流程。 */
export default function NotificationProfileDialog() {
  const { notify } = useAdmin();
  const [open, setOpen] = useState(false);
  const [step, setStep] = useState(0);
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState<NotificationProfile>({ user_type: "hr", gender: "female", platforms: [] });
  const device = useMemo(() => detectDevice(), []);

  useEffect(() => {
    let alive = true;
    /** loadProfile 读取用户通知画像，未填写且未取消时打开弹框。 */
    async function loadProfile() {
      try {
        const data = await cloudRequest("/api/config/notification-profile");
        const profile: NotificationProfile = data.profile || {};
        if (!alive) return;
        setForm({
          user_type: profile.user_type || "hr",
          gender: profile.gender || "female",
          platforms: profile.platforms || [],
          os: profile.os || device.os,
          browser: profile.browser || device.browser,
        });
        setOpen(!profile.completed && !profile.dismissed_at);
      } catch {
        if (alive) setOpen(false);
      }
    }
    void loadProfile();
    return () => {
      alive = false;
    };
  }, [device.browser, device.os]);

  /** cancel 记录用户本次跳过，避免重复打扰。 */
  async function cancel() {
    setLoading(true);
    try {
      await cloudRequest("/api/config/notification-profile", { method: "PUT", body: { ...form, os: device.os, browser: device.browser, dismissed: true } });
      setOpen(false);
    } catch (error) {
      notify(error instanceof Error ? error.message : "我没记住这次跳过，稍后再试一下", "error");
    } finally {
      setLoading(false);
    }
  }

  /** next 进入下一步，表单页会先保存用户画像。 */
  async function next() {
    if (step === 0) {
      setStep(1);
      return;
    }
    if (step === 2) {
      setOpen(false);
      return;
    }
    if (!form.user_type) return notify("你是哪路英雄，稍微选一下我才不乱发邮件", "warning");
    setLoading(true);
    try {
      await cloudRequest("/api/config/notification-profile", { method: "PUT", body: { ...form, os: device.os, browser: device.browser, completed: true } });
      setStep(2);
    } catch (error) {
      notify(error instanceof Error ? error.message : "保存失败了，我先尴尬一下", "error");
    } finally {
      setLoading(false);
    }
  }

  /** togglePlatform 切换一个常用招聘平台。 */
  function togglePlatform(platform: string) {
    setForm((current) => {
      const currentPlatforms = current.platforms || [];
      const nextPlatforms = currentPlatforms.includes(platform) ? currentPlatforms.filter((item) => item !== platform) : [...currentPlatforms, platform];
      return { ...current, platforms: nextPlatforms };
    });
  }

  return (
    <Dialog open={open} fullWidth maxWidth="sm" onClose={loading ? undefined : cancel} slotProps={{ paper: { sx: { borderRadius: "8px", overflow: "hidden" } } }}>
      <Box sx={{ position: "relative", bgcolor: "#f7fbf6", borderBottom: "1px solid", borderColor: "divider" }}>
        <IconButton aria-label="关闭" disabled={loading} onClick={() => void cancel()} sx={{ position: "absolute", top: 12, right: 12, zIndex: 1 }}>
          <CloseRoundedIcon />
        </IconButton>
        <Box sx={{ px: { xs: 2.25, sm: 3 }, pt: 3, pb: 2.5 }}>
          <Stack direction="row" spacing={1.25} sx={{ alignItems: "center" }}>
            <Box sx={{ width: 42, height: 42, borderRadius: "8px", display: "grid", placeItems: "center", bgcolor: "#2f6f4f", color: "white", animation: "profilePulse 1.8s ease-in-out infinite" }}>
              {step === 2 ? <CheckCircleRoundedIcon /> : <NotificationsActiveRoundedIcon />}
            </Box>
            <Box>
              <Typography component="h2" sx={{ fontSize: 22, fontWeight: 780 }}>小小打扰一下</Typography>
              <Typography sx={{ color: "text.secondary", fontSize: 13 }}>这事关系到以后少给你发废话邮件。</Typography>
            </Box>
          </Stack>
        </Box>
      </Box>

      <DialogContent sx={{ px: { xs: 2.25, sm: 3 }, py: 3, minHeight: 300 }}>
        {step === 0 ? <IntroStep /> : null}
        {step === 1 ? <FormStep form={form} setForm={setForm} togglePlatform={togglePlatform} /> : null}
        {step === 2 ? <DoneStep /> : null}
      </DialogContent>

      <DialogActions sx={{ px: { xs: 2.25, sm: 3 }, py: 2, justifyContent: "space-between", borderTop: "1px solid", borderColor: "divider" }}>
        <Button disabled={loading} onClick={() => void cancel()}>取消</Button>
        <Button variant="contained" disabled={loading} onClick={() => void next()} sx={{ bgcolor: "#2f6f4f", "&:hover": { bgcolor: "#285f44" } }}>
          {loading ? <CircularProgress size={18} color="inherit" /> : step === 2 ? "完成" : "下一步"}
        </Button>
      </DialogActions>

      <Box component="style">{`
        @keyframes profilePulse {
          0%, 100% { transform: translateY(0) scale(1); }
          50% { transform: translateY(-2px) scale(1.04); }
        }
      `}</Box>
    </Dialog>
  );
}

/** IntroStep 展示收集说明。 */
function IntroStep() {
  return <Stack spacing={2.5} sx={{ animation: "profilePulse 2.4s ease-in-out 1" }}><Typography sx={{ fontSize: 18, fontWeight: 760 }}>真的只要 10 秒，我先小声跪一下。</Typography><Typography sx={{ color: "text.secondary", lineHeight: 1.9 }}>我们想知道你大概是谁、常用哪些招聘平台。以后 BOSS 更新就别烦不用 BOSS 的人，Mac 版上线也别误伤 Windows 朋友。</Typography><Box sx={{ p: 2, borderRadius: "8px", bgcolor: "#fff8e8", border: "1px solid #f2dfb8" }}><Typography sx={{ fontWeight: 720 }}>你也可以不填。</Typography><Typography sx={{ mt: 0.75, color: "text.secondary", lineHeight: 1.8 }}>只是这样的话，我可能只能把所有系统更新都发给你。不是我想吵，是我真的不知道你用啥。</Typography></Box></Stack>;
}

/** FormStep 展示身份、性别和常用平台选择。 */
function FormStep({ form, setForm, togglePlatform }: { form: NotificationProfile; setForm: (value: NotificationProfile | ((current: NotificationProfile) => NotificationProfile)) => void; togglePlatform: (platform: string) => void }) {
  return <Stack spacing={2.5}><ChoiceGroup icon={<PersonSearchRoundedIcon />} title="你大概是哪类用户？" value={form.user_type || "hr"} options={userTypes} onChange={(value) => setForm((current) => ({ ...current, user_type: value }))} /><ChoiceGroup icon={<FavoriteBorderRoundedIcon />} title="性别" value={form.gender || "female"} options={genders} onChange={(value) => setForm((current) => ({ ...current, gender: value }))} /><Box><Typography sx={{ mb: 1, fontWeight: 760 }}>常用招聘平台，多选</Typography><Stack direction="row" spacing={1} sx={{ flexWrap: "wrap", rowGap: 1 }}>{platforms.map((platform) => <Chip key={platform} clickable label={platform} color={(form.platforms || []).includes(platform) ? "success" : "default"} variant={(form.platforms || []).includes(platform) ? "filled" : "outlined"} onClick={() => togglePlatform(platform)} sx={{ borderRadius: "8px", px: 0.5 }} />)}</Stack></Box></Stack>;
}

/** DoneStep 展示保存完成提示。 */
function DoneStep() {
  return <Stack spacing={2.25} sx={{ textAlign: "center", alignItems: "center", py: 3 }}><CheckCircleRoundedIcon sx={{ fontSize: 58, color: "#2f6f4f", animation: "profilePulse 1.2s ease-in-out 1" }} /><Typography sx={{ fontSize: 22, fontWeight: 780 }}>感谢赏脸，我记住了。</Typography><Typography sx={{ maxWidth: 420, color: "text.secondary", lineHeight: 1.9 }}>以后我们会尽量只发你可能真的用得上的通知。不保证完全不打扰，但我会努力当个有分寸的系统。</Typography></Stack>;
}

/** ChoiceGroup 渲染单选按钮组。 */
function ChoiceGroup({ icon, title, value, options, onChange }: { icon: ReactNode; title: string; value: string; options: { value: string; label: string }[]; onChange: (value: string) => void }) {
  return <Box><Stack direction="row" spacing={1} sx={{ mb: 1, alignItems: "center" }}>{icon}<Typography sx={{ fontWeight: 760 }}>{title}</Typography></Stack><Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr 1fr", sm: "repeat(4, 1fr)" }, gap: 1 }}>{options.map((option) => <Button key={option.value} variant={value === option.value ? "contained" : "outlined"} onClick={() => onChange(option.value)} sx={{ minHeight: 42, borderRadius: "8px", color: value === option.value ? "white" : "text.primary", bgcolor: value === option.value ? "#2f6f4f" : "transparent", borderColor: value === option.value ? "#2f6f4f" : "divider", "&:hover": { bgcolor: value === option.value ? "#285f44" : "#f7fbf6", borderColor: "#2f6f4f" } }}>{option.label}</Button>)}</Box></Box>;
}

/** detectDevice 识别当前电脑系统和浏览器。 */
function detectDevice() {
  if (typeof navigator === "undefined") return { os: "", browser: "" };
  const ua = navigator.userAgent;
  const os = /Macintosh|Mac OS/i.test(ua) ? "Mac" : /Windows/i.test(ua) ? "Windows" : /Linux/i.test(ua) ? "Linux" : "";
  const browser = /Edg\//i.test(ua) ? "Edge" : /Chrome\//i.test(ua) ? "Chrome" : /Safari\//i.test(ua) ? "Safari" : "";
  return { os, browser };
}
