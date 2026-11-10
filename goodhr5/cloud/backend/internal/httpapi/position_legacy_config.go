// Package httpapi 本文件识别原始岗位中的旧关键词与OCR配置，避免模式转换掩盖启动风险。
package httpapi

import "strings"

const positionRecreateRequiredMessage = "这个岗位还在使用旧版配置，暂时不能启动。请先复制岗位名称和要求，再删除该岗位并重新创建。"

// positionRequiresRecreate 判断原始配置是否包含已停用的关键词模式或OCR详情识别。
// position 为未执行平台模式转换的岗位，返回是否必须重新创建。
func positionRequiresRecreate(position Position) bool {
	return strings.EqualFold(strings.TrimSpace(positionConfigString(position.CommonConfig, "mode_default")), "keyword") ||
		strings.EqualFold(strings.TrimSpace(positionConfigString(position.CommonConfig, "detail_mode")), "ocr")
}
