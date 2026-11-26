// 本文件负责备用AI的超管配置、密钥留空保留与并发安全保存，不向普通用户或公开配置返回密钥。
package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"unicode"
)

// BuiltinAIFallbackConfig 定义仅服务端使用的备用平台；重试时三项配置必须一起切换。
type BuiltinAIFallbackConfig struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// aiFallbackView 仅返回备用平台的可编辑信息及已设置状态，不回传密钥原值或掩码代替值。
type aiFallbackView struct {
	Enabled   bool   `json:"enabled"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	APIKeySet bool   `json:"api_key_set"`
}

// aiFallbackRevision 对独立备用字段计算版本，其他公共配置修改不会误伤正在编辑的备用表单。
func aiFallbackRevision(raw json.RawMessage) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// aiFallbackDocument 只拆开已有公共配置，不填默认主用平台，不改已有模型、价格或其他业务数据。
func aiFallbackDocument(record SystemConfig) (map[string]json.RawMessage, map[string]json.RawMessage, BuiltinAIFallbackConfig, json.RawMessage, error) {
	var app map[string]json.RawMessage
	if err := json.Unmarshal([]byte(record.ConfigValue), &app); err != nil || app == nil {
		return nil, nil, BuiltinAIFallbackConfig{}, nil, fmt.Errorf("公共AI配置暂时没读出来")
	}
	builtin := make(map[string]json.RawMessage)
	if raw := app["builtin_ai"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &builtin); err != nil || builtin == nil {
			return nil, nil, BuiltinAIFallbackConfig{}, nil, fmt.Errorf("主用AI配置格式需要先检查")
		}
	}
	raw := builtin["fallback"]
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	var cfg BuiltinAIFallbackConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, cfg, nil, fmt.Errorf("备用AI配置格式需要先检查")
	}
	return app, builtin, cfg, raw, nil
}

// validateAIFallbackConfig 检查备用接口及完整三项配置，禁用时允许留空，不猜测模型或借用主用密钥。
func validateAIFallbackConfig(cfg BuiltinAIFallbackConfig) error {
	if cfg.Enabled && (cfg.BaseURL == "" || cfg.APIKey == "" || cfg.Model == "") {
		return fmt.Errorf("启用备用AI前，请填好接口地址、密钥和模型")
	}
	if cfg.BaseURL != "" {
		u, err := url.Parse(cfg.BaseURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(cfg.BaseURL) > 2048 {
			return fmt.Errorf("接口地址请填写完整的HTTP或HTTPS地址，不要夹带账号、密钥或查询参数")
		}
		// 备用防环按主机名比较，不能让HTTP、显式443端口或末尾点绕过。
		host := strings.TrimSuffix(u.Hostname(), ".")
		public, _ := url.Parse(deploymentPublicBaseURL())
		if strings.EqualFold(host, "goodhr.transit.ai.58it.cn") || public != nil && strings.EqualFold(host, strings.TrimSuffix(public.Hostname(), ".")) {
			return fmt.Errorf("备用地址请填AI服务商的接口，不能填GoodHR自身的中转地址")
		}
	}
	if len(cfg.Model) > 200 || strings.IndexFunc(cfg.Model, unicode.IsControl) >= 0 {
		return fmt.Errorf("模型名称太长或含有换行，请检查一下")
	}
	if len(cfg.APIKey) > 8192 || strings.IndexFunc(cfg.APIKey, unicode.IsControl) >= 0 {
		return fmt.Errorf("密钥太长或含有换行，请检查一下")
	}
	return nil
}

// adminAIFallbackConfig 仅超管可读写备用AI；密钥留空保留、独立版本检查和整行CAS共同防止覆盖。
func (s *Server) adminAIFallbackConfig(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "登录状态过期了，请重新登录")
		return
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "这个配置只向超级管理员开放")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "此接口仅支持读取或保存")
		return
	}
	record, err := s.systemConfigs.Get("system.app_config")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "公共AI配置暂时没读出来，请稍后重试")
		return
	}
	app, builtin, cfg, raw, err := aiFallbackDocument(record)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if r.Method == http.MethodPut {
		var request struct {
			BuiltinAIFallbackConfig
			Revision string `json:"revision"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
			writeError(w, http.StatusBadRequest, "配置内容没读明白，请检查后再保存")
			return
		}
		if request.Revision != aiFallbackRevision(raw) {
			writeError(w, http.StatusConflict, "备用AI配置已经变化，请重新打开页面后再保存")
			return
		}
		next := request.BuiltinAIFallbackConfig
		next.BaseURL = strings.TrimSpace(next.BaseURL)
		next.Model = strings.TrimSpace(next.Model)
		next.APIKey = strings.TrimSpace(next.APIKey)
		if next.APIKey == "" {
			next.APIKey = cfg.APIKey
		}
		if err := validateAIFallbackConfig(next); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		encoded, _ := json.Marshal(next)
		builtin["fallback"] = encoded
		builtinJSON, _ := json.Marshal(builtin)
		app["builtin_ai"] = builtinJSON
		updated, _ := json.Marshal(app)
		expected := record.ConfigValue
		record.ConfigValue = string(updated)
		saved, err := s.systemConfigs.SaveIfUnchanged(record, expected)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "备用AI没保存成功，请稍后重试")
			return
		}
		if !saved {
			writeError(w, http.StatusConflict, "系统配置刚有更新，请重新打开页面后再保存")
			return
		}
		cfg, raw = next, encoded
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": aiFallbackView{Enabled: cfg.Enabled, BaseURL: cfg.BaseURL, Model: cfg.Model, APIKeySet: strings.TrimSpace(cfg.APIKey) != ""}, "revision": aiFallbackRevision(raw)})
}

// appConfigFallbackParts 只拆出公共配置的备用原始字段，未修改的历史坏备用不影响其它公共选项保存。
func appConfigFallbackParts(value string) (map[string]json.RawMessage, map[string]json.RawMessage, json.RawMessage, error) {
	var app map[string]json.RawMessage
	if json.Unmarshal([]byte(value), &app) != nil || app == nil {
		return nil, nil, nil, fmt.Errorf("公共配置请使用JSON对象")
	}
	builtin := map[string]json.RawMessage{}
	if raw := app["builtin_ai"]; len(raw) > 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &builtin) != nil || builtin == nil {
			return nil, nil, nil, fmt.Errorf("主用AI配置格式需要先检查")
		}
	}
	raw := builtin["fallback"]
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	return app, builtin, raw, nil
}

// sameAIFallbackJSON 忽略对象字段顺序和排版，防止公共配置编辑器格式化触发备用误变更。
func sameAIFallbackJSON(first, second json.RawMessage) bool {
	if bytes.Equal(bytes.TrimSpace(first), bytes.TrimSpace(second)) {
		return true
	}
	var a, b any
	return json.Unmarshal(first, &a) == nil && json.Unmarshal(second, &b) == nil && reflect.DeepEqual(a, b)
}

// preserveAppConfigFallback 校验手工修改的备用，留空密钥保留；返回是否真的改变备用，供旧客户端并发保护使用。
func preserveAppConfigFallback(previous, incoming string) (string, bool, error) {
	_, _, oldRaw, err := appConfigFallbackParts(previous)
	if err != nil {
		return incoming, false, err
	}
	app, builtin, newRaw, err := appConfigFallbackParts(incoming)
	if err != nil {
		return incoming, false, err
	}
	if sameAIFallbackJSON(oldRaw, newRaw) {
		return incoming, false, nil
	}
	if string(newRaw) == "null" {
		return incoming, true, nil
	}
	var next BuiltinAIFallbackConfig
	if json.Unmarshal(newRaw, &next) != nil {
		return incoming, true, fmt.Errorf("备用AI配置格式需要先检查")
	}
	var oldKey struct {
		APIKey string `json:"api_key"`
	}
	_ = json.Unmarshal(oldRaw, &oldKey)
	next.BaseURL, next.Model, next.APIKey = strings.TrimSpace(next.BaseURL), strings.TrimSpace(next.Model), strings.TrimSpace(next.APIKey)
	if next.APIKey == "" {
		next.APIKey = oldKey.APIKey
	}
	if err := validateAIFallbackConfig(next); err != nil {
		return incoming, true, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(newRaw, &fields) != nil || fields == nil {
		return incoming, true, fmt.Errorf("备用AI配置请使用JSON对象")
	}
	for key, value := range map[string]any{"enabled": next.Enabled, "base_url": next.BaseURL, "api_key": next.APIKey, "model": next.Model} {
		fields[key], _ = json.Marshal(value)
	}
	normalized, _ := json.Marshal(fields)
	if sameAIFallbackJSON(oldRaw, normalized) {
		return incomingWithPreservedFallback(app, builtin, normalized), false, nil
	}
	return incomingWithPreservedFallback(app, builtin, normalized), true, nil
}

// incomingWithPreservedFallback 只更新备用字段，保留公共配置、主用模型价格和未识别的未来字段。
func incomingWithPreservedFallback(app, builtin map[string]json.RawMessage, fallback json.RawMessage) string {
	builtin["fallback"] = fallback
	app["builtin_ai"], _ = json.Marshal(builtin)
	result, _ := json.Marshal(app)
	return string(result)
}
