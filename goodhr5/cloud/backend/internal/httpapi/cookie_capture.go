// cookie 登录监控
package httpapi

import ("bytes"; "encoding/base64"; "encoding/json"; "fmt"; "io"; "net/http"; "time")

type CookieCapture struct { store CookieStore; httpClient *http.Client }
func NewCookieCapture(store CookieStore) *CookieCapture { return &CookieCapture{store: store, httpClient: &http.Client{Timeout: 60*time.Second}} }

func (c *CookieCapture) Capture(cookieID, tenantID, platformID, agentBaseURL, userDataDir string, memberKeys map[string]string) {
	go c.loop(cookieID, tenantID, platformID, agentBaseURL, userDataDir, memberKeys)
}

func (c *CookieCapture) loop(cookieID, tenantID, platformID, agentBaseURL, userDataDir string, memberKeys map[string]string) {
	c.post(agentBaseURL+"/api/v1/browser/start", map[string]any{"persistent":true,"user_data_dir":userDataDir})
	defer c.post(agentBaseURL+"/api/v1/browser/stop", nil)

	url := platformEntry(platformID)
	c.post(agentBaseURL+"/api/v1/page/open", map[string]any{"url":url})

	for i:=0; i<120; i++ { time.Sleep(2*time.Second); if c.isLoggedIn(agentBaseURL, platformID) { break } }

	resp, _ := c.postJSON(agentBaseURL+"/api/v1/page/export-profile", nil)
	raw, _ := base64.StdEncoding.DecodeString(resp["data"].(string))
	sk, _ := GenerateSK(); encData, _ := EncryptData(raw, sk)

	var encKeys json.RawMessage; encKeys, _ = json.Marshal(memberKeys)
	_ = encData; _ = encKeys; _ = sk
	_ = c.store.UpdateStatus(tenantID, cookieID, "available", "")
}

func (c *CookieCapture) post(url string, body any) error { _,err:=c.postJSON(url,body); return err }
func (c *CookieCapture) postJSON(url string, body any) (map[string]any, error) {
	var reqBody io.Reader
	if body != nil { data,_:=json.Marshal(body); reqBody=bytes.NewReader(data) }
	resp, err := c.httpClient.Post(url, "application/json", reqBody)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	b,_:=io.ReadAll(resp.Body); var m map[string]any; json.Unmarshal(b, &m); return m, nil
}
func (c *CookieCapture) getURL(agentBaseURL string) string { m,_:=c.postJSON(agentBaseURL+"/api/v1/page/url",nil); if m!=nil {return fmt.Sprint(m["url"])}; return "" }
func (c *CookieCapture) isLoggedIn(agentBaseURL, platformID string) bool {
	url := c.getURL(agentBaseURL)
	switch platformID { case "boss": return len(url)>0 && (url[:40]=="https://www.zhipin.com/web/chat/recommend"); case "zhaopin": return len(url)>0 && (url[:40]=="https://rd6.zhaopin.com/app/recommend") }
	return false
}
func platformEntry(platformID string) string { switch platformID { case "boss": return "https://www.zhipin.com/web/chat/recommend"; case "zhaopin": return "https://rd6.zhaopin.com/app/recommend" }; return "" }
