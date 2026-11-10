// Package failurediag 保存岗位故障发生时已知的环节、时限和影响，供本地及服务器日志共用。
package failurediag

import "strings"

// Detail 只接受调用处已核实的阶段和数值，不填请求地址、正文、候选人信息或密钥。
type Detail struct {
	Source         string `json:"source"`
	Operation      string `json:"operation,omitempty"`
	Phase          string `json:"phase,omitempty"`
	Reason         string `json:"reason"`
	Impact         string `json:"impact,omitempty"`
	ElapsedMS      int64  `json:"elapsed_ms,omitempty"`
	LimitMS        int64  `json:"limit_ms,omitempty"`
	Attempt        int64  `json:"attempt,omitempty"`
	ReceivedBytes  int64  `json:"received_bytes,omitempty"`
	HTTPStatus     int    `json:"http_status,omitempty"`
	HeadersMS      int64  `json:"headers_ms,omitempty"`
	FirstContentMS int64  `json:"first_content_ms,omitempty"`
}

// failure 只扩展日志证据，沿用原错误文案和匹配规则。
type failure struct {
	cause  error
	detail Detail
}

// Error 返回原错误文本，使重试与连续错误统计保持原有语义。
func (f *failure) Error() string { return f.cause.Error() }

// Unwrap 返回原错误，供 errors.Is、errors.As 和外层诊断使用。
func (f *failure) Unwrap() error { return f.cause }

// Wrap 把一项已观测的诊断绑定到非空错误上，原调用方仍能识别底层错误。
func Wrap(err error, detail Detail) error {
	if err == nil || detail.Source == "" || detail.Reason == "" {
		return err
	}
	return &failure{cause: err, detail: detail}
}

// Details 按从外到内的顺序提取任务和子步骤证据，支持多重 %w 错误并去重。
func Details(err error) []Detail {
	var result []Detail
	var visit func(error, int)
	visit = func(current error, depth int) {
		if current == nil || depth > 32 || len(result) >= 8 {
			return
		}
		if wrapped, ok := current.(*failure); ok {
			seen := false
			for _, old := range result {
				if old == wrapped.detail {
					seen = true
					break
				}
			}
			if !seen {
				result = append(result, wrapped.detail)
			}
		}
		switch value := current.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range value.Unwrap() {
				visit(child, depth+1)
			}
		case interface{ Unwrap() error }:
			visit(value.Unwrap(), depth+1)
		}
	}
	visit(err, 0)
	return result
}

// Summary 拼接已确认的各层原因，可用于只接受文字的审计和状态栏。
func Summary(details []Detail) string {
	parts := make([]string, 0, len(details))
	for _, d := range details {
		if d.Source == "" || d.Reason == "" {
			continue
		}
		where := d.Source
		for _, label := range []string{d.Operation, d.Phase} {
			if label != "" && !strings.Contains(where, label) {
				where += " · " + label
			}
		}
		parts = append(parts, where+"："+d.Reason)
	}
	return strings.Join(parts, "；")
}
