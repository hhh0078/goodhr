// Package replybridge 本文件将在线简历图片限制在一次性RAM回执，权威命令审计仅接收来源摘要而不保存图片或原文。
package replybridge

import (
	"encoding/json"
	"fmt"
	"net/http"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// ValidateOnlineProfileScreenshots 在设备回传和服务器消费前核对图像数量、真实格式及总字节数，拒绝任意大二进制。
func ValidateOnlineProfileScreenshots(images [][]byte) error {
	if len(images) > 5 {
		return fmt.Errorf("在线资料截图超过5张，请分段后再试")
	}
	var total int
	for _, data := range images {
		total += len(data)
		mime := http.DetectContentType(data)
		if len(data) == 0 || len(data) > 20<<20 || total > 64<<20 || mime != "image/png" && mime != "image/jpeg" {
			return fmt.Errorf("在线资料截图为空、格式不支持或超过传输限制")
		}
	}
	return nil
}

// onlineProfilePersistenceResult 仅过滤持久化副本，等待中的服务器Flow仍收到原始RAM图片；重启后必须重新只读采集。
func onlineProfilePersistenceResult(command Command, result Result) (Result, error) {
	if command.Action != "read_online_profile" || result.Error != "" {
		return result, nil
	}
	var response Response
	if err := json.Unmarshal(result.Value, &response); err != nil {
		return Result{}, fmt.Errorf("在线资料设备回执格式不完整")
	}
	if err := ValidateOnlineProfileScreenshots(response.OnlineProfile.Screenshots); err != nil {
		return Result{}, err
	}
	// 只持久化当前可见来源的模式与摘要；姓名、原文、图片和任意其它回执字段都不会通过此副本落库。
	metadata := Response{ObservedAt: response.ObservedAt, OnlineProfile: model.AutoReplyOnlineProfile{Mode: response.OnlineProfile.Mode, SourceKey: response.OnlineProfile.SourceKey}}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return Result{}, err
	}
	result.Value = raw
	return result, nil
}
