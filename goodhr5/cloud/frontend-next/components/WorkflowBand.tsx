/** 本文件负责展示 GoodHR 招聘自动化主流程。 */
import PublicIllustration from "./PublicIllustration";
import ArrowRightOutlined from "@ant-design/icons/lib/icons/ArrowRightOutlined";

const workflow = [
  {
    illustration: "analysis" as const,
    title: "AI分析",
    description: "AI根据岗位模板分析候选人",
  },
  {
    illustration: "greeting" as const,
    title: "自动打招呼",
    description: "自动打招呼并持续跟进",
  },

  {
    illustration: "followup" as const,
    title: "候选人跟进",
    description: "继续确认候选人意向，自动回复开发中",
  },
  {
    illustration: "interview" as const,
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
          {workflow.map((item, index) => {
            return (
              <div key={item.title} className="public-workflowband-6">
                <PublicIllustration kind={item.illustration} className="public-workflow-art" />
                {index < workflow.length - 1 ? <ArrowRightOutlined className="public-workflow-connector" aria-hidden="true" /> : null}
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
