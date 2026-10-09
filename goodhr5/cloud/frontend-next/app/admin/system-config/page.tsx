/** 本文件负责 系统配置编辑 的 Ant Design 页面。 */
"use client";
import { Alert, Button, Flex, Menu, Tabs, Tag, Typography } from "antd";
import { SaveOutlined, UndoOutlined } from "@ant-design/icons";
import styles from "./page.module.css";
import { useEffect, useMemo, useState } from "react";
import JsonEditor from "@/components/admin/JsonEditor";
import OnboardingRewardField, {
  validateOnboardingReward,
} from "@/components/admin/OnboardingRewardField";
import WechatOfficialConfigForm, {
  officialConfigForSave,
  parseOfficialConfig,
  validateOfficialConfig,
} from "@/components/admin/WechatOfficialConfigForm";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest } from "@/lib/admin-api";
const categories = [
  "全部",
  "AI 配置",
  "基础配置",
  "订阅支付",
  "本地组件",
  "邀请帮助",
  "其他配置",
] as const;
/** SystemConfigPage 管理系统配置并按用途分类。 */
export default function SystemConfigPage() {
  const { user, notify, confirm, refreshWechatOfficialStatus } = useAdmin();
  const [configs, setConfigs] = useState<any[]>([]);
  const [category, setCategory] = useState<(typeof categories)[number]>("全部");
  const [activeKey, setActiveKey] = useState("");
  const [draft, setDraft] = useState("{}");
  const [original, setOriginal] = useState("{}");
  const [loading, setLoading] = useState(false);
  /** load 读取全部系统配置并保持当前选中项。 */
  async function load() {
    setLoading(true);
    try {
      const data = await cloudRequest("/api/admin/system/configs/");
      const nextConfigs = data.configs || [];
      setConfigs(nextConfigs);
      const selected =
        nextConfigs.find((item: any) => configKey(item) === activeKey) ||
        nextConfigs[0];
      if (selected) selectDirect(selected);
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "系统配置读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    if (user?.role === "super_admin") void load();
  }, [user]);
  const filtered = useMemo(
    () =>
      configs.filter(
        (item) =>
          category === "全部" || configCategory(configKey(item)) === category,
      ),
    [configs, category],
  );
  const active = configs.find((item) => configKey(item) === activeKey) || null;
  const dirty = draft !== original;
  const isWechatConfig = activeKey === "system.wechat_official_account";
  const isOnboardingConfig = activeKey === "system.onboarding_config";
  const jsonError =
    validateJSON(draft) ||
    (isWechatConfig ? validateOfficialConfig(draft) : "") ||
    (isOnboardingConfig ? validateOnboardingReward(draft) : "");
  /** selectDirect 不经确认直接切换当前配置。 */
  function selectDirect(item: any) {
    const value = prettyJSON(item.config_value ?? item.value);
    setActiveKey(configKey(item));
    setDraft(value);
    setOriginal(value);
  }
  /** selectConfig 在存在未保存修改时确认是否放弃。 */
  async function selectConfig(item: any) {
    if (configKey(item) === activeKey) return;
    if (
      dirty &&
      !(await confirm(
        "放弃未保存修改",
        "当前配置有未保存修改，确认切换到其他配置吗？",
      ))
    )
      return;
    selectDirect(item);
  }
  /** save 校验并保存当前系统配置。 */
  async function save() {
    if (!active || jsonError)
      return notify(jsonError || "请选择系统配置", "warning");
    setLoading(true);
    try {
      const formatted = isWechatConfig
        ? officialConfigForSave(draft)
        : prettyJSON(draft);
      await cloudRequest(
        `/api/admin/system/configs/${encodeURIComponent(activeKey)}`,
        { method: "PUT", body: { config_value: formatted } },
      );
      setDraft(formatted);
      setOriginal(formatted);
      notify(`${configTitle(activeKey)}已保存`, "success");
      await load();
      if (isWechatConfig || isOnboardingConfig)
        await refreshWechatOfficialStatus();
    } catch (error) {
      notify(error instanceof Error ? error.message : "保存失败", "error");
    } finally {
      setLoading(false);
    }
  }
  /** resetDraft 恢复当前配置的服务端内容。 */
  function resetDraft() {
    setDraft(original);
  }
  if (user?.role !== "super_admin")
    return (
      <SectionPanel>
        <EmptyState text="只有超级管理员可以访问此页面" />
      </SectionPanel>
    );
  return (
    <>
      <PageHeader
        title="系统配置"
        description="配置按业务用途分组。保存前会校验 JSON，错误内容不会提交。"
        actions={
          <>
            <RefreshButton loading={loading} onClick={() => void load()} />
            <Button
              icon={<UndoOutlined aria-hidden="true" />}
              disabled={!dirty || loading}
              onClick={resetDraft}
            >
              撤销修改
            </Button>
            <Button
              type={"primary"}
              icon={<SaveOutlined aria-hidden="true" />}
              disabled={!active || !dirty || Boolean(jsonError) || loading}
              onClick={() => void save()}
            >
              保存当前配置
            </Button>
          </>
        }
      />
      <Tabs
        activeKey={category}
        onChange={(value) => setCategory(value as (typeof categories)[number])}
        items={categories.map((item) => ({ key: item, label: item }))}
      />
      {configs.length ? (
        <div className={styles.layout14} style={{ display: "grid", gap: 16 }}>
          <Menu
            mode="inline"
            selectedKeys={[activeKey]}
            style={{ borderInlineEnd: 0 }}
            onClick={({ key }) => {
              const item = filtered.find((entry) => configKey(entry) === key);
              if (item) void selectConfig(item);
            }}
            items={filtered.map((item) => ({
              key: configKey(item),
              style: { height: "auto", lineHeight: "normal" },
              label: (
                <Flex vertical gap={4} style={{ padding: "8px 0" }}>
                  <Typography.Text strong>
                    {configTitle(configKey(item))}
                  </Typography.Text>
                  <Typography.Text
                    type="secondary"
                    style={{
                      fontSize: 12,
                      whiteSpace: "normal",
                      lineHeight: 1.5,
                    }}
                  >
                    {item.description || configKey(item)}
                  </Typography.Text>
                </Flex>
              ),
            }))}
          />
          <SectionPanel>
            {active ? (
              <>
                <Flex
                  className={
                    styles.layout8 + " " + styles.layout9 + " " + styles.layout8
                  }
                  gap={1.5 * 8}
                  style={{ marginBottom: 16, justifyContent: "space-between" }}
                >
                  <div>
                    <Typography.Title
                      level={2}
                      style={{
                        display: "block",
                        margin: 0,
                        fontSize: 20,
                        fontWeight: 780,
                      }}
                    >
                      {configTitle(activeKey)}
                    </Typography.Title>
                    <Typography.Text
                      style={{
                        display: "block",
                        margin: 0,
                        marginTop: 4,
                        color: "var(--goodhr-text-secondary)",
                        fontSize: 13,
                      }}
                    >
                      {active.description || activeKey}
                    </Typography.Text>
                    <Typography.Text
                      style={{
                        display: "block",
                        margin: 0,
                        marginTop: 4,
                        color: "var(--goodhr-text-secondary)",
                        fontFamily: "monospace",
                        fontSize: 11,
                      }}
                    >
                      {activeKey}
                    </Typography.Text>
                  </div>
                  <Flex gap={1 * 8}>
                    <Tag
                      color={
                        (
                          isWechatConfig
                            ? parseOfficialConfig(draft).enabled !== true
                            : active.enabled === false
                        )
                          ? undefined
                          : "success"
                      }
                    >
                      {(
                        isWechatConfig
                          ? parseOfficialConfig(draft).enabled !== true
                          : active.enabled === false
                      )
                        ? "已停用"
                        : "已启用"}
                    </Tag>
                    {dirty ? (
                      <Tag color={"warning"}>{"有未保存修改"}</Tag>
                    ) : null}
                  </Flex>
                </Flex>
                {activeKey === "ai.default_prompts" ? (
                  <Alert
                    type={"info"}
                    showIcon={true}
                    title={
                      <>
                        这里分别保存首次筛选、打开详情和最终复核使用的系统默认提示词。岗位模板留空时会读取这些值。
                      </>
                    }
                    style={{ marginBottom: 16 }}
                  />
                ) : null}
                {activeKey === "system.subscription_plans" ? (
                  <Alert
                    type={"info"}
                    showIcon={true}
                    title={
                      <>
                        每个套餐都要保留 member_type 和 allow_auto_reply。Plus
                        基础版设为 false，Max 全能版设为
                        true；保存时后端也会再次校验。
                      </>
                    }
                    style={{ marginBottom: 16 }}
                  />
                ) : null}
                {activeKey === "system.payment_wechat" ? (
                  <Alert
                    type={"info"}
                    showIcon={true}
                    title={
                      <>
                        商户私钥和微信支付公钥请填写容器内文件路径，例如
                        /run/secrets/wechat/apiclient_key.pem。替换文件后会自动重新加载，不用复制
                        Base64。
                      </>
                    }
                    style={{ marginBottom: 16 }}
                  />
                ) : null}
                {jsonError ? (
                  <Alert
                    type={"error"}
                    showIcon={true}
                    title={<>{jsonError}</>}
                    style={{ marginBottom: 16 }}
                  />
                ) : null}
                {isWechatConfig ? (
                  <WechatOfficialConfigForm
                    value={draft}
                    onChange={setDraft}
                    notify={notify}
                  />
                ) : (
                  <>
                    {isOnboardingConfig ? (
                      <OnboardingRewardField
                        value={draft}
                        onChange={setDraft}
                      />
                    ) : null}
                    <JsonEditor value={draft} onChange={setDraft} />
                  </>
                )}
              </>
            ) : (
              <EmptyState text="请选择一项配置" />
            )}
          </SectionPanel>
        </div>
      ) : (
        <SectionPanel>
          <EmptyState text="暂无系统配置" />
        </SectionPanel>
      )}
    </>
  );
}
/** configKey 返回配置记录的统一键名。 */
function configKey(item: any) {
  return String(item?.config_key || item?.key || "");
}
/** configCategory 根据配置键返回业务分类。 */
function configCategory(key: string): (typeof categories)[number] {
  if (key === "system.wechat_official_account") return "其他配置";
  if (key.startsWith("ai.") || key.includes("prompt")) return "AI 配置";
  if (key.includes("subscription") || key.includes("payment"))
    return "订阅支付";
  if (
    key.includes("onboarding") ||
    key.includes("runtime") ||
    key.includes("agent")
  )
    return "本地组件";
  if (key.includes("invite") || key.includes("guide") || key.includes("help"))
    return "邀请帮助";
  return "基础配置";
}
/** configTitle 返回系统配置的中文标题。 */
function configTitle(key: string) {
  return (
    (
      {
        "ai.default_prompts": "AI 默认提示词",
        "system.app_config": "公共系统配置",
        "system.subscription_plans": "订阅套餐",
        "system.onboarding_config": "本地程序与组件",
        "system.invite_config": "邀请奖励",
        "system.email_recovery": "自动挽回邮件",
        "system.guide": "帮助中心",
        "system.payment_wechat": "微信支付配置",
        "system.wechat_official_account": "公众号配置",
      } as Record<string, string>
    )[key] || key
  );
}
/** prettyJSON 将任意配置值格式化为缩进 JSON。 */
function prettyJSON(value: unknown) {
  try {
    const parsed = typeof value === "string" ? JSON.parse(value) : value;
    return JSON.stringify(parsed ?? {}, null, 2);
  } catch {
    return String(value || "");
  }
}
/** validateJSON 校验 JSON 文本并返回中文错误。 */
function validateJSON(value: string) {
  try {
    JSON.parse(value || "{}");
    return "";
  } catch (error) {
    return `JSON 语法错误：${error instanceof Error ? error.message : "格式不正确"}`;
  }
}
