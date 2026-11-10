-- 文件作用：仅将上一版完整内置12项话术一次升级为含微信及必要条件不符场景的15项；任何自定义、清空、删改与岗位覆盖均保留。无表或字段变更。
-- 本文件由迁移账本只执行一次，管理员后续删除微信不会被日常读取或重启补回。
UPDATE system_configs
SET config_value = $new_library${
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
      "id": "request_wechat",
      "category": "联系方式",
      "title": "索要微信",
      "scene": "确需微信沟通，尚未获得、申请或被拒绝；平台支持时先核对双方沟通资格",
      "content": "如果方便后续沟通，可以交换微信；想在这里聊也可以。"
    },
    {
      "id": "receive_wechat",
      "category": "联系方式",
      "title": "收到微信",
      "scene": "候选人新提供了可核实的微信号或确认平台交换",
      "content": "微信收到了，谢谢，咱们也可以继续在这里聊。"
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
    },
    {
      "id": "required_mismatch",
      "category": "岗位沟通",
      "title": "必要条件明确不符",
      "scene": "当前HR必要条件已有真实明确反证；未知或资料缺少不能使用",
      "content": "这个岗位有项必要要求和您目前的情况不太一致，这次可能不太合适，感谢您愿意了解。"
    }
  ],
  "condition_options": [
    "能接受现场办公",
    "能接受轮班",
    "能接受短期出差",
    "能接受周末排班"
  ]
}$new_library$::jsonb, updated_at = now()
WHERE config_key = 'system.auto_reply_library'
AND config_value = $previous_library${
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
}$previous_library$::jsonb;
