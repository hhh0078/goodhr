-- 文件作用：提供系统默认话术、岗位级增改和隐藏设置，保留旧岗位的自定义索要简历话术。
ALTER TABLE position_auto_reply_configs ADD COLUMN IF NOT EXISTS phrase_overrides JSONB;
COMMENT ON COLUMN position_auto_reply_configs.phrase_overrides IS '本岗位修改、新增或隐藏的话术，按稳定编号与当前系统默认话术合并';

INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES ('system.auto_reply_library', $library${
  "phrases": [
    {
      "id": "greeting",
      "category": "开场",
      "title": "首次问候",
      "scene": "候选人首次打招呼，尚未提出具体问题",
      "content": "您好，您想先了解这个岗位的哪方面？"
    },
    {
      "id": "request_resume",
      "category": "简历",
      "title": "索要简历",
      "scene": "沟通需要简历，候选人尚未提交也未拒绝",
      "content": "方便发一份简历吗？我想结合您的经历，聊聊这个岗位。"
    },
    {
      "id": "receive_resume",
      "category": "简历",
      "title": "收到简历",
      "scene": "候选人新提交了简历卡片",
      "content": "简历收到了，谢谢你发过来。"
    },
    {
      "id": "unreadable_resume",
      "category": "简历",
      "title": "附件打不开",
      "scene": "已确认收到的附件无法打开，需要候选人重新提供",
      "content": "刚才那份附件没能打开，方便重新发一份 PDF 吗？"
    },
    {
      "id": "request_phone",
      "category": "联系方式",
      "title": "索要手机号",
      "scene": "需要电话沟通，现有资料没有可用手机号，候选人未拒绝",
      "content": "如果方便电话沟通，可以留个手机号；想在这里聊也可以。"
    },
    {
      "id": "receive_phone",
      "category": "联系方式",
      "title": "收到手机号",
      "scene": "候选人刚提供了可识别的手机号",
      "content": "号码收到了，谢谢。"
    },
    {
      "id": "call_time",
      "category": "联系方式",
      "title": "确认通话时间",
      "scene": "候选人同意电话沟通，尚未约定时段",
      "content": "您一般哪个时间段方便接电话？"
    },
    {
      "id": "clarify_question",
      "category": "岗位沟通",
      "title": "澄清候选人的问题",
      "scene": "候选人提问中的具体指代不清楚",
      "content": "您刚才说的「{候选人提到的内容}」，具体是指哪一方面？"
    },
    {
      "id": "work_location",
      "category": "岗位沟通",
      "title": "确认工作地点",
      "scene": "岗位已启用工作地点确认条件，且有明确的工作地点",
      "content": "这个岗位的工作地点在{工作地点}，您这边方便吗？"
    },
    {
      "id": "start_date",
      "category": "岗位沟通",
      "title": "询问到岗时间",
      "scene": "岗位需要确认到岗时间且候选人尚未说明",
      "content": "如果双方都觉得合适，您预计什么时候方便到岗？"
    },
    {
      "id": "manual_followup",
      "category": "人工跟进",
      "title": "信息需要核实",
      "scene": "候选人提问缺少可靠资料，已登记给 HR 跟进",
      "content": "这个问题我需要确认下，稍后再回复你。"
    },
    {
      "id": "end_conversation",
      "category": "结束沟通",
      "title": "候选人暂不考虑",
      "scene": "候选人明确表示暂不考虑这个岗位",
      "content": "好的，了解了，谢谢您告诉我。祝您找工作顺利。"
    }
  ],
  "condition_options": [
    "能接受现场办公",
    "能接受轮班",
    "能接受短期出差",
    "能接受周末排班"
  ]
}$library$::jsonb, '岗位自动回复默认话术与可勾选的常用条件', TRUE)
ON CONFLICT (config_key) DO NOTHING;

UPDATE position_auto_reply_configs
SET phrase_overrides = jsonb_build_array(jsonb_build_object(
    'id','request_resume','category','简历','title','索要简历',
    'scene','沟通需要简历，候选人尚未提交也未拒绝','content',btrim(resume_request_message)))
WHERE phrase_overrides IS NULL AND btrim(resume_request_message) NOT IN ('','你好，能发一份简历吗？');

UPDATE position_auto_reply_configs SET phrase_overrides='[]'::jsonb WHERE phrase_overrides IS NULL;
ALTER TABLE position_auto_reply_configs ALTER COLUMN phrase_overrides SET DEFAULT '[]'::jsonb,
    ALTER COLUMN phrase_overrides SET NOT NULL;
