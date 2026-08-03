// Package response 负责输出统一格式的本地 API JSON 响应。
package response

import (
	"encoding/json"
	"net/http"
)

// Body 是本地 API 统一响应结构。
type Body struct {
	OK   bool   `json:"ok"`
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

// Success 返回成功 JSON。
// w 为 HTTP 响应对象，data 为业务数据。
func Success(w http.ResponseWriter, data any) {
	write(w, http.StatusOK, Body{OK: true, Code: 200, Msg: "成功", Data: data})
}

// Error 返回失败 JSON。
// w 为 HTTP 响应对象，status 为 HTTP 状态码，msg 为中文错误信息。
func Error(w http.ResponseWriter, status int, msg string) {
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if msg == "" {
		msg = "请求失败"
	}
	write(w, status, Body{OK: false, Code: status, Msg: msg})
}

// write 写入 JSON 响应。
// w 为 HTTP 响应对象，status 为 HTTP 状态码，body 为响应体。
func write(w http.ResponseWriter, status int, body Body) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
