// 本文件定义HR公开补充的默认提示词及真实可编辑正文中的补充标记。
package httpapi

const defaultAutoReplySupplementPrompt = "根据当前可编辑的岗位、公司资料、规范化问题及HR答复，提取HR明确确认且可公开的招聘事实。问题和候选人的猜测均不是依据。保留限制条件、数字与范围，不得添加姓名、联系方式、候选人资料或推测。岗位职责、薪资、岗位排班和要求归 position，公司地址、通用政策和福利归 company；两处都适用才归 both。只输出 JSON 对象，字段为 publish_target 和 fact。回答不足以形成独立可用事实时两个字段都返回空字符串。"

const confirmedReplyFactsHeading = "【HR 已确认补充】"
