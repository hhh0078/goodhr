/** 本文件负责展示 GoodHR 验证码与密码登录页面。 */
import "@/components/public-site.css";
import ArrowBackRoundedIcon from "@ant-design/icons/lib/icons/ArrowLeftOutlined";
import CheckCircleRoundedIcon from "@ant-design/icons/lib/icons/CheckCircleOutlined";
import Button from "antd/es/button";
import Paragraph from "antd/es/typography/Paragraph";
import Title from "antd/es/typography/Title";
import BrandMark from "@/components/BrandMark";
import LoginForm from "@/components/LoginForm";

const loginPoints = [
  "验证码或密码都能登录",
  "新账号用验证码和密码完成注册",
  "平台账号与浏览器数据仍保留在本地",
];

/** LoginPage 输出与新版首页统一的明亮登录界面。 */
export default function LoginPage() {
  return (
    <div className="public-login-1">
      {/* 保留原登录标题的响应式字号，避免 AntD 默认标题字号覆盖页面样式。 */}
      <style>{`
        .public-login-1 .public-login-11.ant-typography { font-size: 42px; }
        @media (min-width: 600px) {
          .public-login-1 .public-login-11.ant-typography { font-size: 54px; }
        }
        @media (min-width: 900px) {
          .public-login-1 .public-login-11.ant-typography { font-size: 64px; }
        }
      `}</style>
      <div className="public-container public-login-2">
        <div className="public-login-3">
          <BrandMark />
          <Button href="/" icon={<ArrowBackRoundedIcon aria-hidden="true" />}>
            返回首页
          </Button>
        </div>
      </div>
      <div className="public-container public-login-7">
        <div className="public-login-8">
          <div>
            <Paragraph
              className="public-text public-login-10"
              style={{
                margin: 0,
                color: "var(--goodhr-primary, #159a62)",
                fontSize: 14,
                fontWeight: 800,
                lineHeight: "normal",
              }}
            >
              GOODHR 控制台
            </Paragraph>
            <Title
              level={1}
              className="public-text public-login-11"
              style={{
                margin: "16px 0 0",
                color: "var(--goodhr-text, #17211c)",
                fontWeight: 780,
                lineHeight: 1.12,
              }}
            >
              登录之后，继续你的招聘岗位运行
            </Title>
            <Paragraph
              className="public-text public-login-12"
              style={{
                margin: "24px 0 0",
                color: "var(--goodhr-text-secondary, #637069)",
                fontSize: 18,
                lineHeight: 1.8,
              }}
            >
              账号和岗位信息保存在云端，招聘平台登录状态、截图和浏览器数据只留在你的电脑里。
            </Paragraph>
            <div className="public-login-13">
              {loginPoints.map((point) => (
                <div key={point} className="public-login-14">
                  <CheckCircleRoundedIcon
                    className="public-login-15"
                    aria-hidden="true"
                  />
                  <Paragraph
                    className="public-text public-login-16"
                    style={{
                      margin: 0,
                      color: "var(--goodhr-text-secondary, #637069)",
                      fontSize: "inherit",
                      lineHeight: "normal",
                    }}
                  >
                    {point}
                  </Paragraph>
                </div>
              ))}
            </div>
          </div>
          <div className="public-panel public-login-17">
            <Title
              level={2}
              className="public-text public-login-18"
              style={{
                margin: 0,
                color: "var(--goodhr-text, #17211c)",
                fontSize: 28,
                fontWeight: 750,
                lineHeight: "normal",
              }}
            >
              欢迎回来
            </Title>
            <Paragraph
              className="public-text public-login-19"
              style={{
                margin: "8px 0 28px",
                color: "var(--goodhr-text-secondary, #637069)",
                fontSize: "inherit",
                lineHeight: "normal",
              }}
            >
              选一种顺手的方式登录
            </Paragraph>
            <LoginForm />
          </div>
        </div>
      </div>
    </div>
  );
}
