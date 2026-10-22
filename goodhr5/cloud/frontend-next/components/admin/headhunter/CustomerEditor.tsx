/** 文件作用说明：维护甲方公司基础资料、联系人和合同链接，仅在服务端允许管理时提供编辑。 */
"use client";
import { Button, Col, Flex, Form, Input, Row, Select } from "antd";
import { MinusCircleOutlined, PlusOutlined } from "@ant-design/icons";
import type { FormInstance } from "antd";
import { customerStatuses, type HeadhunterCustomer, type HeadhunterMetadata } from "@/lib/headhunter-api";

/** CustomerEditor 使用表单数组维护联系人与合同，不为未填写字段编造信息。 */
export default function CustomerEditor({ form, initialValues, metadata, readonly, ownerEditable }: { form: FormInstance<HeadhunterCustomer>; initialValues: HeadhunterCustomer; metadata: HeadhunterMetadata; readonly: boolean; ownerEditable: boolean }) {
  return <Form form={form} name="headhunter-customer" initialValues={initialValues} layout="vertical" disabled={readonly} preserve={false}>
    <Row gutter={16}>
      <Col xs={24} sm={12}><Form.Item name="name" label="甲方公司名称" rules={[{ required: true, whitespace: true, message: "先填公司名称，免得把客户认错。" }, { max: 200 }]}><Input maxLength={200} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="short_name" label="公司简称"><Input maxLength={80} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="code" label="客户编号"><Input maxLength={80} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="industry" label="所属行业"><Input maxLength={120} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="scale" label="公司规模"><Input maxLength={80} placeholder="比如：100–500 人" /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="city" label="所在城市"><Input maxLength={120} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="status" label="合作状态" rules={[{ required: true }]}><Select options={customerStatuses} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="source" label="客户来源"><Input maxLength={120} /></Form.Item></Col>
      <Col xs={24}><Form.Item name="owner_email" label="负责人" rules={[{ required: true, message: "请选一位负责人。" }]}><Select disabled={readonly || !ownerEditable} showSearch optionFilterProp="label" options={metadata.members.map((member) => ({ value: member.email, label: member.email }))} /></Form.Item></Col>
    </Row>
    <Form.List name="contacts">{(fields, { add, remove }) => <>
      {fields.map((field) => <div key={field.key} style={{ padding: 12, marginBottom: 12, border: "1px solid var(--ant-color-border-secondary)", borderRadius: 8 }}>
        <Flex justify="space-between" align="center"><strong>联系人 {field.name + 1}</strong>{!readonly ? <Button type="text" aria-label={`删除联系人 ${field.name + 1}`} danger icon={<MinusCircleOutlined />} onClick={() => remove(field.name)} /> : null}</Flex>
        <Row gutter={12}>
          <Col xs={24} sm={12}><Form.Item name={[field.name, "name"]} label="联系人姓名"><Input maxLength={100} /></Form.Item></Col>
          <Col xs={24} sm={12}><Form.Item name={[field.name, "position"]} label="联系人职位"><Input maxLength={100} /></Form.Item></Col>
          <Col xs={24} sm={12}><Form.Item name={[field.name, "phone"]} label="联系电话"><Input maxLength={40} /></Form.Item></Col>
          <Col xs={24} sm={12}><Form.Item name={[field.name, "email"]} label="联系邮箱" rules={[{ type: "email", message: "邮箱格式似乎不对，请核对。" }]}><Input maxLength={200} /></Form.Item></Col>
        </Row>
      </div>)}
      {!readonly ? <Button icon={<PlusOutlined />} onClick={() => add({ name: "", position: "", phone: "", email: "" })}>添加联系人</Button> : null}
    </>}</Form.List>
    <Form.List name="contracts">{(fields, { add, remove }) => <div style={{ marginTop: 16 }}>
      {fields.map((field) => <Row gutter={12} key={field.key}>
        <Col xs={24} sm={8}><Form.Item name={[field.name, "name"]} label="合同名称" rules={[{ required: true, whitespace: true, message: "填个名称，之后好找。" }]}><Input maxLength={200} /></Form.Item></Col>
        <Col xs={20} sm={14}><Form.Item name={[field.name, "url"]} label="合同链接" rules={[{ required: true, type: "url", message: "请填写完整的 http 或 https 链接。" }, { pattern: /^https?:\/\//i, message: "仅支持 http 或 https 链接。" }]}><Input maxLength={2000} /></Form.Item></Col>
        <Col xs={4} sm={2}>{!readonly ? <Button style={{ marginTop: 30 }} type="text" danger aria-label={`删除合同 ${field.name + 1}`} icon={<MinusCircleOutlined />} onClick={() => remove(field.name)} /> : null}</Col>
      </Row>)}
      {!readonly ? <Button icon={<PlusOutlined />} onClick={() => add({ name: "", url: "" })}>添加合同链接</Button> : null}
    </div>}</Form.List>
    <Form.Item name="remark" label="公司备注" style={{ marginTop: 16 }}><Input.TextArea autoSize={{ minRows: 3, maxRows: 8 }} maxLength={10000} /></Form.Item>
  </Form>;
}
