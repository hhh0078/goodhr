/** 本文件提供个人配置页的邮件通知画像收集弹框。 */
"use client";

import { CheckCircleFilled } from "@ant-design/icons";
import {
  Alert,
  Checkbox,
  Flex,
  Form,
  Radio,
  Result,
  Steps,
  Typography,
} from "antd";
import AdminDialog from "./AdminDialog";
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

const platforms = [
  "BOSS直聘",
  "智联招聘",
  "猎聘猎头端",
  "前程无忧",
  "猎聘",
  "脉脉",
  "LinkedIn",
];

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
export default function NotificationProfileDialog({
  openSignal = 0,
}: {
  openSignal?: number;
}) {
  const { notify } = useAdmin();
  const [open, setOpen] = useState(false);
  const [step, setStep] = useState(0);
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState<NotificationProfile>({
    user_type: "hr",
    gender: "female",
    platforms: [],
  });
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

  useEffect(() => {
    if (!openSignal) return;
    setStep(1);
    setOpen(true);
  }, [openSignal]);

  /** cancel 记录用户本次跳过，避免重复打扰。 */
  async function cancel() {
    setLoading(true);
    try {
      await cloudRequest("/api/config/notification-profile", {
        method: "PUT",
        body: {
          ...form,
          os: device.os,
          browser: device.browser,
          dismissed: true,
        },
      });
      setOpen(false);
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "我没记住这次跳过，稍后再试一下",
        "error",
      );
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
    if (!form.user_type)
      return notify("你是哪路英雄，稍微选一下我才不乱发邮件", "warning");
    setLoading(true);
    try {
      await cloudRequest("/api/config/notification-profile", {
        method: "PUT",
        body: {
          ...form,
          os: device.os,
          browser: device.browser,
          completed: true,
        },
      });
      setStep(2);
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "保存失败了，我先尴尬一下",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }

  /** togglePlatform 切换一个常用招聘平台。 */
  function togglePlatform(platform: string) {
    setForm((current) => {
      const currentPlatforms = current.platforms || [];
      const nextPlatforms = currentPlatforms.includes(platform)
        ? currentPlatforms.filter((item) => item !== platform)
        : [...currentPlatforms, platform];
      return { ...current, platforms: nextPlatforms };
    });
  }

  return (
    <AdminDialog
      open={open}
      title="小小打扰一下"
      description="这事关系到以后少给你发废话邮件。"
      confirmText={step === 2 ? "完成" : "下一步"}
      loading={loading}
      onClose={() => void cancel()}
      onConfirm={() => void next()}
    >
      <Steps
        size="small"
        current={step}
        items={[{ title: "说明" }, { title: "选择" }, { title: "完成" }]}
        style={{ marginBottom: 24 }}
      />
      {step === 0 ? <IntroStep /> : null}
      {step === 1 ? (
        <FormStep
          form={form}
          setForm={setForm}
          togglePlatform={togglePlatform}
        />
      ) : null}
      {step === 2 ? <DoneStep /> : null}
    </AdminDialog>
  );
}
/** IntroStep 展示通知画像的收集说明。 */
function IntroStep() {
  return (
    <Flex vertical gap={16}>
      <Typography.Title level={4}>
        真的只要 10 秒，我先小声跪一下。
      </Typography.Title>
      <Typography.Paragraph type="secondary">
        我们想知道你大概是谁、常用哪些招聘平台。以后 BOSS 更新就别烦不用 BOSS
        的人，Mac 版上线也别误伤 Windows 朋友。
      </Typography.Paragraph>
      <Alert
        type="warning"
        title="你也可以不填。"
        description="只是这样的话，我可能只能把所有系统更新都发给你。不是我想吵，是我真的不知道你用啥。"
      />
    </Flex>
  );
}
/** FormStep 用标准单选和多选组件收集身份与招聘平台。 */
function FormStep({
  form,
  setForm,
}: {
  form: NotificationProfile;
  setForm: (
    value:
      | NotificationProfile
      | ((current: NotificationProfile) => NotificationProfile),
  ) => void;
  togglePlatform: (platform: string) => void;
}) {
  return (
    <Form layout="vertical">
      <Form.Item label="你大概是哪类用户？">
        <Radio.Group
          value={form.user_type || "hr"}
          options={userTypes}
          onChange={(event) =>
            setForm((current) => ({
              ...current,
              user_type: event.target.value,
            }))
          }
        />
      </Form.Item>
      <Form.Item label="性别">
        <Radio.Group
          value={form.gender || "female"}
          options={genders}
          onChange={(event) =>
            setForm((current) => ({ ...current, gender: event.target.value }))
          }
        />
      </Form.Item>
      <Form.Item label="常用招聘平台，多选">
        <Checkbox.Group
          value={form.platforms || []}
          options={platforms}
          onChange={(value) =>
            setForm((current) => ({ ...current, platforms: value.map(String) }))
          }
        />
      </Form.Item>
    </Form>
  );
}
/** DoneStep 展示画像保存完成后的反馈。 */
function DoneStep() {
  return (
    <Result
      icon={<CheckCircleFilled style={{ color: "var(--goodhr-primary)" }} />}
      title="感谢赏脸，我记住了。"
      subTitle="以后我们会尽量只发你可能真的用得上的通知。不保证完全不打扰，但我会努力当个有分寸的系统。"
    />
  );
}

/** detectDevice 识别当前电脑系统和浏览器。 */
function detectDevice() {
  if (typeof navigator === "undefined") return { os: "", browser: "" };
  const ua = navigator.userAgent;
  const os = /Macintosh|Mac OS/i.test(ua)
    ? "Mac"
    : /Windows/i.test(ua)
      ? "Windows"
      : /Linux/i.test(ua)
        ? "Linux"
        : "";
  const browser = /Edg\//i.test(ua)
    ? "Edge"
    : /Chrome\//i.test(ua)
      ? "Chrome"
      : /Safari\//i.test(ua)
        ? "Safari"
        : "";
  return { os, browser };
}
