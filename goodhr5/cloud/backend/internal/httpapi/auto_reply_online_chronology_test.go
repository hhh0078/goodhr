// 本文件通过真实在线资料HTTP入口和独立PostgreSQL验证严格最早年月影响匹配结果，所有数据均为合成测试数据。
package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// onlineChronologyHTTPFixture 复用真实登录、会员、设备及岗位权限构造在线匹配入口，不绕过API到存储层的证据校验。
func onlineChronologyHTTPFixture(t *testing.T, f editableFactFixture, store *PostgresCandidateStore, in onlineProfileStoreInput) *AutoReplyService {
	t.Helper()
	auth := NewMemoryAuthStore()
	if err := auth.SaveSession("chronology-session", Session{Email: in.Scope.UserEmail}, time.Hour); err != nil {
		t.Fatal(err)
	}
	tenants := NewMemoryTenantStore()
	tenants.tenants[f.tenant] = Tenant{ID: f.tenant, OwnerEmail: in.Scope.UserEmail}
	tenants.members[f.tenant] = []TenantMember{{Email: in.Scope.UserEmail, Role: "admin", Status: "active"}}
	positions := NewMemoryPositionStore()
	positions.positions[f.pos] = Position{ID: f.pos, UserEmail: in.Scope.UserEmail, PlatformID: "boss", CommonConfig: map[string]any{"output_structured_resume": true}}
	agents := NewMemoryAgentStore()
	if _, err := agents.SaveBinding(AgentBinding{UserEmail: in.Scope.UserEmail, MachineID: in.MachineID}); err != nil {
		t.Fatal(err)
	}
	subscriptions := NewMemorySubscriptionStore()
	subscriptions.items[in.Scope.UserEmail] = Subscription{MemberType: memberTypeMax, ExpiresAt: time.Now().Add(time.Hour)}
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", "chronology-service")
	return NewAutoReplyService(&AuthService{store: auth, superAdmins: map[string]struct{}{in.Scope.UserEmail: {}}}, f.store, tenants, positions, nil, store, subscriptions, NewMemorySystemConfigStore(), agents, nil)
}

// TestOnlineProfileHTTPStrictCanonicalChronology 验证同年异月可正确唯一关联，主档起点缺失或冲突时不以第二条或旧来源快照冒充最早经历。
func TestOnlineProfileHTTPStrictCanonicalChronology(t *testing.T) {
	for _, kind := range []string{"company", "school"} {
		for _, scenario := range []string{"unordered", "same-year-months", "missing-start", "earliest-empty-name", "unknown-month-conflict", "same-month-conflict", "snapshot-bypass"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				f, store, in := onlineProfileFixture(t)
				in.MachineID = stableAgentMachineIDPrefix + "chronology"
				if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{machine_id}',to_jsonb($1::text)) WHERE kind='runtime_commands' AND id='online-read'`, in.MachineID); err != nil {
					t.Fatal(err)
				}
				name, year := in.Request.Profile.EarliestCompanyName, *in.Request.Profile.EarliestCompanyYear
				if kind == "school" {
					name, year = in.Request.Profile.EarliestSchoolName, *in.Request.Profile.EarliestSchoolYear
				}
				period := fmt.Sprintf("%d-02", year)
				records := [][2]string{{name, period}, {"更晚机构", fmt.Sprintf("%d-01", year+2)}}
				switch scenario {
				case "unordered":
					records[0], records[1] = records[1], records[0]
				case "same-year-months":
					records = [][2]string{{"同年更晚机构", fmt.Sprintf("%d-11", year)}, {name, period}}
				case "missing-start", "snapshot-bypass":
					records[1][1] = ""
				case "earliest-empty-name":
					records[1] = [2]string{"", fmt.Sprintf("%d-01", year-2)}
				case "unknown-month-conflict":
					records[0][1] = fmt.Sprint(year)
					records[1][1] = fmt.Sprintf("%d-01", year)
				case "same-month-conflict":
					records[1][1] = period
				}
				canonicalProfile := in.Request.Profile
				if kind == "company" {
					canonicalProfile.WorkExperiences = nil
					for _, record := range records {
						canonicalProfile.WorkExperiences = append(canonicalProfile.WorkExperiences, CandidateWorkExperience{CompanyName: record[0], StartYM: record[1]})
					}
				} else {
					canonicalProfile.Educations = nil
					for _, record := range records {
						canonicalProfile.Educations = append(canonicalProfile.Educations, CandidateEducation{SchoolName: record[0], StartYM: record[1]})
					}
				}
				old := createOnlineCanonical(t, f, canonicalProfile, "13800001111", in.Scope.UserEmail)
				if scenario == "snapshot-bypass" {
					snapshot, _ := json.Marshal(onlineProfileReceipt{Profile: in.Request.Profile, ProfileKey: onlineProfileFingerprint(in.Request.Profile)})
					if _, err := f.db.Exec(`UPDATE candidate_profile_sources SET online_profile=$2::jsonb WHERE candidate_id=$1`, old.ID, string(snapshot)); err != nil {
						t.Fatal(err)
					}
				}
				service := onlineChronologyHTTPFixture(t, f, store, in)
				raw, _ := json.Marshal(in.Request)
				request := httptest.NewRequest(http.MethodPost, "/api/auto-reply/agent/online-profile", bytes.NewReader(raw))
				request.Header.Set("Authorization", "Bearer chronology-session")
				request.Header.Set("X-GoodHR-Machine-ID", in.MachineID)
				request.Header.Set("X-GoodHR-Runtime-Service", "chronology-service")
				response := httptest.NewRecorder()
				service.Agent(response, request)
				var result autoReplyOnlineProfileResult
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.CandidateID == "" {
					t.Fatalf("在线API没有保存可核实的新资料：%d %s", response.Code, response.Body.String())
				}
				shouldMatch := scenario == "unordered" || scenario == "same-year-months"
				if (result.CandidateID == old.ID) != shouldMatch {
					t.Fatalf("%s错误推断主档最早%s：old=%s result=%+v", scenario, kind, old.ID, result)
				}
			})
		}
	}
}
