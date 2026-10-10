/** 本文件负责在已登录控制台确认插件一次性配对，网页凭证只传给本机 Agent，不交给插件页面。 */
"use client";

import {useState} from "react";
import {useSearchParams,useRouter} from "next/navigation";
import {Alert} from "antd";
import AdminDialog from "@/components/admin/AdminDialog";
import {getToken,localRequest} from "@/lib/admin-api";
import {useAdmin} from "@/components/admin/AdminApp";

/** ExtensionPairApproval 展示当前账号与授权范围，经用户点击才提交配对。 */
export default function ExtensionPairApproval() {
  const params=useSearchParams(),router=useRouter();
  const {user,notify}=useAdmin();
  const [busy,setBusy]=useState(false),[done,setDone]=useState(false);
  const pairingID=params.get("extension_pair") || "";
  const port=Number(params.get("agent_port") || "43129");
  /** approve 校验本机端口后使用当前网页登录凭证批准一次性请求。 */
  async function approve() {
    if (!Number.isInteger(port) || port<1024 || port>65535 || !pairingID) {notify("配对地址不完整，请重新从插件连接","warning");return;}
    setBusy(true);
    try {await localRequest(`http://127.0.0.1:${port}`,"/api/v1/extensions/pair/approve",{method:"POST",body:{pairing_id:pairingID,token:getToken()}});setDone(true);notify("插件已授权，可以回插件继续了","success");}
    catch(error) {notify(error instanceof Error ? error.message : "授权暂时没完成，请重试","error");}
    finally {setBusy(false);}
  }
  /** close 移除已经处理的配对参数，保留简历库页面。 */
  function close() {const next=new URLSearchParams(params.toString());next.delete("extension_pair");next.delete("agent_port");router.replace(`/admin/resumes${next.size ? `?${next}` : ""}`);}
  if (!pairingID) return null;
  return <AdminDialog open title="连接 GoodHR 浏览器插件" onClose={close} cancelText={done ? "完成" : "取消"} loading={busy} onConfirm={done ? undefined : ()=>void approve()} confirmText="确认连接">
    <p>插件将使用当前账号 {user?.email || ""}。</p>
    <Alert type={done ? "success" : "info"} showIcon message={done ? "授权完成，请回到插件" : "允许查询和添加简历、记录备注、关联岗位与推荐。插件只读取招聘页面，不自动操作招聘网站。"}/>
  </AdminDialog>;
}
