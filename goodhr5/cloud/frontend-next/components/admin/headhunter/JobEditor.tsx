/** 文件作用说明：维护甲方真实招聘岗位并绑定已有平台岗位，绑定不会修改或新建平台任务。 */
"use client";
import { Alert, Col, Form, Input, InputNumber, Row, Select } from "antd";
import type { FormInstance } from "antd";
import { jobPriorities, jobStatuses, type HeadhunterCustomer, type HeadhunterJob, type HeadhunterMetadata } from "@/lib/headhunter-api";

/** JobEditor 区分甲方岗位要求与平台岗位绑定，薪资按年薪万元显示。 */
export default function JobEditor({ form, initialValues, metadata, customers, readonly, ownerEditable }: { form: FormInstance<HeadhunterJob>; initialValues: HeadhunterJob; metadata: HeadhunterMetadata; customers: HeadhunterCustomer[]; readonly: boolean; ownerEditable: boolean }) {
  return <Form form={form} name="headhunter-job" initialValues={initialValues} layout="vertical" disabled={readonly} preserve={false}>
    <Row gutter={16}>
      <Col xs={24} sm={12}><Form.Item name="name" label="甲方岗位名称" rules={[{ required: true, whitespace: true, message: "先写清楚要招什么岗位。" }, { max: 200 }]}><Input maxLength={200} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="customer_id" label="甲方客户" rules={[{ required: true, message: "请先选客户，没有的话先去客户管理新建。" }]}><Select showSearch optionFilterProp="label" options={customers.map((customer) => ({ value: customer.id, label: customer.name }))} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="city" label="工作城市"><Input maxLength={120} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="department" label="所属部门"><Input maxLength={120} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="headcount" label="招聘人数" rules={[{ required: true, message: "请填写至少 1 人。" }]}><InputNumber min={1} max={100000} precision={0} style={{ width: "100%" }} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="priority" label="优先级"><Select options={jobPriorities} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="salary_min" label="年薪下限（万元）" rules={[{ type: "number", min: 0 }]}><InputNumber min={0} max={100000} precision={2} style={{ width: "100%" }} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="salary_max" label="年薪上限（万元）" dependencies={["salary_min"]} rules={[{ type: "number", min: 0 }, ({ getFieldValue }) => ({
        /** validator 仅在上下限均明确时比较，零表示未知而非承诺零薪资。 */
        validator(_, value) { const lower = getFieldValue("salary_min"); return !value || !lower || value >= lower ? Promise.resolve() : Promise.reject(new Error("上限不能低于下限，请核对。")); },
      })]}><InputNumber min={0} max={100000} precision={2} style={{ width: "100%" }} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="education" label="学历要求"><Input maxLength={120} placeholder="未明确时可以留空" /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="experience" label="经验要求"><Input maxLength={200} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="status" label="岗位状态"><Select options={jobStatuses} /></Form.Item></Col>
      <Col xs={24} sm={12}><Form.Item name="owner_email" label="负责人" rules={[{ required: true, message: "请选负责人。" }]}><Select disabled={readonly || !ownerEditable} showSearch optionFilterProp="label" options={metadata.members.map((member) => ({ value: member.email, label: member.email }))} /></Form.Item></Col>
    </Row>
    <Form.Item name="description" label="岗位职责"><Input.TextArea autoSize={{ minRows: 4, maxRows: 12 }} maxLength={30000} /></Form.Item>
    <Form.Item name="requirements" label="任职要求"><Input.TextArea autoSize={{ minRows: 4, maxRows: 12 }} maxLength={30000} /></Form.Item>
    <Alert type="info" title="下面绑定的是已有招聘平台岗位；这里只建立对应关系，不会自动开始招聘。" style={{ marginBottom: 12 }} />
    <Form.Item name="position_ids" label="绑定已有平台岗位"><Select mode="multiple" showSearch optionFilterProp="label" options={metadata.positions.map((position) => ({ value: position.id, label: `${position.name}${position.platform_id ? ` · ${position.platform_id}` : ""}${position.is_open === false ? " · 已关闭" : ""}` }))} /></Form.Item>
    <Form.Item name="remark" label="岗位备注"><Input.TextArea autoSize={{ minRows: 2, maxRows: 8 }} maxLength={10000} /></Form.Item>
  </Form>;
}
