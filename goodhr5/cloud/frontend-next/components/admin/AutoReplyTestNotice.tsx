/** 文件作用说明：在自动回复工作台和岗位设置共用测试提示及作者微信入口。 */
import { Alert, Button } from "antd";
import { AUTO_REPLY_TEST_NOTICE } from "@/lib/auto-reply-rollout";
import { AUTHOR_CONTACT } from "@/lib/author-contact";

/** AutoReplyTestNotice 显示测试说明和作者联系方式，由用户自行选择是否添加微信。 */
export default function AutoReplyTestNotice() {
  return <Alert type="info" showIcon title={AUTO_REPLY_TEST_NOTICE}
    description={<div>目前支持 BOSS直聘、猎聘企业端和智联招聘。<Button type="link" href={AUTHOR_CONTACT.contactPageURL}
      target="_blank" rel="noopener noreferrer">添加作者微信</Button></div>} />;
}
