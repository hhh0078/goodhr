-- 本迁移新增浏览器插件公开下载配置：Chrome 与 Firefox 共用版本号，下载地址由超级管理员维护；不修改本地程序版本。
INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES ('system.browser_extension', '{"version":"1.0.2","note":"自动查重、独立备注、推荐进展和AI总结都补齐了。Chrome 与 Firefox 共用版本号，当前为开发包。","url_chrome":"/downloads/goodhr-extension-chrome-1.0.2.zip","url_firefox":"/downloads/goodhr-extension-firefox-1.0.2.zip","sha256_chrome":"20cc217154bd1fa17ec0442a1a669dd9cc84625cec0b0dab566e30836a7ffc4c","sha256_firefox":"16156c049370229e01262c30bd4fbc2fb747b8688b2dc11397ad1d048a10e998","firefox_signed":false}', '浏览器插件统一版本、Chrome/Firefox 下载地址、校验值和更新说明', true)
ON CONFLICT (config_key) DO NOTHING;
