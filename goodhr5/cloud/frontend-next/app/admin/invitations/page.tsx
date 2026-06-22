/** 本文件负责新版后台邀请活动、邀请链接、奖励数据和邀请记录展示。 */
"use client";

import CardGiftcardRoundedIcon from "@mui/icons-material/CardGiftcardRounded";
import ContentCopyRoundedIcon from "@mui/icons-material/ContentCopyRounded";
import GroupAddRoundedIcon from "@mui/icons-material/GroupAddRounded";
import PaymentsRoundedIcon from "@mui/icons-material/PaymentsRounded";
import { Box, Button, Chip, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useMemo, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import {
  EmptyState,
  FormActionRow,
  PageHeader,
  RefreshButton,
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
    const currentOrigin = "https://goodhr5.58it.cn";
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
        title='邀请奖励'
        description='把 GoodHR 分享给朋友，注册和订阅都能为你增加会员时间。'
        actions={
          <RefreshButton loading={loading} onClick={() => void load()} />
        }
      />

      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: {
            xs: "1fr",
            lg: "minmax(0, 1.45fr) minmax(320px, .75fr)",
          },
          gap: 2,
          mb: 2,
        }}
      >
        <SectionPanel
          sx={{
            position: "relative",
            overflow: "hidden",
            bgcolor: "#f3f8f4",
            borderColor: "#cfe0d3",
          }}
        >
          <Stack direction='row' spacing={1.5} sx={{ alignItems: "center" }}>
            <Box
              sx={{
                width: 44,
                height: 44,
                borderRadius: "8px",
                display: "grid",
                placeItems: "center",
                bgcolor: "#1e6545",
                color: "white",
                flexShrink: 0,
              }}
            >
              <CardGiftcardRoundedIcon />
            </Box>
            <Box>
              <Typography
                component='h2'
                sx={{ fontSize: { xs: 20, md: 23 }, fontWeight: 790 }}
              >
                {config.activity_title || "邀请好友奖励会员天数"}
              </Typography>
              <Typography
                sx={{ mt: 0.5, color: "text.secondary", lineHeight: 1.75 }}
              >
                {config.activity_description || defaultDescription}
              </Typography>
            </Box>
          </Stack>
          <Box
            sx={{
              display: "grid",
              gridTemplateColumns: {
                xs: "1fr",
                sm: "repeat(2, minmax(0, 1fr))",
              },
              gap: 1.5,
              mt: 3,
            }}
          >
            <RewardCard
              icon={<GroupAddRoundedIcon />}
              value={`${Number(config.register_reward_days || 0)} 天`}
              label='好友注册成功奖励'
            />
            <RewardCard
              icon={<PaymentsRoundedIcon />}
              value={`${Number(config.paid_month_reward_days || 0)} 天/月`}
              label='好友订阅会员奖励'
            />
          </Box>
        </SectionPanel>

        <SectionPanel>
          <Typography sx={{ color: "text.secondary", fontSize: 13 }}>
            邀请成果
          </Typography>
          <Stack direction='row' spacing={4} sx={{ mt: 1.5 }}>
            <Box>
              <Typography sx={{ fontSize: 30, fontWeight: 800 }}>
                {invitees.length}
              </Typography>
              <Typography color='text.secondary'>已邀请人数</Typography>
            </Box>
            <Box>
              <Typography
                sx={{ fontSize: 30, fontWeight: 800, color: "#1e6545" }}
              >
                {Number(data.reward_days || data.total_reward_days || 0)}
              </Typography>
              <Typography color='text.secondary'>累计奖励天数</Typography>
            </Box>
          </Stack>
          <Typography sx={{ mt: 3, mb: 1, fontWeight: 720 }}>
            我的专属邀请链接
          </Typography>
          <FormActionRow
            field={
              <TextField
                size='small'
                value={inviteURL}
                fullWidth
                slotProps={{ input: { readOnly: true } }}
              />
            }
            action={
              <Button
                variant='contained'
                startIcon={<ContentCopyRoundedIcon />}
                onClick={() => void copyLink()}
              >
                复制链接
              </Button>
            }
            maxWidth='100%'
          />
        </SectionPanel>
      </Box>

      <SectionPanel>
        <Stack
          direction='row'
          sx={{
            alignItems: "center",
            justifyContent: "space-between",
            mb: invitees.length ? 0.5 : 0,
          }}
        >
          <Box>
            <Typography component='h2' sx={{ fontSize: 19, fontWeight: 760 }}>
              邀请记录
            </Typography>
            <Typography sx={{ mt: 0.4, color: "text.secondary", fontSize: 13 }}>
              好友完成注册后，奖励状态会自动更新。
            </Typography>
          </Box>
          <Chip
            size='small'
            label={`共 ${invitees.length} 人`}
            sx={{ bgcolor: "#edf5ef", color: "#1e6545" }}
          />
        </Stack>
        {invitees.length ? (
          <Stack>
            {invitees.map((item: any) => (
              <Stack
                key={item.id || item.email}
                direction={{ xs: "column", sm: "row" }}
                spacing={2}
                sx={{
                  py: 2,
                  borderBottom: "1px solid",
                  borderColor: "divider",
                  alignItems: { sm: "center" },
                  justifyContent: "space-between",
                }}
              >
                <Box sx={{ minWidth: 0 }}>
                  <Typography noWrap sx={{ fontWeight: 700 }}>
                    {item.email || "未显示邮箱"}
                  </Typography>
                  <Typography sx={{ color: "text.secondary", fontSize: 12 }}>
                    注册时间：{formatDate(item.created_at)}
                  </Typography>
                </Box>
                <Chip
                  size='small'
                  color={
                    item.invite_registered_rewarded_at ? "success" : "warning"
                  }
                  label={
                    item.invite_registered_rewarded_at
                      ? "已发注册奖励"
                      : "等待奖励"
                  }
                />
              </Stack>
            ))}
          </Stack>
        ) : (
          <EmptyState
            text={
              loading
                ? "正在读取邀请记录"
                : "暂无邀请记录，复制链接邀请第一位好友吧"
            }
          />
        )}
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
    <Stack
      direction='row'
      spacing={1.5}
      sx={{
        p: 1.75,
        border: "1px solid #d8e6db",
        borderRadius: "8px",
        bgcolor: "rgba(255,255,255,.74)",
        alignItems: "center",
      }}
    >
      <Box sx={{ color: "#1e6545", display: "grid", placeItems: "center" }}>
        {icon}
      </Box>
      <Box>
        <Typography sx={{ fontSize: 21, fontWeight: 800 }}>{value}</Typography>
        <Typography sx={{ color: "text.secondary", fontSize: 12 }}>
          {label}
        </Typography>
      </Box>
    </Stack>
  );
}
