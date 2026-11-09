// Package lifecycle 文件作用：将浏览器回执和岗位各环节的故障整理到同一条日志。
package lifecycle

import (
	"context"
	"errors"

	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// taskFailureDetails 汇总原错误链与浏览器证据，只有没有来源证据时才写明“未知”。
func taskFailureDetails(err error, browser *browserDiagnostic) []failurediag.Detail {
	details := failurediag.Details(err)
	if browser != nil && browser.Request != nil {
		request := browser.Request
		d := failurediag.Detail{Source: "浏览器", ElapsedMS: request.ElapsedMS, HTTPStatus: request.HTTPStatus}
		if browser.List != nil {
			d.Operation = browser.List.Stage
		}
		switch request.Phase {
		case "before_request":
			d.Phase = "发出请求之前"
		case "connect_worker":
			d.Phase = "连接浏览器执行程序"
		case "send_request":
			d.Phase = "发送浏览器请求"
		case "wait_response_headers":
			d.Phase = "等待浏览器执行程序响应"
		case "read_response_body":
			d.Phase = "读取浏览器操作结果"
		case "worker_response":
			d.Phase = "浏览器执行操作"
		}
		// 传输中断位置与执行端当时的动作是两项不同证据。
		switch request.WorkerPhase {
		case "locate_read_target":
			d.Operation = "定位要读取的页面元素"
		case "read_value":
			d.Operation = "读取已定位元素的内容"
		}
		switch request.Reason {
		case "worker_deadline":
			d.Reason, d.LimitMS = "浏览器执行时限已到", request.WorkerTimeoutMS
		case "request_deadline":
			d.Reason, d.LimitMS = "本地等待浏览器回执超时", request.GoTimeoutMS
		case "parent_deadline":
			d.Reason = "上层步骤时限已到，浏览器请求随之结束"
		case "parent_cancelled", "request_cancelled":
			d.Reason = "浏览器请求收到取消信号"
		case "client_disconnected":
			d.Reason = "浏览器执行程序观察到请求连接断开"
		case "connection_refused":
			d.Reason = "浏览器执行程序拒绝连接"
		case "connection_reset", "connection_closed":
			d.Reason = "与浏览器执行程序的连接中断"
		case "read_value_timeout":
			d.Reason, d.LimitMS = "读取已定位元素的内容超时", request.ReadValueTimeoutMS
		case "native_input_deadline":
			d.Reason = "浏览器原生输入等待超时"
		case "queue_blocked_by_prior_request":
			d.Reason = "先前的浏览器操作阻塞了当前请求"
		case "invalid_response":
			d.Reason = "浏览器回执格式异常"
		case "worker_error":
			switch {
			case request.ErrorCode == "ELEMENT_NOT_FOUND":
				d.Reason = "未能定位符合条件的页面元素"
			case request.WorkerPhase == "read_value":
				d.Reason = "已定位页面元素，但未能取回内容"
			case request.WorkerPhase == "locate_read_target":
				d.Reason = "定位页面元素时执行失败"
			}
		}
		if d.Reason != "" {
			details = append(details, d)
		}
	}
	if len(details) == 0 {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return []failurediag.Detail{{Source: "任务步骤", Reason: "时限已到，当前记录无法确认由哪一层设置"}}
		case errors.Is(err, context.Canceled):
			return []failurediag.Detail{{Source: "任务步骤", Reason: "收到取消信号，发起方未知"}}
		}
	}
	return details
}
