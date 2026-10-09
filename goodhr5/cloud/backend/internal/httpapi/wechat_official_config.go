// 本文件负责公众号私有配置的读取、校验和脱敏，密钥不进入公共系统配置。
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const wechatOfficialConfigKey = "system.wechat_official_account"
const defaultWechatFollowRewardDays = 2

// loadWechatFollowRewardDays 从注册赠送所在的配置组读取关注天数，缺失时默认赠送两天。
func loadWechatFollowRewardDays(store SystemConfigStore) (int, error) {
	if store == nil {
		return defaultWechatFollowRewardDays, nil
	}
	record, err := store.Get("system.onboarding_config")
	if errors.Is(err, ErrConfigNotFound) {
		return defaultWechatFollowRewardDays, nil
	}
	if err != nil {
		return 0, err
	}
	return parseWechatFollowRewardDays(record.ConfigValue)
}

// parseWechatFollowRewardDays 校验关注赠送的整数范围，不修改注册试用和本地更新字段。
func parseWechatFollowRewardDays(raw string) (int, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return 0, errors.New("本地程序与组件配置需要是 JSON 对象")
	}
	value, exists := fields["wechat_follow_reward_days"]
	if !exists {
		return defaultWechatFollowRewardDays, nil
	}
	var days int
	if string(value) == "null" || json.Unmarshal(value, &days) != nil || days < 1 || days > 365 {
		return 0, errors.New("关注赠送天数请填写 1 到 365 的整数")
	}
	return days, nil
}

// WechatOfficialConfig 保存回调与关注绑定配置，仅允许超级管理员编辑。
type WechatOfficialConfig struct {
	Enabled        bool   `json:"enabled"`
	AccountName    string `json:"account_name"`
	AppID          string `json:"app_id"`
	OriginalID     string `json:"original_id"`
	Token          string `json:"token"`
	EncodingAESKey string `json:"encoding_aes_key"`
	QRImageURL     string `json:"qr_image_url"`
	MessageMode    string `json:"message_mode"`
}

// defaultWechatOfficialConfig 返回不含任何凭据、默认停用的公众号配置。
func defaultWechatOfficialConfig() WechatOfficialConfig {
	return WechatOfficialConfig{AccountName: "GoodHR", MessageMode: "safe"}
}

// loadWechatOfficialConfig 读取独立私有配置并填充展示默认值。
func loadWechatOfficialConfig(store SystemConfigStore) (WechatOfficialConfig, error) {
	config := defaultWechatOfficialConfig()
	if store == nil {
		return config, nil
	}
	record, err := store.Get(wechatOfficialConfigKey)
	if errors.Is(err, ErrConfigNotFound) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	if err := json.Unmarshal([]byte(record.ConfigValue), &config); err != nil {
		return config, errors.New("公众号配置暂时没读好，请检查其他配置")
	}
	config.AccountName = strings.TrimSpace(config.AccountName)
	if config.AccountName == "" {
		config.AccountName = "GoodHR"
	}
	if config.QRImageURL == "" && config.OriginalID != "" {
		config.QRImageURL = "https://open.weixin.qq.com/qr/code?username=" + url.QueryEscape(config.OriginalID)
	}
	config.Enabled = config.Enabled && record.Enabled
	return config, nil
}

// configured 判断微信回调所需参数是否完整有效。
func (c WechatOfficialConfig) configured() bool {
	return c.AppID != "" && c.OriginalID != "" && c.Token != "" && c.EncodingAESKey != "" && validateWechatOfficialConfig(c) == nil
}

// validateWechatOfficialConfig 校验已填写的值，启用时要求所有回调参数齐全。
func validateWechatOfficialConfig(c WechatOfficialConfig) error {
	if c.MessageMode != "" && c.MessageMode != "safe" && c.MessageMode != "compat" && c.MessageMode != "plaintext" {
		return errors.New("消息模式请选安全、兼容或明文模式")
	}
	if c.AppID != "" && !regexp.MustCompile(`^wx[0-9a-fA-F]{16}$`).MatchString(c.AppID) {
		return errors.New("AppID 请填写 wx 开头的 18 位编号")
	}
	if c.OriginalID != "" && !regexp.MustCompile(`^gh_[a-zA-Z0-9_]{1,64}$`).MatchString(c.OriginalID) {
		return errors.New("原始 ID 请填写 gh_ 开头的编号")
	}
	if c.Token != "" && !regexp.MustCompile(`^[a-zA-Z0-9]{3,32}$`).MatchString(c.Token) {
		return errors.New("Token 请用 3 到 32 位英文字母或数字")
	}
	if c.EncodingAESKey != "" {
		key, err := base64.StdEncoding.DecodeString(c.EncodingAESKey + "=")
		if len(c.EncodingAESKey) != 43 || err != nil || len(key) != 32 {
			return errors.New("EncodingAESKey 请填写微信后台的 43 位密钥")
		}
	}
	if len(c.AccountName) > 120 {
		return errors.New("公众号名称太长了，请缩短一点")
	}
	if c.QRImageURL != "" {
		u, err := url.Parse(c.QRImageURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(c.QRImageURL) > 2048 {
			return errors.New("二维码图片请填写完整 HTTPS 链接")
		}
	}
	if c.Enabled && (c.AppID == "" || c.OriginalID == "" || c.Token == "" || c.EncodingAESKey == "") {
		return errors.New("先补齐 AppID、原始 ID、Token 和 EncodingAESKey，再开启关注奖励")
	}
	return nil
}

// mergeWechatOfficialConfig 保留未填写的密钥和未知字段，防止脱敏结果覆盖真实凭据。
func mergeWechatOfficialConfig(existing, incoming string) (string, error) {
	var oldValues, values map[string]json.RawMessage
	if err := json.Unmarshal([]byte(existing), &oldValues); err != nil {
		return "", errors.New("原公众号配置无法读取，请先检查配置")
	}
	if oldValues == nil {
		oldValues = make(map[string]json.RawMessage)
	}
	if err := json.Unmarshal([]byte(incoming), &values); err != nil || values == nil {
		return "", errors.New("公众号配置需要是 JSON 对象")
	}
	var err error
	oldValues, err = canonicalWechatOfficialFields(oldValues)
	if err != nil {
		return "", err
	}
	values, err = canonicalWechatOfficialFields(values)
	if err != nil {
		return "", err
	}
	for key, value := range values {
		if key != "encoding_aes_key_configured" && key != "callback_url" && key != "reward_days" {
			oldValues[key] = value
		}
	}
	var newKey string
	if key, exists := values["encoding_aes_key"]; exists {
		if err := json.Unmarshal(key, &newKey); err != nil {
			return "", errors.New("EncodingAESKey 需要填写文字")
		}
		if strings.TrimSpace(newKey) == "" || newKey == "********" {
			var old WechatOfficialConfig
			_ = json.Unmarshal([]byte(existing), &old)
			oldValues["encoding_aes_key"], _ = json.Marshal(old.EncodingAESKey)
		}
	}
	delete(oldValues, "encoding_aes_key_configured")
	delete(oldValues, "callback_url")
	delete(oldValues, "reward_days")
	merged, err := json.Marshal(oldValues)
	if err != nil {
		return "", err
	}
	var config WechatOfficialConfig
	if err := json.Unmarshal(merged, &config); err != nil {
		return "", errors.New("公众号配置的字段格式没填对")
	}
	config.AppID = strings.TrimSpace(config.AppID)
	config.OriginalID = strings.TrimSpace(config.OriginalID)
	config.Token = strings.TrimSpace(config.Token)
	config.EncodingAESKey = strings.TrimSpace(config.EncodingAESKey)
	config.QRImageURL = strings.TrimSpace(config.QRImageURL)
	config.AccountName = strings.TrimSpace(config.AccountName)
	if err := validateWechatOfficialConfig(config); err != nil {
		return "", err
	}
	// 规范字段覆盖回原对象，保留未来版本或管理员补充的其他配置。
	normalized, _ := json.Marshal(config)
	var known map[string]json.RawMessage
	_ = json.Unmarshal(normalized, &known)
	for key, value := range known {
		oldValues[key] = value
	}
	merged, err = json.Marshal(oldValues)
	return string(merged), err
}

// canonicalWechatOfficialFields 统一已知字段大小写，拒绝歧义别名，避免密钥藏在未脱敏的重复键中。
func canonicalWechatOfficialFields(values map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		canonical := key
		switch strings.ToLower(key) {
		case "enabled", "account_name", "app_id", "original_id", "token", "encoding_aes_key", "qr_image_url", "message_mode", "encoding_aes_key_configured", "callback_url", "reward_days":
			canonical = strings.ToLower(key)
		}
		if _, exists := result[canonical]; exists {
			return nil, errors.New("公众号配置有重复字段，请保留同一字段的一份值")
		}
		result[canonical] = value
	}
	return result, nil
}

// redactWechatOfficialRecord 返回超管可用的配置，真实 AES 密钥始终留在服务端。
func redactWechatOfficialRecord(record SystemConfig, rewardDays ...int) SystemConfig {
	if record.ConfigKey != wechatOfficialConfigKey {
		return record
	}
	var values map[string]any
	if json.Unmarshal([]byte(record.ConfigValue), &values) != nil || values == nil {
		values = map[string]any{}
	}
	configured := false
	for field, value := range values {
		if strings.EqualFold(field, "encoding_aes_key") {
			key, _ := value.(string)
			configured = configured || key != ""
			delete(values, field)
		}
	}
	values["encoding_aes_key"] = ""
	values["encoding_aes_key_configured"] = configured
	days := defaultWechatFollowRewardDays
	if len(rewardDays) > 0 && rewardDays[0] >= 1 && rewardDays[0] <= 365 {
		days = rewardDays[0]
	}
	values["reward_days"] = days
	values["callback_url"] = "https://goodhr.58it.cn/api/wechat-official/callback"
	raw, _ := json.Marshal(values)
	record.ConfigValue = string(raw)
	return record
}
