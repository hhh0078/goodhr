-- 本迁移新增微信支付系统配置，让超级管理员可以在后台在线维护微信支付参数，保存后立即生效。
INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
  'system.payment_wechat',
  '{
    "app_id": "",
    "mch_id": "",
    "merchant_serial_no": "",
    "private_key_base64": "",
    "api_v3_key": "",
    "public_key_id": "",
    "public_key_base64": "",
    "notify_url": ""
  }'::jsonb,
  '微信支付配置：应用ID(app_id)、商户号(mch_id)、商户证书序列号(merchant_serial_no)、商户私钥(private_key_base64)、APIv3密钥(api_v3_key)、公钥ID(public_key_id)、公钥(public_key_base64)和支付回调地址(notify_url)，明文保存，修改后立即生效',
  true
)
ON CONFLICT (config_key) DO NOTHING;
