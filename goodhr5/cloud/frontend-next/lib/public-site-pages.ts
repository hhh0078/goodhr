/** 本文件集中维护无需登录的官网页面，供站点地图和 AI 阅读索引复用。 */

export const PUBLIC_SITE_PAGES = [
  { path: "/", title: "首页", description: "GoodHR 招聘自动化与 AI 招聘助手介绍。" },
  { path: "/features", title: "功能介绍", description: "了解候选人筛选、招聘沟通、简历库与团队协作。" },
  { path: "/recruitment-automation", title: "多平台招聘自动化", description: "了解 BOSS直聘、猎聘、智联招聘的筛选与打招呼流程，以及前程无忧的适配进度。" },
  { path: "/recruitment-auto-reply", title: "招聘消息自动回复", description: "了解招聘沟通场景、人工复核边界与各平台自动回复的开发进度。" },
  { path: "/hr-tools", title: "HR与猎头工具", description: "从简历导入、查重和人才库，到猎头推荐与企业招聘流程的使用说明。" },
  { path: "/pricing", title: "产品定价", description: "查看会员方案与功能范围。" },
  { path: "/videos", title: "视频教程", description: "学习安装本地程序、创建岗位与开始招聘任务。" },
  { path: "/articles", title: "文章/动态", description: "阅读公开的使用方法、招聘实践和产品动态。" },
  { path: "/download", title: "下载", description: "下载 Windows、macOS 本地程序和浏览器插件。" },
  { path: "/contact", title: "联系我们", description: "查看 GoodHR 的公开联系方式。" },
] as const;
