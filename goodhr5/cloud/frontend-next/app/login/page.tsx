/** 本文件负责展示 GoodHR 验证码与密码登录页面。 */
import "@/components/public-site.css";
import ArrowBackRoundedIcon from "@ant-design/icons/lib/icons/ArrowLeftOutlined";
import CheckCircleRoundedIcon from "@ant-design/icons/lib/icons/CheckCircleOutlined";
import Button from "antd/es/button";
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
            <p className="public-text public-login-10">GOODHR 控制台</p>
            <h1 className="public-text public-login-11">
              登录之后，继续你的招聘岗位运行
            </h1>
            <p className="public-text public-login-12">
              账号和岗位信息保存在云端，招聘平台登录状态、截图和浏览器数据只留在你的电脑里。
            </p>
            <div className="public-login-13">
              {loginPoints.map((point) => (
                <div key={point} className="public-login-14">
                  <CheckCircleRoundedIcon
                    className="public-login-15"
                    aria-hidden="true"
                  />
                  <p className="public-text public-login-16">{point}</p>
                </div>
              ))}
            </div>
          </div>
          <div className="public-panel public-login-17">
            <h2 className="public-text public-login-18">欢迎回来</h2>
            <p className="public-text public-login-19">选一种顺手的方式登录</p>
            <LoginForm />
          </div>
        </div>
      </div>
    </div>
  );
}
