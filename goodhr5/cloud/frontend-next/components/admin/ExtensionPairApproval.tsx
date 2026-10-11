/** 本文件负责在已登录控制台确认插件一次性配对，网页凭证只传给本机 Agent，不交给插件页面。 */
"use client";

import {useEffect,useRef,useState} from "react";
import {useSearchParams,useRouter} from "next/navigation";
import {Alert} from "antd";
import AdminDialog from "@/components/admin/AdminDialog";
import {getToken,localRequest} from "@/lib/admin-api";
import {useAdmin} from "@/components/admin/AdminApp";

/** ExtensionPairApproval 展示当前账号与授权范围，经用户点击才提交配对。 */
export default function ExtensionPairApproval() {
  const params=useSearchParams();
  const {user}=useAdmin();
  const pairingID=params.get("extension_pair") || "";
  const port=Number(params.get("agent_port") || "43129");
  const accountKey=String(user?.email || "").trim().toLowerCase() || String(user?.id || "");
  if (!pairingID) return null;
  return <PairApprovalDialog key={JSON.stringify([pairingID,port,accountKey])} pairingID={pairingID} port={port}/>;
}

/** PairApprovalDialog 隔离每次配对的状态，关闭或更换账号后忽略旧请求结果。 */
function PairApprovalDialog({pairingID,port}:{pairingID:string;port:number}) {
  const params=useSearchParams(),router=useRouter();
  const {user,notify}=useAdmin();
  const [visible,setVisible]=useState(true);
  const [busy,setBusy]=useState(false),[done,setDone]=useState(false);
  const activeRef=useRef(true),busyRef=useRef(false);
  /** 保持当前配对实例的有效标记，卸载后不再更新弹框或提示。 */
  useEffect(()=>{
    activeRef.current=true;
    return ()=>{activeRef.current=false;};
  },[]);
  /** isCurrentRequest 只允许仍打开且登录凭证未变化的配对接收请求结果。 */
  function isCurrentRequest(token:string | undefined) {
    if (!activeRef.current) return false;
    if (token===undefined) return true;
    try {return getToken()===token;}
    catch {return false;}
  }
  /** approve 校验本机端口后使用当前网页登录凭证批准一次性请求。 */
  async function approve() {
    if (!activeRef.current || busyRef.current || done) return;
    if (!Number.isInteger(port) || port<1024 || port>65535 || !pairingID) {notify("配对地址不完整，请重新从插件连接","warning");return;}
    busyRef.current=true;
    setBusy(true);
    let token:string | undefined;
    try {
      token=getToken();
      await localRequest(`http://127.0.0.1:${port}`,"/api/v1/extensions/pair/approve",{method:"POST",body:{pairing_id:pairingID,token}});
      if (!isCurrentRequest(token)) return;
      setDone(true);
      notify("插件已授权，可以回插件继续了","success");
    }
    catch(error) {if (isCurrentRequest(token)) notify(error instanceof Error ? error.message : "授权暂时没完成，请重试","error");}
    finally {busyRef.current=false;if (isCurrentRequest(token)) setBusy(false);}
  }
  /** close 立即隐藏弹框并移除配对参数，保留简历库筛选且不撤回已发送的授权请求。 */
  function close() {
    if (!activeRef.current) return;
    activeRef.current=false;
    setVisible(false);
    const next=new URLSearchParams(params.toString());
    next.delete("extension_pair");
    next.delete("agent_port");
    router.replace(`/admin/resumes${next.size ? `?${next}` : ""}`,{scroll:false});
  }
  return <AdminDialog open={visible} title="连接 GoodHR 浏览器插件" onClose={close} cancelText={done ? "完成" : busy ? "关闭" : "取消"} confirmLoading={busy} onConfirm={done ? undefined : ()=>void approve()} confirmText="确认连接">
    <p>插件将使用当前账号 {user?.email || ""}。</p>
    <Alert type={done ? "success" : "info"} showIcon message={done ? "授权完成，请回到插件" : busy ? "连接正在处理，关掉弹框后也可能完成，可回插件查看结果。" : "允许查询和添加简历、记录备注、关联岗位与推荐。插件只读取招聘页面，不自动操作招聘网站。"}/>
  </AdminDialog>;
}
