-- 本迁移新增浏览器插件公开下载配置：Chrome 与 Firefox 共用版本号，下载地址由超级管理员维护；不修改本地程序版本。
INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES ('system.browser_extension', '{"version":"1.0.0","note":"首版简历采集插件，Chrome 与 Firefox 共用版本号。当前为开发安装包。","url_chrome":"/downloads/goodhr-extension-chrome-1.0.0.zip","url_firefox":"/downloads/goodhr-extension-firefox-1.0.0.zip","sha256_chrome":"f0a52c490adb63d2d4a086e32be8caee21fbd0e5c73cff92600f74efabae70de","sha256_firefox":"f159f4b61176d5c909031d0bc0ba7f4b20b7bc40eab1ea739765a168f755ec46","firefox_signed":false}', '浏览器插件统一版本、Chrome/Firefox 下载地址、校验值和更新说明', true)
ON CONFLICT (config_key) DO NOTHING;
