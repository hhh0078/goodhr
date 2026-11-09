/** 本文件为管理员提供默认话术和常用条件的可视化增删编辑，草稿随系统配置统一保存。 */
"use client";
import { Button, Card, Flex, Input, Tabs, Typography } from "antd";
import { DeleteOutlined, EditOutlined, PlusOutlined } from "@ant-design/icons";
import { useState } from "react";
import type { ReplyPhrase } from "@/lib/auto-reply";
import ReplyPhraseEditor from "./ReplyPhraseEditor";

export const AUTO_REPLY_LIBRARY_KEY = "system.auto_reply_library";
type Library = { phrases: ReplyPhrase[]; condition_options: string[]; [key: string]: unknown };

/** validateAutoReplyLibrary 校验可视化输入，避免空白、重复和超长条件提交到后台。 */
export function validateAutoReplyLibrary(value: string) {
  try {
    const data = JSON.parse(value) as Library;
    if (!data || !Array.isArray(data.phrases) || !Array.isArray(data.condition_options)) return "话术或条件还没读完整，请重新加载";
    const ids = new Set<string>();
    for (const item of data.phrases) {
      if (!/^[A-Za-z][A-Za-z0-9_-]{0,79}$/.test(item.id) || item.id.startsWith("job_") || ids.has(item.id)) return "话术编号需要保持唯一，请重新添加这条话术";
      ids.add(item.id);
      if (!item.category?.trim() || !item.title?.trim() || !item.scene?.trim() || !item.content?.trim()) return "请补全话术的分类、标题、适用场景和内容";
    }
    if (data.phrases.length > 64 || data.condition_options.length > 80) return "话术最多64条，常用条件最多80条";
    if (data.condition_options.some(x => typeof x !== "string" || !x.trim() || Array.from(x).length > 1000)) return "常用条件需要填写具体内容，每条最多一千字";
    if (new Set(data.condition_options.map(x => x.trim())).size !== data.condition_options.length) return "有两条常用条件相同，保留一条就好";
    return "";
  } catch { return "默认话术暂时没读出来，请重新加载"; }
}

/** AutoReplyLibraryForm 通过标题、场景和正文管理系统默认话术。 */
export default function AutoReplyLibraryForm({value,onChange,disabled}:{value:string;onChange:(value:string)=>void;disabled?:boolean}) {
  const [editing,setEditing] = useState<ReplyPhrase|null>(null);
  let data: Library;
  try { data=JSON.parse(value); } catch { return null; }
  if (!data || !Array.isArray(data.phrases) || !Array.isArray(data.condition_options)) return null;
  /** updateLibrary 将本次可视化改动合并到完整系统草稿。 */
  function updateLibrary(patch:Partial<Library>) { onChange(JSON.stringify({...data,...patch},null,2)); }
  /** savePhrase 保存当前条目并保留原有稳定编号。 */
  function savePhrase(phrase:ReplyPhrase) {
    const exists=data.phrases.some(x=>x.id===phrase.id);
    updateLibrary({phrases:exists?data.phrases.map(x=>x.id===phrase.id?phrase:x):[...data.phrases,phrase]});setEditing(null);
  }
  return <>
    <Tabs items={[
      {key:"phrases",label:`默认话术 · ${data.phrases.length}`,children:<Flex vertical gap={8}>
        <Flex justify="flex-end"><Button disabled={disabled} icon={<PlusOutlined/>} onClick={()=>setEditing({id:`default_${crypto.randomUUID()}`,category:"岗位沟通",title:"",scene:"",content:""})}>添加话术</Button></Flex>
        {data.phrases.map(item=><Card key={item.id} size="small" styles={{body:{padding:12}}}>
          <Flex align="flex-start" justify="space-between" gap={8}>
            <div style={{minWidth:0}}><Typography.Text strong>{item.title}</Typography.Text><Typography.Text type="secondary" style={{marginLeft:8,fontSize:12}}>{item.category}</Typography.Text>
              <Typography.Paragraph type="secondary" style={{fontSize:12,margin:"4px 0"}}>{item.scene}</Typography.Paragraph>
              <Typography.Paragraph style={{marginBottom:0,whiteSpace:"pre-wrap",overflowWrap:"anywhere"}}>{item.content}</Typography.Paragraph></div>
            <Flex gap={4}><Button aria-label={`编辑${item.title}`} icon={<EditOutlined/>} disabled={disabled} onClick={()=>setEditing(item)}/><Button danger aria-label={`删除${item.title}`} icon={<DeleteOutlined/>} disabled={disabled} onClick={()=>updateLibrary({phrases:data.phrases.filter(x=>x.id!==item.id)})}/></Flex>
          </Flex>
        </Card>)}
      </Flex>},
      {key:"conditions",label:"常用条件",children:<Flex vertical gap={8}>
        <Typography.Paragraph type="secondary">设置具体、可勾选的条件。地点、班次等信息请写完整。</Typography.Paragraph>
        {data.condition_options.map((item,index)=><Flex key={index} gap={8}><Input aria-label={`常用条件 ${index+1}`} value={item} disabled={disabled} onChange={event=>updateLibrary({condition_options:data.condition_options.map((x,i)=>i===index?event.target.value:x)})}/><Button danger aria-label={`删除常用条件 ${index+1}`} icon={<DeleteOutlined/>} disabled={disabled} onClick={()=>updateLibrary({condition_options:data.condition_options.filter((_,i)=>i!==index)})}/></Flex>)}
        <Button style={{alignSelf:"flex-start"}} icon={<PlusOutlined/>} disabled={disabled} onClick={()=>updateLibrary({condition_options:[...data.condition_options,""]})}>添加常用条件</Button>
      </Flex>},
    ]}/>
    {editing?<ReplyPhraseEditor phrase={editing} categories={Array.from(new Set(data.phrases.map(x=>x.category)))} onSave={savePhrase} onClose={()=>setEditing(null)}/>:null}
  </>;
}
