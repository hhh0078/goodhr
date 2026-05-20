// cookie 登录监控
package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type CookieCapture struct {
	store      CookieStore
	httpClient *http.Client
}

func NewCookieCapture(store CookieStore) *CookieCapture {
	return &CookieCapture{store: store, httpClient: &http.Client{Timeout: 60 * time.Second}}
}

func (c *CookieCapture) Capture(cookieID, tenantID, platformID, agentBaseURL, userDataDir string, memberKeys map[string]string) {
	go c.loop(cookieID, tenantID, platformID, strings.TrimRight(agentBaseURL, "/"), userDataDir, memberKeys)
}

func (c *CookieCapture) loop(cookieID, tenantID, platformID, agentBaseURL, userDataDir string, memberKeys map[string]string) {
	log.Printf("[cookies] capture start cookie=%s platform=%s agent=%s", cookieID, platformID, agentBaseURL)

	if err := c.post(agentBaseURL+"/api/v1/browser/start", map[string]any{"persistent": true, "user_data_dir": userDataDir}); err != nil {
		log.Printf("[cookies] browser start failed cookie=%s: %v", cookieID, err)
		_ = c.store.UpdateStatus(tenantID, cookieID, "failed", "")
		return
	}

	url := platformEntry(platformID)
	if url == "" {
		log.Printf("[cookies] unsupported platform cookie=%s platform=%s", cookieID, platformID)
		_ = c.store.UpdateStatus(tenantID, cookieID, "failed", "")
		return
	}
	if err := c.post(agentBaseURL+"/api/v1/page/open", map[string]any{"url": url}); err != nil {
		log.Printf("[cookies] page open failed cookie=%s: %v", cookieID, err)
		_ = c.store.UpdateStatus(tenantID, cookieID, "failed", "")
		return
	}

	for i := 0; i < 120; i++ {
		time.Sleep(2 * time.Second)
		if c.isLoggedIn(agentBaseURL, platformID) {
			log.Printf("[cookies] login detected cookie=%s", cookieID)
			break
		}
	}

	resp, err := c.postJSON(agentBaseURL+"/api/v1/page/export-profile", nil)
	if err != nil {
		log.Printf("[cookies] export profile failed cookie=%s: %v", cookieID, err)
		_ = c.store.UpdateStatus(tenantID, cookieID, "failed", "")
		return
	}
	data, _ := resp["data"].(string)
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		log.Printf("[cookies] export profile decode failed cookie=%s: %v", cookieID, err)
		_ = c.store.UpdateStatus(tenantID, cookieID, "failed", "")
		return
	}

	sk, err := GenerateSK()
	if err != nil {
		log.Printf("[cookies] generate sk failed cookie=%s: %v", cookieID, err)
		_ = c.store.UpdateStatus(tenantID, cookieID, "failed", "")
		return
	}
	encData, err := EncryptData(raw, sk)
	if err != nil {
		log.Printf("[cookies] encrypt profile failed cookie=%s: %v", cookieID, err)
		_ = c.store.UpdateStatus(tenantID, cookieID, "failed", "")
		return
	}

	encKeys, _ := json.Marshal(memberKeys)
	_ = encData
	_ = encKeys
	_ = sk
	_ = c.store.UpdateStatus(tenantID, cookieID, "available", "")
	log.Printf("[cookies] capture done cookie=%s", cookieID)
}

func (c *CookieCapture) post(url string, body any) error {
	_, err := c.postJSON(url, body)
	return err
}

func (c *CookieCapture) postJSON(url string, body any) (map[string]any, error) {
	var reqBody io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewReader(data)
	}
	resp, err := c.httpClient.Post(url, "application/json", reqBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return m, fmt.Errorf("local agent status=%d body=%s", resp.StatusCode, string(b))
	}
	if ok, exists := m["ok"].(bool); exists && !ok {
		return m, fmt.Errorf("local agent error=%v", m["error"])
	}
	return m, nil
}

func (c *CookieCapture) getURL(agentBaseURL string) string {
	m, err := c.postJSON(agentBaseURL+"/api/v1/page/url", nil)
	if err != nil {
		return ""
	}
	return fmt.Sprint(m["url"])
}

func (c *CookieCapture) isLoggedIn(agentBaseURL, platformID string) bool {
	url := c.getURL(agentBaseURL)
	switch platformID {
	case "boss":
		return strings.HasPrefix(url, "https://www.zhipin.com/web/chat/recommend")
	case "zhaopin":
		return strings.HasPrefix(url, "https://rd6.zhaopin.com/app/recommend")
	}
	return false
}

func platformEntry(platformID string) string {
	switch platformID {
	case "boss":
		return "https://www.zhipin.com/web/chat/recommend"
	case "zhaopin":
		return "https://rd6.zhaopin.com/app/recommend"
	}
	return ""
}
