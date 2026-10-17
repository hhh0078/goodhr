/** 本文件集中维护无需登录的官网页面，供站点地图和 AI 阅读索引复用。 */

export const PUBLIC_SITE_PAGES = [
  { path: "/", title: "首页", description: "GoodHR 招聘自动化与 AI 招聘助手介绍。" },
  { path: "/features", title: "功能介绍", description: "了解候选人筛选、招聘沟通、简历库与团队协作。" },
  { path: "/pricing", title: "产品定价", description: "查看会员方案与功能范围。" },
  { path: "/videos", title: "视频教程", description: "学习安装本地程序、创建岗位与开始招聘任务。" },
  { path: "/articles", title: "文章/动态", description: "阅读公开的使用方法、招聘实践和产品动态。" },
  { path: "/download", title: "下载", description: "下载 Windows、macOS 本地程序和浏览器插件。" },
  { path: "/contact", title: "联系我们", description: "查看 GoodHR 的公开联系方式。" },
] as const;
