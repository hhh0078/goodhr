/** 本文件负责 邀请奖励与记录 的 Ant Design 页面。 */
"use client";
import { Button, Flex, Input, Table, Tag, Typography } from "antd";
import {
  CopyOutlined,
  GiftOutlined,
  UsergroupAddOutlined,
  WalletOutlined,
} from "@ant-design/icons";
import styles from "./page.module.css";
import { useEffect, useMemo, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import {
  EmptyState,
  FormActionRow,
  PageHeader,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
const defaultDescription =
  "邀请好友注册成功后，邀请人可获得注册奖励；好友充值会员后，还可按购买月份获得额外会员天数。临时邮箱无法完成注册。";
/** InvitationsPage 展示当前用户的邀请活动、专属链接和奖励记录。 */
export default function InvitationsPage() {
  const { notify } = useAdmin();
  const [data, setData] = useState<any>({});
  const [loading, setLoading] = useState(false);
  const config = data.config || {};
  const invitees = Array.isArray(data.invitees) ? data.invitees : [];
  const inviteURL = useMemo(() => {
    const currentOrigin = "https://goodhr.58it.cn";
    const url = new URL(process.env.NEXT_PUBLIC_SITE_URL || currentOrigin);
    if (data.invite_id) url.searchParams.set("invite", data.invite_id);
    return url.toString();
  }, [data.invite_id]);
  /** load 读取邀请活动配置和被邀请用户列表。 */
  async function load() {
    setLoading(true);
    try {
      setData(await cloudRequest("/api/invitations/summary"));
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "邀请记录读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
  }, []);
  /** copyLink 将当前用户的专属邀请链接复制到剪贴板。 */
  async function copyLink() {
    try {
      await navigator.clipboard.writeText(inviteURL);
      notify("邀请链接已复制", "success");
    } catch {
      notify("复制失败，请手动复制", "error");
    }
  }
  return (
    <>
      <PageHeader
        title="邀请奖励"
        description="把 GoodHR 分享给朋友，注册和订阅都能为你增加会员时间。"
      />

      <div
        className={styles.layout12}
        style={{ display: "grid", gap: 16, marginBottom: 16 }}
      >
        <SectionPanel
          style={{
            position: "relative",
            overflow: "hidden",
            backgroundColor: "var(--goodhr-primary-soft)",
            borderColor: "var(--goodhr-primary-soft)",
          }}
        >
          <Flex gap={1.5 * 8} style={{ alignItems: "center" }}>
            <div
              style={{
                width: 44,
                height: 44,
                borderRadius: "8px",
                display: "grid",
                placeItems: "center",
                backgroundColor: "var(--goodhr-primary)",
                color: "#fff",
                flexShrink: 0,
              }}
            >
              <GiftOutlined aria-hidden="true" />
            </div>
            <div>
              <Typography.Title
                className={styles.layout2}
                level={2}
                style={{ display: "block", margin: 0, fontWeight: 790 }}
              >
                {config.activity_title || "邀请好友奖励会员天数"}
              </Typography.Title>
              <Typography.Text
                style={{
                  display: "block",
                  margin: 0,
                  marginTop: 4,
                  color: "var(--goodhr-text-secondary)",
                  lineHeight: 1.75,
                }}
              >
                {config.activity_description || defaultDescription}
              </Typography.Text>
            </div>
          </Flex>
          <div
            className={styles.layout5}
            style={{ display: "grid", gap: 12, marginTop: 24 }}
          >
            <RewardCard
              icon={<UsergroupAddOutlined aria-hidden="true" />}
              value={`${Number(config.register_reward_days || 0)} 天`}
              label="好友注册成功奖励"
            />
            <RewardCard
              icon={<WalletOutlined aria-hidden="true" />}
              value={`${Number(config.paid_month_reward_days || 0)} 天/月`}
              label="好友订阅会员奖励"
            />
          </div>
        </SectionPanel>
        <SectionPanel>
          <Typography.Text
            style={{
              display: "block",
              margin: 0,
              color: "var(--goodhr-text-secondary)",
              fontSize: 13,
            }}
          >
            邀请成果
          </Typography.Text>
          <Flex gap={4 * 8} style={{ marginTop: 12 }}>
            <div>
              <Typography.Text
                style={{
                  display: "block",
                  margin: 0,
                  fontSize: 30,
                  fontWeight: 800,
                }}
              >
                {invitees.length}
              </Typography.Text>
              <Typography.Text
                style={{
                  display: "block",
                  margin: 0,
                  color: "var(--goodhr-text-secondary)",
                }}
              >
                已邀请人数
              </Typography.Text>
            </div>
            <div>
              <Typography.Text
                style={{
                  display: "block",
                  margin: 0,
                  fontSize: 30,
                  fontWeight: 800,
                  color: "var(--goodhr-primary)",
                }}
              >
                {Number(data.reward_days || data.total_reward_days || 0)}
              </Typography.Text>
              <Typography.Text
                style={{
                  display: "block",
                  margin: 0,
                  color: "var(--goodhr-text-secondary)",
                }}
              >
                累计奖励天数
              </Typography.Text>
            </div>
          </Flex>
          <Typography.Text
            style={{
              display: "block",
              margin: 0,
              marginTop: 24,
              marginBottom: 8,
              fontWeight: 720,
            }}
          >
            我的专属邀请链接
          </Typography.Text>
          <FormActionRow
            field={
              <Input
                readOnly={true}
                style={{ width: "100%" }}
                size="small"
                value={inviteURL}
              />
            }
            action={
              <Button
                type={"primary"}
                icon={<CopyOutlined aria-hidden="true" />}
                onClick={() => void copyLink()}
              >
                复制链接
              </Button>
            }
            maxWidth="100%"
          />
        </SectionPanel>
      </div>

      <SectionPanel>
        <Flex
          justify="space-between"
          align="center"
          gap={12}
          style={{ marginBottom: 16 }}
        >
          <div>
            <Typography.Title level={5} style={{ margin: 0 }}>
              邀请记录
            </Typography.Title>
            <Typography.Text type="secondary">
              好友完成注册后，奖励状态会自动更新。
            </Typography.Text>
          </div>
          <Tag color="var(--goodhr-primary)">共 {invitees.length} 人</Tag>
        </Flex>
        <Table<any>
          rowKey={(item) => item.id || item.email}
          dataSource={invitees}
          loading={loading}
          size="small"
          pagination={{ pageSize: 10 }}
          scroll={{ x: 580 }}
          locale={{
            emptyText: (
              <EmptyState text="暂无邀请记录，复制链接邀请第一位好友吧" />
            ),
          }}
          columns={[
            {
              title: "好友邮箱",
              dataIndex: "email",
              render: (email) => email || "未显示邮箱",
            },
            { title: "注册时间", dataIndex: "created_at", render: formatDate },
            {
              title: "奖励状态",
              dataIndex: "invite_registered_rewarded_at",
              render: (rewarded) => (
                <Tag color={rewarded ? "success" : "warning"}>
                  {rewarded ? "已发注册奖励" : "等待奖励"}
                </Tag>
              ),
            },
          ]}
        />
      </SectionPanel>
    </>
  );
}
/** RewardCard 展示一种邀请奖励及其说明。 */
function RewardCard({
  icon,
  value,
  label,
}: {
  icon: React.ReactNode;
  value: string;
  label: string;
}) {
  return (
    <Flex
      gap={1.5 * 8}
      style={{
        padding: 14,
        border: "1px solid",
        borderColor: "var(--goodhr-border)",
        borderRadius: "8px",
        backgroundColor: "rgba(255,255,255,.74)",
        alignItems: "center",
      }}
    >
      <div
        style={{
          color: "var(--goodhr-primary)",
          display: "grid",
          placeItems: "center",
        }}
      >
        {icon}
      </div>
      <div>
        <Typography.Text
          style={{ display: "block", margin: 0, fontSize: 21, fontWeight: 800 }}
        >
          {value}
        </Typography.Text>
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-text-secondary)",
            fontSize: 12,
          }}
        >
          {label}
        </Typography.Text>
      </div>
    </Flex>
  );
}
