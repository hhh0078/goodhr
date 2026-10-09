/** 本文件负责 会员激活码管理 的 Ant Design 页面。 */
"use client";
import {
  Button,
  Flex,
  Form,
  Input,
  InputNumber,
  Table,
  Tag,
  Typography,
} from "antd";
import { CopyOutlined, PlusOutlined } from "@ant-design/icons";
import styles from "./page.module.css";
import { useEffect, useState } from "react";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import AdminDialog from "@/components/admin/AdminDialog";
/** ActivationCodesPage 管理会员激活码。 */
export default function ActivationCodesPage() {
  const { user, notify } = useAdmin();
  const [codes, setCodes] = useState<any[]>([]);
  const [generated, setGenerated] = useState<any[]>([]);
  const [form, setForm] = useState({ days: 30, count: 1, remark: "" });
  const [loading, setLoading] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  /** load 读取全部会员激活码。 */
  async function load() {
    setLoading(true);
    try {
      const data = await cloudRequest("/api/admin/activation-codes");
      setCodes(data.codes || []);
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "激活码读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    if (user?.role === "super_admin") void load();
  }, [user]);
  /** createCodes 批量生成会员激活码。 */
  async function createCodes() {
    if (creating) return;
    if (!Number.isInteger(form.days) || form.days < 1)
      return notify("会员天数要填大于 0 的整数", "warning");
    if (!Number.isInteger(form.count) || form.count < 1 || form.count > 200)
      return notify("生成数量请填 1 到 200 之间的整数", "warning");
    setCreating(true);
    try {
      const data = await cloudRequest("/api/admin/activation-codes", {
        method: "POST",
        body: form,
      });
      setGenerated(data.codes || []);
      setDialogOpen(false);
      notify(`已生成 ${(data.codes || []).length} 个激活码`, "success");
      await load();
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "生成激活码失败",
        "error",
      );
    } finally {
      setCreating(false);
    }
  }
  /** copyGenerated 复制本次生成的全部激活码。 */
  async function copyGenerated() {
    try {
      await navigator.clipboard.writeText(
        generated.map((item) => item.code).join("\n"),
      );
      notify("激活码已复制", "success");
    } catch {
      notify("复制失败", "error");
    }
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
        title="激活码管理"
        description="激活码统一赠送 Max 全能版，包含自动回复。"
        actions={
          <>
            <Button
              type={"primary"}
              icon={<PlusOutlined aria-hidden="true" />}
              onClick={() => setDialogOpen(true)}
            >
              生成激活码
            </Button>
            <RefreshButton loading={loading} onClick={() => void load()} />
          </>
        }
      />
      {generated.length ? (
        <SectionPanel style={{ marginBottom: 16 }}>
          <Flex
            className={
              styles.layout1 + " " + styles.layout2 + " " + styles.layout1
            }
            gap={1 * 8}
            style={{ justifyContent: "space-between" }}
          >
            <Typography.Text
              style={{
                display: "block",
                margin: 0,
                color: "var(--goodhr-text-secondary)",
              }}
            >
              本次生成：{generated.map((item) => item.code).join("、")}
            </Typography.Text>
            <Button
              icon={<CopyOutlined aria-hidden="true" />}
              onClick={() => void copyGenerated()}
            >
              复制全部
            </Button>
          </Flex>
        </SectionPanel>
      ) : null}
      <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
        <Table
          rowKey={(item) => item.id || item.code}
          dataSource={codes}
          loading={loading}
          size="small"
          pagination={{ pageSize: 20 }}
          scroll={{ x: 900 }}
          columns={[
            {
              title: "激活码",
              dataIndex: "code",
              render: (code) => (
                <Typography.Text code copyable>
                  {code}
                </Typography.Text>
              ),
            },
            {
              title: "会员天数",
              dataIndex: "days",
              render: (days) => days + " 天 Max",
            },
            {
              title: "状态",
              dataIndex: "status",
              render: (status) => (
                <Tag color={status === "used" ? undefined : "success"}>
                  {status === "used" ? "已使用" : "未使用"}
                </Tag>
              ),
            },
            {
              title: "使用人 / 备注",
              key: "remark",
              render: (_, item) => item.used_by_email || item.remark || "--",
            },
            {
              title: "时间",
              key: "date",
              render: (_, item) => formatDate(item.used_at || item.created_at),
            },
          ]}
        />
      </SectionPanel>
      <AdminDialog
        open={dialogOpen}
        title="批量生成 Max 激活码"
        confirmText="生成"
        loading={creating}
        onClose={() => setDialogOpen(false)}
        onConfirm={() => void createCodes()}
      >
        <Flex vertical={true} gap={2 * 8}>
          <Form.Item
            htmlFor="goodhr-activation-codes-field-1"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"Max 会员天数"}
          >
            <InputNumber
              id="goodhr-activation-codes-field-1"
              min={1}
              step={1}
              style={{ width: "100%" }}
              value={form.days}
              onChange={(value) =>
                setForm({ ...form, days: Number(value ?? 0) })
              }
            />
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-activation-codes-field-2"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"生成数量"}
            extra={"单次最多生成 200 个。"}
          >
            <InputNumber
              id="goodhr-activation-codes-field-2"
              min={1}
              step={1}
              max={200}
              style={{ width: "100%" }}
              value={form.count}
              onChange={(value) =>
                setForm({ ...form, count: Number(value ?? 0) })
              }
            />
          </Form.Item>
          <Form.Item
            htmlFor="goodhr-activation-codes-field-3"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"备注"}
          >
            <Input
              id="goodhr-activation-codes-field-3"
              style={{ width: "100%" }}
              value={form.remark}
              onChange={(event) =>
                setForm({ ...form, remark: event.target.value })
              }
            />
          </Form.Item>
        </Flex>
      </AdminDialog>
    </>
  );
}
