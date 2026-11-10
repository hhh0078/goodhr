// Package replybridge 本文件验证在线截图仅进入当前RAM等待者，命令持久化不会携带图片、原文或设备路径。
package replybridge

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// profileTestImage 创建无候选人数据的真实小PNG，验证真实格式而非假二进制。
func profileTestImage(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// TestOnlineProfileRAMReceiptKeepsImagesOutOfPersistence 验证持久化副本与正在等待的调用方收到的结果不同，重交同回执保持幂等。
func TestOnlineProfileRAMReceiptKeepsImagesOutOfPersistence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var persisted Result
	broker := NewBroker(func(_ context.Context, _ Command, status string, result Result) error {
		if status == "completed" {
			persisted = result
		}
		return nil
	})
	broker.RunID, broker.PlatformID = "run", "boss"
	var output Response
	finished := make(chan error, 1)
	go func() { finished <- broker.Call(ctx, "read_online_profile", Request{}, &output) }()
	command, err := broker.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data := profileTestImage(t)
	raw, _ := json.Marshal(Response{OnlineProfile: model.AutoReplyOnlineProfile{Mode: "ai", SourceKey: "online-image:fixture", CandidateName: "私人原文姓名", RawText: "PRIVATE-RAW", Screenshots: [][]byte{data}}})
	result := Result{ID: command.ID, Value: raw}
	if err = broker.Complete(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	if len(output.OnlineProfile.Screenshots) != 1 || !bytes.Equal(output.OnlineProfile.Screenshots[0], data) {
		t.Fatal("RAM调用方未收到真实截图")
	}
	if strings.Contains(string(persisted.Value), "PRIVATE-RAW") || strings.Contains(string(persisted.Value), "私人原文姓名") || strings.Contains(string(persisted.Value), "screenshots") {
		t.Fatal("截图或原文进入持久化副本")
	}
	var metadata Response
	if json.Unmarshal(persisted.Value, &metadata) != nil || metadata.OnlineProfile.SourceKey != "online-image:fixture" {
		t.Fatal("来源摘要丢失")
	}
	if err = broker.Complete(ctx, result); err != nil {
		t.Fatal("相同设备回执无法重交", err)
	}
}

// TestOnlineProfileImagesRejectUnknownBytes 验证空图、非图和超量图片不会被当成可识别简历。
func TestOnlineProfileImagesRejectUnknownBytes(t *testing.T) {
	if ValidateOnlineProfileScreenshots([][]byte{nil}) == nil || ValidateOnlineProfileScreenshots([][]byte{[]byte("/device/private.png")}) == nil {
		t.Fatal("非截图数据被接受")
	}
	data := profileTestImage(t)
	if ValidateOnlineProfileScreenshots([][]byte{data, data, data, data, data, data}) == nil {
		t.Fatal("超量截图被接受")
	}
}
