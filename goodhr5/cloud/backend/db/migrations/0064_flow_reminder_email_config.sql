-- 本迁移更新流程提醒邮件的统一作者信息，并让八个流程模板使用代码中的最新教程模板。
UPDATE system_configs
SET config_value = jsonb_set(
    jsonb_set(
        config_value,
        '{wechat}',
        to_jsonb('17607080935'::text),
        true
    ),
    '{website}',
    to_jsonb('https://goodhr5.58it.cn'::text),
    true
)
    #- '{templates,agent_detected}'
    #- '{templates,runtime_ready}'
    #- '{templates,position_created}'
    #- '{templates,task_created}'
    #- '{templates,platform_login_verified}'
    #- '{templates,task_started}'
    #- '{templates,first_resume_processed}'
    #- '{templates,first_greet_success}',
    updated_at = now()
WHERE config_key = 'system.email_recovery';
