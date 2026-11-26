// Package ai 文件作用：在候选人AI返回边界兼容无歧义的空值和数字表示，保留原始字段位置，不修改云端接口类型。
package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// candidateJSONField 保存外部JSON字段及其在最外层对象中的真实字节位置。
type candidateJSONField struct {
	Raw    json.RawMessage
	Offset int64
}

// candidateObject 读取对象字段和偏移，保留最后一次同名字段，拒绝语法损坏与非对象结果。
func candidateObject(raw []byte, offset int64) (map[string]candidateJSONField, error) {
	var checked map[string]json.RawMessage
	if err := json.Unmarshal(raw, &checked); err != nil {
		return nil, err
	}
	if checked == nil {
		return nil, &json.UnmarshalTypeError{Value: "null", Type: reflect.TypeOf(checked), Offset: offset + int64(len(raw))}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	_, _ = decoder.Token()
	fields := make(map[string]candidateJSONField, len(checked))
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[strings.ToLower(key.(string))] = candidateJSONField{Raw: value, Offset: offset + decoder.InputOffset() - int64(len(value))}
	}
	return fields, nil
}

// candidateFieldError 标出具体字段与原始值结束位置，不把规范化后的JSON偏移冒充原文位置。
func candidateFieldError(field candidateJSONField, path string, target reflect.Type) error {
	value := "object"
	raw := bytes.TrimSpace(field.Raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		value = "null"
	} else {
		switch raw[0] {
		case '"':
			value = "string"
		case '[':
			value = "array"
		case 't', 'f':
			value = "bool"
		case '{':
		default:
			value = "number"
		}
	}
	return &json.UnmarshalTypeError{Value: value, Type: target, Field: path, Offset: field.Offset + int64(len(field.Raw))}
}

// candidateNumber 复用流式评分的有限数字规则，不接受布尔值、单位字符串或非有限数字。
func candidateNumber(field candidateJSONField, path string) (float64, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(field.Raw))
	decoder.UseNumber()
	if decoder.Decode(&value) == nil {
		if number, ok := numberValue(value); ok {
			return number, nil
		}
	}
	return 0, candidateFieldError(field, path, reflect.TypeOf(float64(0)))
}

// normalizeCandidateField 只按现有简历类型处理确定的表示差异，无法确认的内容继续返回字段错误。
func normalizeCandidateField(field candidateJSONField, target reflect.Type, path string) (json.RawMessage, error) {
	raw := bytes.TrimSpace(field.Raw)
	if bytes.Equal(raw, []byte("null")) {
		return raw, nil
	}
	switch target.Kind() {
	case reflect.String:
		if len(raw) > 0 && raw[0] == '"' {
			return raw, nil
		}
		if len(raw) > 0 && (raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') {
			// 号码、年份只保留原数字文字，不经过float64，避免长号码精度损失。
			return json.Marshal(string(raw))
		}
	case reflect.Pointer:
		if target.Elem().Kind() == reflect.Float64 {
			var text string
			if json.Unmarshal(raw, &text) == nil && strings.TrimSpace(text) == "" {
				return json.RawMessage("null"), nil
			}
			number, err := candidateNumber(field, path)
			if err != nil {
				return nil, err
			}
			return json.Marshal(number)
		}
	case reflect.Struct:
		fields, err := candidateObject(raw, field.Offset)
		if err != nil {
			return nil, candidateFieldError(field, path, target)
		}
		result := make(map[string]json.RawMessage, len(fields))
		for key, value := range fields {
			result[key] = value.Raw
		}
		for i := 0; i < target.NumField(); i++ {
			property := target.Field(i)
			key := strings.Split(property.Tag.Get("json"), ",")[0]
			value, exists := fields[key]
			if !exists || key == "-" {
				continue
			}
			fieldPath := key
			if path != "" {
				fieldPath = path + "." + key
			}
			result[key], err = normalizeCandidateField(value, property.Type, fieldPath)
			if err != nil {
				return nil, err
			}
		}
		return json.Marshal(result)
	case reflect.Slice:
		var empty string
		if json.Unmarshal(raw, &empty) == nil && strings.TrimSpace(empty) == "" {
			return json.RawMessage("[]"), nil
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) == nil {
			decoder := json.NewDecoder(bytes.NewReader(raw))
			_, _ = decoder.Token()
			result := make([]json.RawMessage, 0, len(items))
			for i := range items {
				_ = decoder.Decode(&items[i])
				// null 是缺失占位，不能变成一条虚构的空经历。
				if bytes.Equal(bytes.TrimSpace(items[i]), []byte("null")) {
					continue
				}
				item := candidateJSONField{Raw: items[i], Offset: field.Offset + decoder.InputOffset() - int64(len(items[i]))}
				value, err := normalizeCandidateField(item, target.Elem(), path+"["+strconv.Itoa(i)+"]")
				if err != nil {
					return nil, err
				}
				result = append(result, value)
			}
			return json.Marshal(result)
		}
	default:
		return nil, fmt.Errorf("AI 简历字段 %s 暂不支持此类型", path)
	}
	return nil, candidateFieldError(field, path, target)
}
