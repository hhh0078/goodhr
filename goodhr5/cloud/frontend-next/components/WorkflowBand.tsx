/** 本文件负责展示 GoodHR 招聘自动化主流程。 */
import AutoAwesomeRoundedIcon from "@ant-design/icons/lib/icons/ThunderboltOutlined";
import CalendarMonthRoundedIcon from "@ant-design/icons/lib/icons/CalendarOutlined";
import FilterAltRoundedIcon from "@ant-design/icons/lib/icons/FilterOutlined";
import ForumRoundedIcon from "@ant-design/icons/lib/icons/CommentOutlined";

const workflow = [
  {
    icon: AutoAwesomeRoundedIcon,
    index: "01",
    title: "AI分析",
    description: "AI根据岗位模板分析候选人",
  },
  {
    icon: FilterAltRoundedIcon,
    index: "01",
    title: "自动打招呼",
    description: "自动打招呼并持续跟进",
  },

  {
    icon: ForumRoundedIcon,
    index: "03",
    title: "候选人跟进",
    description: "继续确认候选人意向，自动回复开发中",
  },
  {
    icon: CalendarMonthRoundedIcon,
    index: "04",
    title: "邀约",
    description: "推进高意向候选人面试",
  },
];

/** WorkflowBand 使用轻量横向结构展示招聘流程。 */
export default function WorkflowBand() {
  return (
    <div className="public-workflowband-1">
      <div className="public-container">
        <div className="public-workflowband-3">
          <div aria-hidden="true" className="public-workflowband-4">
            <div className="public-workflowband-5" />
          </div>
          {workflow.map((item, index) => {
            const Icon = item.icon;
            return (
              <div key={item.title} className="public-workflowband-6">
                <div
                  style={
                    {
                      "--public-workflowband-7-0": `${index * 1.05}s`,
                    } as import("react").CSSProperties
                  }
                  className="public-workflowband-7"
                >
                  <Icon className="public-workflowband-8" aria-hidden="true" />
                </div>
                <div>
                  <p className="public-text public-workflowband-10">
                    {item.title}
                  </p>
                  <p className="public-text public-workflowband-11">
                    {item.description}
                  </p>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
