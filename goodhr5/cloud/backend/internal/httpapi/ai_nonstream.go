// 本文件让内置聊天上游统一使用非流式请求，并在完整结果返回后兼容旧客户端的 SSE 协议。
package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// disableAIUpstreamStream 只关闭上游流式并移除流专用参数，保留消息、图片、模型和思考设置。
func disableAIUpstreamStream(body []byte) []byte {
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil || payload == nil {
		return body
	}
	payload["stream"] = json.RawMessage("false")
	delete(payload, "stream_options")
	result, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return result
}

// completedAIJSONStream 将已校验的完整结果一次性包装成 SSE，不重新请求模型或修改用量。
func completedAIJSONStream(body []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	// 上游不能借结果包装注入 GoodHR 自有账单或故障信号。
	for key := range payload {
		if strings.HasPrefix(strings.ToLower(key), "goodhr_") {
			delete(payload, key)
		}
	}
	var choices []map[string]json.RawMessage
	if err := json.Unmarshal(payload["choices"], &choices); err != nil {
		return nil, err
	}
	for _, choice := range choices {
		var message map[string]json.RawMessage
		if err := json.Unmarshal(choice["message"], &message); err != nil {
			return nil, err
		}
		if message == nil {
			return nil, fmt.Errorf("missing completion message")
		}
		if calls, ok := message["tool_calls"]; ok {
			var tools []map[string]json.RawMessage
			if err := json.Unmarshal(calls, &tools); err != nil {
				return nil, err
			}
			for index, tool := range tools {
				if tool == nil {
					return nil, fmt.Errorf("invalid tool call")
				}
				tool["index"], _ = json.Marshal(index)
			}
			message["tool_calls"], _ = json.Marshal(tools)
		}
		choice["delta"], _ = json.Marshal(message)
		delete(choice, "message")
	}
	payload["choices"], _ = json.Marshal(choices)
	payload["object"] = json.RawMessage(`"chat.completion.chunk"`)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("data: ")
	out.Write(encoded)
	out.WriteString("\n\ndata: [DONE]\n\n")
	return out.Bytes(), nil
}
