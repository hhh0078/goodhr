/** 本文件负责 平台账号管理与登录 的 Ant Design 页面。 */
"use client";
import { Alert, Button, Flex, Form, Input, Table, Tag, Typography } from "antd";
import {
  DeleteOutlined,
  ExportOutlined,
  LoginOutlined,
  PlusOutlined,
} from "@ant-design/icons";
import { useEffect, useState } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import ChoiceCards from "@/components/admin/ChoiceCards";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import PlatformLogo, {
  platformIconSrc,
  platformLabel,
} from "@/components/admin/PlatformLogo";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import {
  openPlatformBrowser,
  openPlatformLoginBrowser,
  pickPlatformAuthConfig,
  waitForPlatformLoggedIn,
} from "@/lib/platform-login";
const CHROMIUM_ICON_SRC = "/assets/platforms/camoufox.png";
/** AccountsPage 管理云端账号信息和本地浏览器资料目录。 */
export default function AccountsPage() {
  const { agentBase, notify, confirm } = useAdmin();
  const [accounts, setAccounts] = useState<any[]>([]);
  const [platforms, setPlatforms] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [loginStatus, setLoginStatus] = useState("");
  const [form, setForm] = useState({ platform_id: "boss", display_name: "" });
  /** load 读取平台账号和平台公开配置。 */
  async function load() {
    setLoading(true);
    try {
      const [accountData, platformData] = await Promise.all([
        cloudRequest("/api/platform-accounts"),
        cloudRequest("/api/platforms/config/", { auth: false }),
      ]);
      setAccounts(accountData.accounts || []);
      setPlatforms(platformData.platforms || platformData.configs || []);
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "平台账号加载失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
  }, []);
  /** create 创建云端账号并等待本地浏览器登录确认。 */
  async function create() {
    if (!form.display_name.trim()) return notify("请填写账号名称", "warning");
    if (!agentBase) return notify("请先启动本地程序", "error");
    setLoading(true);
    setLoginStatus("正在创建平台账号...");
    try {
      const data = await cloudRequest("/api/platform-accounts/create", {
        method: "POST",
        body: form,
      });
      const created = data.account || data;
      setLoginStatus("账号已创建，正在打开浏览器...");
      await loginWithAccount(created);
      setForm({ ...form, display_name: "" });
      setShowForm(false);
      setLoginStatus("");
      notify("账号登录确认成功", "success");
      await load();
    } catch (error) {
      setLoginStatus(error instanceof Error ? error.message : "创建账号失败");
      notify(error instanceof Error ? error.message : "创建账号失败", "error");
    } finally {
      setLoading(false);
    }
  }
  /** openAccount 使用云端账号 ID 作为本地浏览器资料目录打开平台入口页。 */
  async function openAccount(account: any) {
    if (!agentBase) throw new Error("请先启动本地程序");
    const auth = pickPlatformAuthConfig(platforms, account.platform_id);
    await openPlatformBrowser(agentBase, account, auth);
  }
  /** loginWithAccount 打开浏览器并连续确认平台账号已登录。 */
  async function loginWithAccount(account: any) {
    if (!agentBase) throw new Error("请先启动本地程序");
    const auth = pickPlatformAuthConfig(platforms, account.platform_id);
    await openPlatformLoginBrowser(agentBase, account, auth);
    setLoginStatus(
      "请点击任务栏里的橙色狐狸浏览器图标，打开浏览器并完成登录。",
    );
    await waitForPlatformLoggedIn(agentBase, auth, setLoginStatus);
  }
  /** relogin 重新打开指定账号并等待登录确认。 */
  async function relogin(account: any) {
    setLoading(true);
    setLoginStatus("正在打开浏览器...");
    try {
      await loginWithAccount(account);
      setLoginStatus("");
      notify("账号登录确认成功", "success");
      await load();
    } catch (error) {
      setLoginStatus(error instanceof Error ? error.message : "重新登录失败");
      notify(error instanceof Error ? error.message : "重新登录失败", "error");
    } finally {
      setLoading(false);
    }
  }
  /** remove 删除指定平台账号。 */
  async function remove(account: any) {
    if (
      !(await confirm(
        "删除平台账号",
        `确认删除“${account.display_name || account.id}”吗？本地浏览器目录不会自动删除。`,
      ))
    )
      return;
    try {
      await cloudRequest(`/api/platform-accounts/${account.id}`, {
        method: "DELETE",
      });
      notify("账号已删除", "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "删除失败", "error");
    }
  }
  return (
    <>
      <PageHeader
        title="平台账号"
        description="云端保存账号名称，本地程序保存招聘平台登录状态。"
        actions={
          <>
            <Button
              type={"primary"}
              icon={<PlusOutlined aria-hidden="true" />}
              onClick={() => setShowForm(true)}
            >
              新增账号
            </Button>
            <RefreshButton loading={loading} onClick={() => void load()} />
          </>
        }
      />
      <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
        <Table
          rowKey="id"
          dataSource={accounts}
          loading={loading}
          size="small"
          pagination={false}
          scroll={{ x: 820 }}
          locale={{ emptyText: <EmptyState text="暂无平台账号" /> }}
          columns={[
            {
              title: "账号",
              key: "account",
              render: (_, account) => (
                <Flex align="center" gap={12}>
                  <PlatformLogo platformID={account.platform_id} size={36} />
                  <Flex vertical gap={4}>
                    <Typography.Text strong>
                      {account.display_name || "未命名账号"}
                    </Typography.Text>
                    <Tag>{platformLabel(account.platform_id)}</Tag>
                  </Flex>
                </Flex>
              ),
            },
            {
              title: "状态",
              dataIndex: "status",
              render: (status) => (
                <Tag color={status === "available" ? "success" : "warning"}>
                  {status === "available" ? "已创建" : "需要登录"}
                </Tag>
              ),
            },
            { title: "更新时间", dataIndex: "updated_at", render: formatDate },
            {
              title: "操作",
              key: "actions",
              width: 280,
              render: (_, account) => (
                <Flex wrap gap={8}>
                  <Button
                    icon={<ExportOutlined aria-hidden="true" />}
                    onClick={() =>
                      void openAccount(account)
                        .then(() => notify("浏览器已打开", "success"))
                        .catch((error) => notify(error.message, "error"))
                    }
                  >
                    打开
                  </Button>
                  <Button
                    icon={<LoginOutlined aria-hidden="true" />}
                    disabled={loading}
                    onClick={() => void relogin(account)}
                  >
                    重新登录
                  </Button>
                  <Button
                    danger
                    icon={<DeleteOutlined aria-hidden="true" />}
                    onClick={() => void remove(account)}
                  >
                    删除
                  </Button>
                </Flex>
              ),
            },
          ]}
        />
      </SectionPanel>
      <AdminDialog
        open={showForm}
        title="新增平台账号"
        description="创建后会打开本地浏览器，请在招聘平台中完成登录。"
        confirmText="创建并登录"
        loading={loading}
        confirmDisabled={!form.display_name.trim()}
        onClose={() => {
          setShowForm(false);
          setLoginStatus("");
        }}
        onConfirm={() => void create()}
      >
        <Flex vertical={true} gap={2.5 * 8}>
          <ChoiceCards
            label="招聘平台"
            value={form.platform_id}
            columns={3}
            autoWidth
            onChange={(value) =>
              setForm({ ...form, platform_id: String(value) })
            }
            options={[
              {
                value: "boss",
                label: "Boss直聘",
                description: "当前主要支持的平台。",
                iconSrc: platformIconSrc("boss"),
              },
              {
                value: "zhaopin",
                label: "智联招聘",
                description: "只支持 DOM 详情识别。",
                iconSrc: platformIconSrc("zhaopin"),
              },
              {
                value: "liepin",
                label: "猎聘企业端",
                description: "只支持 DOM 详情识别。",
                iconSrc: platformIconSrc("liepin"),
              },
              {
                value: "hliepin",
                label: "猎聘猎头端",
                description: "只支持 DOM 详情识别。",
                iconSrc: platformIconSrc("hliepin"),
              },
            ]}
          />
          <Form.Item
            htmlFor="goodhr-accounts-field-1"
            layout="vertical"
            style={{ marginBottom: 0, minWidth: 0 }}
            label={"账号名称"}
            extra={"用于在岗位运行和控制台中区分不同招聘账号。"}
          >
            <Input
              id="goodhr-accounts-field-1"
              style={{ width: "100%" }}
              value={form.display_name}
              onChange={(event) =>
                setForm({ ...form, display_name: event.target.value })
              }
              placeholder="例如：成都招聘账号"
            />
          </Form.Item>
          <Alert
            type={"info"}
            showIcon={true}
            title={
              <>
                创建后，任务栏里会出现一个橙色狐狸浏览器图标。请点击它打开浏览器，完成登录后保持浏览器打开，我会连续确认
                3 次登录状态。
              </>
            }
            icon={
              <img
                style={{ width: 28, height: 28 }}
                src={CHROMIUM_ICON_SRC}
                alt="浏览器图标"
              />
            }
          />
          {loginStatus ? (
            <Alert
              type={
                loginStatus.includes("失败") || loginStatus.includes("超时")
                  ? "warning"
                  : "success"
              }
              showIcon={true}
              title={<>{loginStatus}</>}
            />
          ) : null}
        </Flex>
      </AdminDialog>
    </>
  );
}
