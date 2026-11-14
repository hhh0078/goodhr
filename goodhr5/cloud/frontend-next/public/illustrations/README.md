<!-- 文件作用：说明官网手绘切图的用途、主题对应关系与优化方式。 -->
# 官网手绘素材

本轮使用内置 Image Gen 生成，人物为虚构成年女性 HR，无真实候选人资料、宣传文字或第三方标志。

- `hero-green.webp` / `hero-rose.webp`：首页、内页及登录页的手绘人物主视觉。
- `workflow-analysis.webp` / `workflow-analysis-rose.webp`：AI 简历分析与文档相关区域。
- `workflow-greeting.webp` / `workflow-greeting-rose.webp`：自动打招呼与沟通相关区域。
- `workflow-followup.webp` / `workflow-followup-rose.webp`：候选人跟进区域。
- `workflow-interview.webp` / `workflow-interview-rose.webp`：面试邀约区域。
- `backdrop-green.webp` / `backdrop-rose.webp`：首页和内页首屏的渐变色场、柔和曲线色带背景，无文字和人物。

人物与流程素材为1254×1254、真实透明背景，采用铅笔线条与水彩纸感；背景为1672×941、不透明。两套主题各自使用对应配色。网页由 `PublicIllustration` 输出正常文字之外的装饰图片，不把页面文案画进图片。

生成提示方向：成年东亚女性HR、职业西装、笔记本电脑、简历纸带、咖啡与办公绿植；高级二维手绘卡通、深色铅笔轮廓和柔和水彩纹理，专业亲切，透明留白，无文字无水印。流程图分别是简历与放大镜、聊天气泡、联系人卡与时钟、日历与勾选标记。

原图采用内置工具生成的透明PNG；项目内保留构图、尺寸及透明通道，使用已安装的Sharp转换为quality=90的WebP，合计约2.7MiB，比原PNG减少83%。原始生成文件保存在工具默认生成目录，项目不重复保存PNG。网页继续由Next Image按显示尺寸提供优化图片。
