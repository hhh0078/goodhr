/** 本文件负责官网联系我们页面及联系方式展示。 */
import EmailRoundedIcon from "@ant-design/icons/lib/icons/MailOutlined";
import ChatRoundedIcon from "@ant-design/icons/lib/icons/CommentOutlined";
import GroupsRoundedIcon from "@ant-design/icons/lib/icons/TeamOutlined";
import LanguageRoundedIcon from "@ant-design/icons/lib/icons/GlobalOutlined";
import PhoneRoundedIcon from "@ant-design/icons/lib/icons/PhoneOutlined";

import type { Metadata } from "next";
import Image from "next/image";
import MarketingShell from "@/components/MarketingShell";
import { createPageMetadata, SITE_URL } from "@/lib/seo";
import { AUTHOR_CONTACT } from "@/lib/author-contact";

export const metadata: Metadata = createPageMetadata({
  title: "联系GoodHR - 招聘自动化与AI招聘工具咨询",
  description:
    "联系 GoodHR，咨询 BOSS、猎聘、智联等招聘平台自动化、AI筛选、自动打招呼、自动回复、本地程序安装和订阅问题。",
  path: "/contact",
  keywords: ["招聘自动化咨询", "AI招聘工具客服", "BOSS自动打招呼技术支持"],
});

const contacts = [
  {
    icon: PhoneRoundedIcon,
    label: "手机与微信",
    value: AUTHOR_CONTACT.phone,
    href: `tel:${AUTHOR_CONTACT.phone}`,
    note: "工作日和周末都可以留言。",
  },
  {
    icon: EmailRoundedIcon,
    label: "电子邮箱",
    value: AUTHOR_CONTACT.email,
    href: `mailto:${AUTHOR_CONTACT.email}`,
    note: "可发送问题截图、日志和使用需求。",
  },
  {
    icon: LanguageRoundedIcon,
    label: "官方网站",
    value: new URL(SITE_URL).host,
    href: SITE_URL,
    note: "查看产品更新、教程和下载入口。",
  },
];

const qrcodes = [
  {
    icon: ChatRoundedIcon,
    title: "微信联系作者",
    description: "安装、订阅、发票、功能建议，都可以直接加我。",
    image: AUTHOR_CONTACT.wechatQrCode,
    alt: AUTHOR_CONTACT.wechatQrCodeAlt,
  },
  {
    icon: GroupsRoundedIcon,
    title: "加入 GoodHR 解答群",
    description: "常见问题、使用交流、更新通知，群里会同步。",
    image: "/assets/contact/qq-group.jpg",
    alt: "GoodHR 解答 QQ 群二维码",
  },
];

/** ContactPage 展示 GoodHR 联系方式。 */
export default function ContactPage() {
  return (
    <MarketingShell
      eyebrow="联系我们"
      title="遇到问题，直接找到我们"
      description="无论是安装、本地连接、AI 配置还是订阅问题，都可以通过下面的方式联系。"
    >
      <section className="public-contact-2">
        <div className="public-container">
          <div className="public-contact-4">
            {contacts.map((item) => {
              const Icon = item.icon;
              return (
                <div key={item.label} className="public-contact-5">
                  <div className="public-contact-6">
                    <Icon className="public-contact-7" aria-hidden="true" />
                    <p className="public-text public-contact-8">{item.label}</p>
                  </div>
                  <div>
                    <a href={item.href} className="public-contact-10">
                      {item.value}
                    </a>
                    <p className="public-text public-contact-11">{item.note}</p>
                  </div>
                </div>
              );
            })}
          </div>
          <div className="public-contact-12">
            {qrcodes.map((item) => {
              const Icon = item.icon;
              return (
                <div key={item.title} className="public-contact-13">
                  <div className="public-contact-14">
                    <Image
                      src={item.image}
                      alt={item.alt}
                      fill
                      sizes="160px"
                      style={{ objectFit: "cover" }}
                    />
                  </div>
                  <div>
                    <div className="public-contact-17">
                      <Icon className="public-contact-18" aria-hidden="true" />
                      <p className="public-text public-contact-19">
                        {item.title}
                      </p>
                    </div>
                    <p className="public-text public-contact-20">
                      {item.description}
                    </p>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </section>
    </MarketingShell>
  );
}
