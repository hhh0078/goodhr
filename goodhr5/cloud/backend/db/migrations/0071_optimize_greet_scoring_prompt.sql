-- 文件作用说明：升级旧版打招呼评分提示词，明确岗位要求、缺失信息、加分项和通用维度的权重顺序。

DO $migration$
DECLARE
  prompt_value text := $greet$你是资深招聘顾问。请根据“岗位要求”和“候选人信息”给出【打招呼建议分】。

任务目标：
- 这一步只判断是否值得立即打招呼，不是最终录用结论。
- 分数用于直接和岗位的打招呼阈值比较。

评分权重（必须按顺序执行）：
1. 岗位要求中的硬性条件权重最高。
2. 岗位要求中的普通条件是主要权重。
3. 岗位要求标注的“优先、加分、最好”等加分项只占低权重。
4. 岗位要求没有写出的通用维度只占很低权重。

岗位要求逐项判断：
- 候选人信息明确符合：该项按完全满足计分。
- 候选人信息没有提及：标记为“待核验”，按可能满足处理并保留该项大部分分值，不能按不符合扣分。
- 候选人信息明确冲突：才能判定该项不符合；明确冲突的硬性条件必须显著降分。
- 加分项符合时只能小幅加分，未提及或不符合都不能扣分。

辅助思考：
- 可以低权重参考角色相关性、岗位方向、学历、工作年限、行业经验、到岗状态、稳定性、岗位连续性及明确风险。
- 上述维度只有被用户写进岗位要求时，才按对应的硬性条件、普通条件或加分项计权；否则不能压过岗位要求。
- 不得把“未展示”推断为“不满足”，也不能凭空补充用户没有提出的筛选条件。

评分标准（0-100）：
- 85-100：明确符合主要岗位要求，建议优先打招呼。
- 70-84：没有明确硬性冲突，部分条件待核验，建议打招呼。
- 55-69：存在明确弱匹配或较多不确定项，建议谨慎打招呼。
- 0-54：存在明确的硬性条件冲突，不建议打招呼。

输出约束：
- 只输出 JSON，不要任何额外文字。
- score 为 0-100 数字，可以是小数。
- reason 控制在 30 字以内，优先说明岗位要求中最关键条件的匹配、待核验或明确冲突情况。
- 严格按照以下 JSON 结构返回，没有的信息保持为空：
${结构化简历}$greet$;
BEGIN
  -- 系统配置只升级仍然包含旧版评分规则的默认提示词。
  UPDATE system_configs
  SET config_value = jsonb_set(config_value, '{filter_prompt}', to_jsonb(prompt_value), true)
  WHERE config_key = 'ai.default_prompts'
    AND coalesce(config_value->>'filter_prompt', '') LIKE '%重点评估维度（按优先级）%'
    AND coalesce(config_value->>'filter_prompt', '') LIKE '%信息缺失时可适度降分%';

  -- 岗位只升级旧默认字段，用户自己编写的其它提示词保持原样。
  UPDATE positions
  SET ai_config = coalesce(ai_config, '{}'::jsonb)
    || CASE
      WHEN coalesce(ai_config->>'filter_prompt', '') LIKE '%重点评估维度（按优先级）%'
        AND coalesce(ai_config->>'filter_prompt', '') LIKE '%信息缺失时可适度降分%'
      THEN jsonb_build_object('filter_prompt', prompt_value)
      ELSE '{}'::jsonb
    END
    || CASE
      WHEN coalesce(ai_config->>'greet_prompt', '') LIKE '%重点评估维度（按优先级）%'
        AND coalesce(ai_config->>'greet_prompt', '') LIKE '%信息缺失时可适度降分%'
      THEN jsonb_build_object('greet_prompt', prompt_value)
      ELSE '{}'::jsonb
    END
    || CASE
      WHEN coalesce(ai_config->>'click_prompt', '') LIKE '%重点评估维度（按优先级）%'
        AND coalesce(ai_config->>'click_prompt', '') LIKE '%信息缺失时可适度降分%'
      THEN jsonb_build_object('click_prompt', prompt_value)
      ELSE '{}'::jsonb
    END
  WHERE (
      coalesce(ai_config->>'filter_prompt', '') LIKE '%重点评估维度（按优先级）%'
      AND coalesce(ai_config->>'filter_prompt', '') LIKE '%信息缺失时可适度降分%'
    ) OR (
      coalesce(ai_config->>'greet_prompt', '') LIKE '%重点评估维度（按优先级）%'
      AND coalesce(ai_config->>'greet_prompt', '') LIKE '%信息缺失时可适度降分%'
    ) OR (
      coalesce(ai_config->>'click_prompt', '') LIKE '%重点评估维度（按优先级）%'
      AND coalesce(ai_config->>'click_prompt', '') LIKE '%信息缺失时可适度降分%'
    );
END
$migration$;
