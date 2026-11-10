// Package cloudserver 本文件通过真实Platform、Broker、设备Executor及服务器命令账本核验在线资料协议与跨岗位凭据。
package cloudserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/replybridge"
)

// onlineContractRuntime 只替换资料采集原子结果，真实桥接调度、设备白名单和命令证据写入均不替换。
type onlineContractRuntime struct {
	model.Runtime
	model.AutoReplyRuntime
	profile  model.AutoReplyOnlineProfile
	err      error
	reads    int
	observed model.AutoReplyConversationSnapshot
}

// ReadAutoReplyOnlineProfile 回传合成真实PNG并记录设备实际收到的候选人身份，不启动浏览器或调用AI。
func (r *onlineContractRuntime) ReadAutoReplyOnlineProfile(_ context.Context, _ model.Browser, _ model.Config, s model.AutoReplyConversationSnapshot) (model.AutoReplyOnlineProfile, error) {
	r.reads++
	r.observed = s
	return r.profile, r.err
}

// TestOnlineProfilePlatformExecutorLeaseContract 验证从岗位A启动但处理岗位B时，真实设备白名单可消费命令且只保存B的来源摘要。
func TestOnlineProfilePlatformExecutorLeaseContract(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(map[bool]string{false: "image", true: "unavailable"}[unavailable], func(t *testing.T) {
			r, fixture, conversation, _, _, _ := serverGoalFixture(t)
			conversation.PositionID = "position-B"
			if _, err := r.client.SaveAutoReplyConversation(context.Background(), r.cred, conversation); err != nil {
				t.Fatal(err)
			}
			var picture bytes.Buffer
			if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
				t.Fatal(err)
			}
			device := &onlineContractRuntime{profile: model.AutoReplyOnlineProfile{Mode: "ai", CandidateName: "合成人选", RawText: "PRIVATE-RAW", SourceKey: "online-image:synthetic-bound-source", Screenshots: [][]byte{picture.Bytes()}}}
			if unavailable {
				device.err = model.ErrAutoReplyOnlineProfileUnavailable
			}
			broker := replybridge.NewBroker(r.persistCommand)
			broker.RunID, broker.PlatformID = r.id, r.platform
			platform := replybridge.NewPlatform(r.platform, broker.Call, t.TempDir())
			executor := &replybridge.Executor{PlatformID: r.platform, Runtime: device}
			snapshot := model.AutoReplyConversationSnapshot{CandidateName: "合成人选", PlatformCandidateID: "synthetic-candidate", PlatformAccountID: "account", PlatformThreadID: "thread"}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var got model.AutoReplyOnlineProfile
			var readErr error
			done := make(chan struct{})
			go func() {
				got, readErr = platform.ReadAutoReplyOnlineProfile(ctx, nil, model.Config{}, snapshot)
				close(done)
			}()
			command, err := broker.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if command.Action != "read_online_profile" {
				t.Fatalf("Platform未使用设备实际白名单协议：%s", command.Action)
			}
			result := executor.Execute(ctx, command)
			if err = broker.Complete(ctx, result); err != nil {
				t.Fatal(err)
			}
			<-done
			if device.reads != 1 || device.observed.PlatformCandidateID != snapshot.PlatformCandidateID {
				t.Fatalf("设备未收到原身份或重复执行：%+v", device)
			}
			if unavailable {
				if !errors.Is(readErr, model.ErrAutoReplyOnlineProfileUnavailable) {
					t.Fatalf("不可用哨兵跨协议丢失：%v", readErr)
				}
			} else if readErr != nil || len(got.Screenshots) != 1 || !bytes.Equal(got.Screenshots[0], picture.Bytes()) {
				t.Fatalf("服务器等待者未获得真实截图：%v", readErr)
			}
			var proof commandState
			fixture.mu.Lock()
			raw := append([]byte(nil), fixture.data["runtime_commands|"+command.ID].Payload...)
			fixture.mu.Unlock()
			if err = json.Unmarshal(raw, &proof); err != nil || proof.PositionID != "position-B" || proof.ConversationID != conversation.ID || proof.ThreadID != "thread" || proof.AccountID != "account" || proof.Status != "completed" || proof.Action != "read_online_profile" {
				t.Fatalf("跨岗位命令证据不完整：%+v %v", proof, err)
			}
			if !unavailable && proof.SourceKey != device.profile.SourceKey {
				t.Fatal("API所需真实来源凭据没有入库")
			}
			if unavailable && proof.ResultCode != "ONLINE_PROFILE_UNAVAILABLE" {
				t.Fatal("不可用错误被写成成功证据")
			}
			if strings.Contains(string(raw), "PRIVATE-RAW") || strings.Contains(string(raw), "screenshots") || strings.Contains(string(raw), "合成人选") {
				t.Fatal("图片或正文进入了命令证据")
			}
			if r.position != "p" || r.leaseKey != "lease:one" {
				t.Fatal("按实际岗位绑定破坏了启动任务租约")
			}
		})
	}
}

// TestNativeInfoUsesActualConversationPosition 验证原生资料与普通发送均绑定B岗位，A会话的意图不能用于B的命令。
func TestNativeInfoUsesActualConversationPosition(t *testing.T) {
	for _, action := range []string{"request_info", "send"} {
		t.Run(action, func(t *testing.T) {
			r, fixture, b, _, _, _ := serverGoalFixture(t)
			b.PositionID = "position-B"
			if _, err := r.client.SaveAutoReplyConversation(context.Background(), r.cred, b); err != nil {
				t.Fatal(err)
			}
			a, err := r.client.SaveAutoReplyConversation(context.Background(), r.cred, cloud.AutoReplyConversation{PlatformID: r.platform, PlatformAccountID: "account", PlatformThreadID: "other-thread", PositionID: "p"})
			if err != nil {
				t.Fatal(err)
			}
			key, content := "info-request:phone", "平台资料索要：phone"
			request := replybridge.Request{InfoRequest: model.CandidateInfoRequest{RequestPhone: true}, Snapshot: model.AutoReplyConversationSnapshot{PlatformThreadID: b.PlatformThreadID, PlatformAccountID: b.PlatformAccountID}}
			if action == "send" {
				key, content = "answer", "合成答复"
				request.Message = content
			}
			for _, conv := range []cloud.AutoReplyConversation{a, b} {
				if _, claimed, err := r.client.ClaimAutoReplySend(context.Background(), r.cred, conv.ID, key, content, 0); err != nil || !claimed {
					t.Fatal(claimed, err)
				}
			}
			payload, _ := json.Marshal(request)
			command := replybridge.Command{ID: "native-multiple-position", RunID: r.id, PlatformID: r.platform, Action: action, Deadline: time.Now().Add(time.Minute), Payload: payload}
			wrong := cloud.WithAutoReplySendIntent(context.Background(), cloud.AutoReplySendIntentID(a.ID, key, 0))
			if err := r.persistCommand(wrong, command, "pending", replybridge.Result{}); err == nil {
				t.Fatal("A岗位会话意图用于B岗位命令")
			}
			right := cloud.WithAutoReplySendIntent(context.Background(), cloud.AutoReplySendIntentID(b.ID, key, 0))
			if err := r.persistCommand(right, command, "pending", replybridge.Result{}); err != nil {
				t.Fatal(err)
			}
			var proof commandState
			if err := json.Unmarshal(fixture.data["runtime_commands|"+command.ID].Payload, &proof); err != nil || proof.PositionID != "position-B" || proof.ConversationID != b.ID {
				t.Fatalf("原生或普通发送仍用了启动岗位：%+v %v", proof, err)
			}
		})
	}
}
