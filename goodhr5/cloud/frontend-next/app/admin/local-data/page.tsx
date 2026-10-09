/** 本文件负责 本地下载和规则信息 的 Ant Design 页面。 */
"use client";
import { Button, Flex, Table, Tag, Typography } from "antd";
import { SyncOutlined } from "@ant-design/icons";
import { useEffect, useState } from "react";
import { formatDate, localRequest } from "@/lib/admin-api";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
/** LocalDataPage 展示本机下载记录、规则包和诊断信息。 */
export default function LocalDataPage() {
  const { agentBase, notify } = useAdmin();
  const [downloads, setDownloads] = useState<any[]>([]);
  const [rules, setRules] = useState<any[]>([]);
  const [diagnostics, setDiagnostics] = useState<any>({});
  const [loading, setLoading] = useState(false);
  /** load 从当前连接的本地程序读取本机数据。 */
  async function load() {
    if (!agentBase) return;
    setLoading(true);
    try {
      const [downloadData, ruleData, diagnosticData] = await Promise.all([
        localRequest(agentBase, "/api/v1/local/downloads"),
        localRequest(agentBase, "/api/v1/local/rules/status"),
        localRequest(agentBase, "/api/v1/diagnostics"),
      ]);
      setDownloads(downloadData.downloads || []);
      setRules(ruleData.rules || []);
      setDiagnostics(diagnosticData || {});
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "本地数据读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    void load();
  }, [agentBase]);
  /** updateRules 触发本地规则包更新。 */
  async function updateRules() {
    if (!agentBase) return notify("本地程序未连接", "error");
    try {
      const data = await localRequest(agentBase, "/api/v1/local/rules/update", {
        method: "POST",
        body: {},
      });
      notify(`规则更新完成：更新 ${data.updated?.length || 0} 个`, "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "规则更新失败", "error");
    }
  }
  /** canOpenDownload 判断下载记录是否已经保存到本机，可以打开。 */
  function canOpenDownload(item: any) {
    return item?.status === "saved" && Boolean(item?.file_path);
  }
  /** openDownloadFile 调用本地程序打开文件，或在文件夹里定位文件。 */
  async function openDownloadFile(path: string, endpoint: string) {
    if (!agentBase) return notify("本地程序未连接", "error");
    if (!path) return notify("这条记录没有文件路径，我打不开", "error");
    try {
      await localRequest(agentBase, endpoint, {
        method: "POST",
        body: { path },
      });
      notify(
        endpoint.endsWith("/reveal")
          ? "已经在文件夹里帮你选中了"
          : "已经用默认程序打开了",
        "success",
      );
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "我没打开成功，文件可能已经被移动或删除了",
        "error",
      );
    }
  }
  return (
    <>
      <PageHeader
        title="本地数据"
        description="数据来自当前电脑，不会上传到云端。"
        actions={
          <RefreshButton loading={loading} onClick={() => void load()} />
        }
      />
      {!agentBase ? (
        <SectionPanel>
          <EmptyState text="请先启动本地程序" />
        </SectionPanel>
      ) : (
        <Flex vertical={true} gap={2 * 8}>
          <SectionPanel>
            <Typography.Title
              level={2}
              style={{
                display: "block",
                margin: 0,
                fontSize: 19,
                fontWeight: 760,
              }}
            >
              本地诊断
            </Typography.Title>
            <pre
              style={{
                marginTop: 16,
                maxHeight: 280,
                overflow: "auto",
                padding: 16,
                backgroundColor: "var(--goodhr-hover)",
                borderRadius: "8px",
                whiteSpace: "pre-wrap",
                fontSize: 12,
              }}
            >
              {JSON.stringify(diagnostics, null, 2)}
            </pre>
          </SectionPanel>
          <SectionPanel>
            <Flex
              align="center"
              justify="space-between"
              style={{ marginBottom: 12 }}
            >
              <Typography.Title level={5} style={{ margin: 0 }}>
                平台规则包
              </Typography.Title>
              <Button
                icon={<SyncOutlined aria-hidden="true" />}
                onClick={() => void updateRules()}
              >
                更新规则
              </Button>
            </Flex>
            <Table
              rowKey="platform_id"
              dataSource={rules}
              size="small"
              pagination={false}
              scroll={{ x: 640 }}
              columns={[
                { title: "平台", dataIndex: "platform_id" },
                {
                  title: "版本",
                  dataIndex: "version",
                  render: (version) => version || "--",
                },
                {
                  title: "状态",
                  dataIndex: "status",
                  render: (status) => <Tag>{status || "未知"}</Tag>,
                },
                {
                  title: "更新时间",
                  dataIndex: "updated_at",
                  render: formatDate,
                },
              ]}
            />
          </SectionPanel>
          <SectionPanel>
            <Typography.Title level={5}>下载记录</Typography.Title>
            <Table
              rowKey={(item) => item.id || item.file_path || item.url}
              dataSource={downloads}
              size="small"
              pagination={{ pageSize: 10 }}
              scroll={{ x: 860 }}
              columns={[
                {
                  title: "文件",
                  key: "file",
                  render: (_, item) => (
                    <Flex vertical gap={4}>
                      <Typography.Text strong>
                        {item.file_name || fileName(item.file_path)}
                      </Typography.Text>
                      <Typography.Text
                        type="secondary"
                        style={{ fontSize: 12, overflowWrap: "anywhere" }}
                      >
                        {item.file_path || item.url || ""}
                      </Typography.Text>
                    </Flex>
                  ),
                },
                {
                  title: "状态",
                  dataIndex: "status",
                  render: (status) => <Tag>{status || "--"}</Tag>,
                },
                { title: "大小", dataIndex: "size", render: formatSize },
                {
                  title: "操作",
                  key: "actions",
                  width: 240,
                  render: (_, item) =>
                    canOpenDownload(item) ? (
                      <Flex gap={8}>
                        <Button
                          size="small"
                          onClick={() =>
                            void openDownloadFile(
                              item.file_path,
                              "/api/v1/files/open",
                            )
                          }
                        >
                          打开文件
                        </Button>
                        <Button
                          size="small"
                          onClick={() =>
                            void openDownloadFile(
                              item.file_path,
                              "/api/v1/files/reveal",
                            )
                          }
                        >
                          打开文件夹
                        </Button>
                      </Flex>
                    ) : null,
                },
              ]}
            />
          </SectionPanel>
        </Flex>
      )}
    </>
  );
}
/** formatSize 将字节转换为易读大小。 */
function formatSize(value: number) {
  const size = Number(value || 0);
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
}
/** fileName 从路径中提取文件名。 */
function fileName(value: string) {
  return (
    String(value || "")
      .split(/[\\/]/)
      .filter(Boolean)
      .pop() || "未命名文件"
  );
}
