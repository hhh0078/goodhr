// 本文件记录每次 AI 上游响应的安全时间线，并在 SSE 完成标记处结束读取，不记录模型正文。
package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

// aiResponseTimeline 只保留阶段时刻、字节数和结束方式，错误正文沿用原有脱敏详情。
type aiResponseTimeline struct {
	RequestWrittenAt string `json:"request_written_at,omitempty"`
	FirstResponseAt  string `json:"first_response_at,omitempty"`
	HeadersAt        string `json:"headers_at,omitempty"`
	FirstDataAt      string `json:"first_data_at,omitempty"`
	LastDataAt       string `json:"last_data_at,omitempty"`
	FinishReasonAt   string `json:"finish_reason_at,omitempty"`
	DoneAt           string `json:"done_at,omitempty"`
	EOFAt            string `json:"eof_at,omitempty"`
	ErrorAt          string `json:"error_at,omitempty"`
	EndAt            string `json:"end_at,omitempty"`
	EndReason        string `json:"end_reason,omitempty"`
	ReceivedBytes    int64  `json:"received_bytes"`
}

// aiTimedReader 记录真实网络读取时刻，互斥保护 HTTP 传输回调与正文读取。
type aiTimedReader struct {
	source   io.Reader
	mu       sync.Mutex
	timeline aiResponseTimeline
}

// stamp 以 UTC 纳秒格式写入阶段时刻。
func (r *aiTimedReader) stamp(field *string) { *field = time.Now().UTC().Format(time.RFC3339Nano) }

// Read 在收到非空网络数据时更新首尾时间；EOF 与读错误分别记录。
func (r *aiTimedReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	if n > 0 {
		if r.timeline.FirstDataAt == "" {
			r.stamp(&r.timeline.FirstDataAt)
		}
		r.stamp(&r.timeline.LastDataAt)
		r.timeline.ReceivedBytes += int64(n)
	}
	if err == io.EOF {
		r.stamp(&r.timeline.EOFAt)
	} else if err != nil {
		r.stamp(&r.timeline.ErrorAt)
	}
	return n, err
}

// trace 记录请求写完及首响应字节；标准组合机制保留既有诊断追踪。
func (r *aiTimedReader) trace(req *http.Request) *http.Request {
	trace := &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			r.mu.Lock()
			defer r.mu.Unlock()
			if info.Err == nil {
				r.stamp(&r.timeline.RequestWrittenAt)
			} else {
				r.stamp(&r.timeline.ErrorAt)
			}
		},
		GotFirstResponseByte: func() { r.mu.Lock(); defer r.mu.Unlock(); r.stamp(&r.timeline.FirstResponseAt) },
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}

// readAIResponse 保持正文上限；仅 SSE 的 DONE 结束网络读取，finish_reason 后仍继续接收 usage 和结构化结果。
func readAIResponse(r *aiTimedReader, stream bool) ([]byte, error) {
	limited := io.LimitReader(r, maxAICompatibleBodyBytes+1)
	if !stream {
		return io.ReadAll(limited)
	}
	reader := bufio.NewReader(limited)
	var body bytes.Buffer
	for {
		line, err := reader.ReadBytes('\n')
		body.Write(line)
		data := bytes.TrimSpace(line)
		if bytes.HasPrefix(data, []byte("data:")) {
			value := bytes.TrimSpace(data[5:])
			r.mu.Lock()
			if bytes.Equal(value, []byte("[DONE]")) {
				r.stamp(&r.timeline.DoneAt)
				r.mu.Unlock()
				// 补齐结束事件的空行，保留旧客户端的标准 SSE 帧格式；不再向网络等待。
				if bytes.HasSuffix(line, []byte("\r\n")) {
					body.WriteString("\r\n")
				} else if bytes.HasSuffix(line, []byte("\n")) {
					body.WriteByte('\n')
				}
				return body.Bytes(), nil
			}
			var event struct {
				Error   json.RawMessage `json:"error"`
				Choices []struct {
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			if json.Unmarshal(value, &event) == nil {
				if len(event.Error) > 0 && !bytes.Equal(event.Error, []byte("null")) && r.timeline.ErrorAt == "" {
					r.stamp(&r.timeline.ErrorAt)
				}
				for _, choice := range event.Choices {
					if choice.FinishReason != nil && *choice.FinishReason != "" && r.timeline.FinishReasonAt == "" {
						r.stamp(&r.timeline.FinishReasonAt)
					}
				}
			}
			r.mu.Unlock()
		}
		if err == io.EOF {
			return body.Bytes(), nil
		}
		if err != nil {
			return body.Bytes(), err
		}
	}
}
