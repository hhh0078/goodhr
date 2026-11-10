// Package lifecycle 文件作用：把同一次列表故障的流程进度和浏览器请求证据写到岗位日志里。
package lifecycle

import (
	"encoding/json"
	"errors"
	"runtime"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/version"
)

// browserDiagnostic 只包含允许分享的运行信息，缺少的一端保持空白。
type browserDiagnostic struct {
	List         *model.CandidateListDiagnostic `json:"list,omitempty"`
	Request      *contract.RequestDiagnostic    `json:"request,omitempty"`
	AgentVersion string                         `json:"agent_version"`
	OS           string                         `json:"os"`
	Arch         string                         `json:"arch"`
}

// browserDiagnosticFromError 读取同一条错误链上的证据，避免并发任务之间串用浏览器状态。
func browserDiagnosticFromError(err error) *browserDiagnostic {
	if err == nil {
		return nil
	}
	var result browserDiagnostic
	var listError interface {
		CandidateListDiagnostic() model.CandidateListDiagnostic
	}
	if errors.As(err, &listError) {
		list := listError.CandidateListDiagnostic()
		result.List = &list
	}
	var workerError *contract.WorkerError
	if errors.As(err, &workerError) && workerError != nil {
		var details struct {
			Request *contract.RequestDiagnostic `json:"request_diagnostic"`
		}
		if json.Unmarshal(workerError.Body.Details, &details) == nil {
			result.Request = details.Request
		}
	}
	var transportError interface {
		BrowserRequestDiagnostic() contract.RequestDiagnostic
	}
	if errors.As(err, &transportError) {
		request := transportError.BrowserRequestDiagnostic()
		result.Request = &request
	}
	if result.List == nil && result.Request == nil {
		return nil
	}
	result.AgentVersion = version.Value
	result.OS, result.Arch = runtime.GOOS, runtime.GOARCH
	return &result
}
