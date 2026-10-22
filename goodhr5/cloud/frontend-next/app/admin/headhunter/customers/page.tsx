/** 文件作用说明：挂载猎头甲方客户管理页面，团队数据与编辑权限由服务端校验。 */
import HeadhunterPage from "@/components/admin/headhunter/HeadhunterPage";
/** CustomersPage 展示客户管理，复用统一后台布局与猎头资料模块。 */
export default function CustomersPage() { return <HeadhunterPage kind="customers" />; }
