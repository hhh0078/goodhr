// 本文件负责接收浏览器登录凭证，并由本地程序完成稳定设备绑定。
package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/version"
)

type agentBindRequest struct {
	Token string `json:"token"`
}

// handleAgentBind 读取本机设备编号并使用当前浏览器 Token 请求云端绑定。
func (s *Server) handleAgentBind(w http.ResponseWriter, r *http.Request) {
	var request agentBindRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err)
		return
	}
	if strings.TrimSpace(request.Token) == "" {
		writeError(w, http.StatusBadRequest, "TOKEN_REQUIRED", fmt.Errorf("登录凭证不能为空，请重新登录"))
		return
	}
	if s.cloud == nil || s.deviceID == nil {
		writeError(w, http.StatusServiceUnavailable, "DEVICE_BINDING_UNAVAILABLE", fmt.Errorf("设备绑定能力还没准备好，请重启本地程序后再试"))
		return
	}
	machineID, err := s.deviceID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DEVICE_ID_UNAVAILABLE", fmt.Errorf("这台电脑的设备编号暂时没读出来：%w", err))
		return
	}
	binding, err := s.cloud.BindAgent(r.Context(), request.Token, cloud.AgentBindRequest{
		MachineID: machineID, AgentVersion: version.Value, LocalPort: s.cfg.Port,
	})
	if err != nil {
		status, code := http.StatusBadGateway, "DEVICE_BIND_FAILED"
		var apiErr *cloud.APIError
		if errors.As(err, &apiErr) {
			status = apiErr.StatusCode
			if strings.TrimSpace(apiErr.Code) != "" {
				code = apiErr.Code
			}
		}
		writeError(w, status, code, err)
		return
	}
	writeSuccess(w, http.StatusOK, binding)
}
