// Package model 本文件统一固定的详情识别策略，让历史OCR配置在流程边界按平台兼容。
package model

import "strings"

// DetailRecognitionMode 返回平台固定的详情识别方式；旧ocr、ai、dom选项不再决定执行路径。
func DetailRecognitionMode(platformID string) string {
	if strings.EqualFold(strings.TrimSpace(platformID), "boss") {
		return "ai"
	}
	return "dom"
}
