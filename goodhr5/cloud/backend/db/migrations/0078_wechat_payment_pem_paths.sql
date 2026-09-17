-- 本迁移为微信支付配置增加 PEM 文件路径，避免在系统配置中复制或保存完整证书内容。
UPDATE system_configs
SET config_value = config_value
  || '{"private_key_path":"","public_key_path":""}'::jsonb,
    description = '微信支付配置：商户私钥和微信支付公钥优先使用容器内文件路径，修改配置或替换文件后自动生效',
    updated_at = NOW()
WHERE config_key = 'system.payment_wechat'
  AND (
    NOT config_value ? 'private_key_path'
    OR NOT config_value ? 'public_key_path'
  );
