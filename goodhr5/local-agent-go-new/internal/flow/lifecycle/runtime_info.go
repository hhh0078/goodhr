// Package lifecycle 本文件为原生资料索要记录耗时，并按真实实现保留详情、身份和联系人核验等可选能力。
package lifecycle

import (
	"context"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// loggedNativeInfoRuntime 保留原平台完整消息能力，新增方法只记录操作名称与耗时，不记录候选人资料。
type loggedNativeInfoRuntime struct {
	*loggedRuntime
	model.AutoReplyRuntime
	requester model.AutoReplyInfoRequester
}

// ReadAutoReplyOnlineProfile 保留真实平台的在线资料读取能力，缺少该可选能力时明确返回不可用。
func (l *loggedNativeInfoRuntime) ReadAutoReplyOnlineProfile(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (result model.AutoReplyOnlineProfile, err error) {
	defer l.record("read_online_profile", time.Now(), &err)
	reader, ok := l.Runtime.(model.AutoReplyOnlineProfileReader)
	if !ok {
		return result, model.ErrAutoReplyOnlineProfileUnavailable
	}
	return reader.ReadAutoReplyOnlineProfile(ctx, browser, cfg, snapshot)
}

// ReadAutoReplyOnlineProfile 在旧平台日志包装中安全转发在线读取；不伪造空成功，也不改变原有详情或身份能力。
func (l *loggedAutoReplyRuntime) ReadAutoReplyOnlineProfile(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (result model.AutoReplyOnlineProfile, err error) {
	defer l.record("read_online_profile", time.Now(), &err)
	reader, ok := l.Runtime.(model.AutoReplyOnlineProfileReader)
	if !ok {
		return result, model.ErrAutoReplyOnlineProfileUnavailable
	}
	return reader.ReadAutoReplyOnlineProfile(ctx, browser, cfg, snapshot)
}

// RequestAutoReplyCandidateInfo 记录原生资料索要并原样返回明确暂缓或已请求状态。
func (l *loggedNativeInfoRuntime) RequestAutoReplyCandidateInfo(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, request model.CandidateInfoRequest) (result model.AutoReplyInfoRequestResult, err error) {
	defer l.record("request_candidate_info", time.Now(), &err)
	return l.requester.RequestAutoReplyCandidateInfo(ctx, browser, cfg, snapshot, request)
}

// newLoggedNativeInfoRuntime 仅为确有原生索要能力的平台组装包装，不让旧平台被误判支持新能力。
func newLoggedNativeInfoRuntime(base *loggedRuntime, runtime model.Runtime, reply model.AutoReplyRuntime, requester model.AutoReplyInfoRequester) model.Runtime {
	native := &loggedNativeInfoRuntime{loggedRuntime: base, AutoReplyRuntime: reply, requester: requester}
	detail, hasDetail := runtime.(model.DetailBrowser)
	identity, hasIdentity := runtime.(model.AutoReplyIdentityVerifier)
	inspector, hasInspector := runtime.(model.AutoReplyContactInspector)
	switch {
	case hasDetail && hasIdentity && hasInspector:
		return struct {
			*loggedNativeInfoRuntime
			model.DetailBrowser
			model.AutoReplyIdentityVerifier
			model.AutoReplyContactInspector
		}{native, detail, identity, inspector}
	case hasDetail && hasIdentity:
		return struct {
			*loggedNativeInfoRuntime
			model.DetailBrowser
			model.AutoReplyIdentityVerifier
		}{native, detail, identity}
	case hasDetail && hasInspector:
		return struct {
			*loggedNativeInfoRuntime
			model.DetailBrowser
			model.AutoReplyContactInspector
		}{native, detail, inspector}
	case hasIdentity && hasInspector:
		return struct {
			*loggedNativeInfoRuntime
			model.AutoReplyIdentityVerifier
			model.AutoReplyContactInspector
		}{native, identity, inspector}
	case hasDetail:
		return struct {
			*loggedNativeInfoRuntime
			model.DetailBrowser
		}{native, detail}
	case hasIdentity:
		return struct {
			*loggedNativeInfoRuntime
			model.AutoReplyIdentityVerifier
		}{native, identity}
	case hasInspector:
		return struct {
			*loggedNativeInfoRuntime
			model.AutoReplyContactInspector
		}{native, inspector}
	default:
		return native
	}
}
