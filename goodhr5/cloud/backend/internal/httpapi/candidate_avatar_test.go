// 本文件用合成图片验证头像规范化、简历权限、空更新保护和 PostgreSQL JSONB 持久化，不读取个人图片。
package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

// candidateAvatarTestImage 在内存生成确定性的图片，覆盖高熵 PNG、JPEG 和 GIF，不下载外部数据。
func candidateAvatarTestImage(t *testing.T, format string, width, height int, noisy bool) string {
	t.Helper()
	input := image.NewNRGBA(image.Rect(0, 0, width, height))
	state := uint32(0x71629384)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixel := color.NRGBA{R: 40, G: 120, B: 80, A: 160}
			if noisy {
				state ^= state << 13
				state ^= state >> 17
				state ^= state << 5
				pixel = color.NRGBA{R: uint8(state), G: uint8(state >> 8), B: uint8(state >> 16), A: 255}
			}
			input.SetNRGBA(x, y, pixel)
		}
	}
	var body bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&body, input)
	case "jpeg":
		err = jpeg.Encode(&body, input, &jpeg.Options{Quality: 95})
	case "gif":
		err = gif.Encode(&body, input, nil)
	default:
		t.Fatal("unsupported synthetic image format")
	}
	if err != nil {
		t.Fatal(err)
	}
	return "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(body.Bytes())
}

// candidateAvatarTestPixels 解码规范化后的合成头像，用于尺寸、透明度和元信息断言。
func candidateAvatarTestPixels(t *testing.T, value string) (image.Image, []byte) {
	t.Helper()
	_, payload, _ := strings.Cut(value, ",")
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return decoded, raw
}

// TestCandidateAvatarNormalization 验证图片真实解码、等比缩小、透明度和最终数据大小，并转换静态 GIF。
func TestCandidateAvatarNormalization(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif"} {
		input := candidateAvatarTestImage(t, format, 512, 320, format == "png")
		output, err := normalizeCandidateAvatar(input)
		if err != nil || output == "" || len(output) > candidateAvatarOutputLimit {
			t.Fatalf("format=%s size=%d err=%v", format, len(output), err)
		}
		pixels, _ := candidateAvatarTestPixels(t, output)
		width, height := pixels.Bounds().Dx(), pixels.Bounds().Dy()
		if width > 256 || height > 256 || width*320 != height*512 {
			t.Fatalf("format=%s avatar was stretched: %dx%d", format, width, height)
		}
		if format == "gif" && !strings.HasPrefix(output, "data:image/png;base64,") {
			t.Fatal("GIF did not become a static PNG")
		}
		if format == "png" && (len(input) <= 20000 || len(output) <= 20000) {
			t.Fatal("high-entropy fixture does not exercise avatar data above the ordinary text limit")
		}
	}
	input := candidateAvatarTestImage(t, "png", 7, 5, false)
	output, err := normalizeCandidateAvatar(input)
	if err != nil {
		t.Fatal(err)
	}
	pixels, _ := candidateAvatarTestPixels(t, output)
	_, _, _, alpha := pixels.At(0, 0).RGBA()
	if pixels.Bounds().Dx() != 7 || pixels.Bounds().Dy() != 5 || alpha != 160*257 {
		t.Fatal("small transparent avatar was enlarged or lost transparency")
	}
}

// candidateAvatarTestPNGChunk 将合成 PNG 的头部尺寸或额外元信息修改为有效校验和，验证先检查再解码。
func candidateAvatarTestPNGChunk(t *testing.T, width, height uint32, metadata string) string {
	t.Helper()
	_, raw := candidateAvatarTestPixels(t, candidateAvatarTestImage(t, "png", 1, 1, false))
	binary.BigEndian.PutUint32(raw[16:20], width)
	binary.BigEndian.PutUint32(raw[20:24], height)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	if metadata != "" {
		chunk := append([]byte("tEXt"), []byte("private\x00"+metadata)...)
		var extra bytes.Buffer
		_ = binary.Write(&extra, binary.BigEndian, uint32(len(chunk)-4))
		extra.Write(chunk)
		_ = binary.Write(&extra, binary.BigEndian, crc32.ChecksumIEEE(chunk))
		raw = append(append(append([]byte{}, raw[:33]...), extra.Bytes()...), raw[33:]...)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
}

// TestCandidateAvatarRejectsUnsafeAndStripsMetadata 验证远程地址、伪图片、SVG、超限原图及炸图头被拒绝，元信息不入库。
func TestCandidateAvatarRejectsUnsafeAndStripsMetadata(t *testing.T) {
	valid := candidateAvatarTestImage(t, "png", 8, 8, false)
	invalid := []any{
		"https://example.test/avatar.png", "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)),
		"data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not an image")),
		strings.Replace(valid, "image/png", "image/jpeg", 1), "data:image/png;base64,%%%", 123,
		"data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, candidateAvatarInputLimit+1)),
		candidateAvatarTestPNGChunk(t, 4097, 1, ""), candidateAvatarTestPNGChunk(t, 4096, 4096, ""),
	}
	for index, value := range invalid {
		if _, err := normalizeCandidateAvatar(value); err == nil {
			t.Fatalf("unsafe avatar %d accepted", index)
		}
	}
	for _, value := range []any{nil, "", "  "} {
		output, err := normalizeCandidateAvatar(value)
		if err != nil || output != "" {
			t.Fatalf("empty avatar was rejected or used as deletion: %v", err)
		}
	}
	output, err := normalizeCandidateAvatar(candidateAvatarTestPNGChunk(t, 1, 1, "synthetic-private-metadata"))
	if err != nil {
		t.Fatal(err)
	}
	_, raw := candidateAvatarTestPixels(t, output)
	if bytes.Contains(raw, []byte("synthetic-private-metadata")) || bytes.Contains(raw, []byte("tEXt")) {
		t.Fatal("avatar metadata survived re-encoding")
	}
}

// TestCandidateAvatarHTTP 验证真实路由新增、详情和更新保留头像，跨团队及普通成员均不能取到别人头像。
func TestCandidateAvatarHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "avatar-owner@example.com"
	token := loginForTest(t, routes, email)
	avatar := candidateAvatarTestImage(t, "png", 512, 320, true)
	status, response := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "头像合成候选人", "phone": "13800000001", "avatar_data_url": avatar, "idempotency_key": "avatar-create"})
	if status != http.StatusOK {
		t.Fatalf("avatar create status=%d body=%v", status, response)
	}
	candidate := response["candidate"].(map[string]any)
	id := candidate["id"].(string)
	stored, _ := candidate["avatar_data_url"].(string)
	if stored == "" || len(stored) > candidateAvatarOutputLimit {
		t.Fatal("avatar missing from protected response")
	}
	status, response = candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates/"+id, nil)
	if status != http.StatusOK || response["candidate"].(map[string]any)["avatar_data_url"] != stored {
		t.Fatal("saved avatar was not returned by detail")
	}
	for _, fields := range []map[string]any{{"personal_description": "保留头像"}, {"avatar_data_url": ""}, {"avatar_data_url": nil}} {
		fields["row_version"] = candidate["row_version"]
		status, response = candidateTestRequest(t, routes, token, http.MethodPatch, "/api/candidates/"+id, fields)
		if status != http.StatusOK {
			t.Fatalf("avatar-preserving PATCH failed: %d %v", status, response)
		}
		candidate = response["candidate"].(map[string]any)
		if candidate["avatar_data_url"] != stored {
			t.Fatal("omitted or empty avatar cleared saved data")
		}
	}
	for _, invalid := range []string{"https://example.test/photo.jpg", "data:image/svg+xml;base64,PHN2Zy8+", "data:image/png;base64,aW52YWxpZA==", "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, candidateAvatarInputLimit+1)), candidateAvatarTestPNGChunk(t, 4096, 4096, "")} {
		status, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "不合规头像", "phone": "13900000001", "avatar_data_url": invalid})
		if status != http.StatusBadRequest {
			t.Fatalf("invalid avatar reached candidate creation: %d", status)
		}
	}
	otherEmail := "avatar-other@example.com"
	otherToken := loginForTest(t, routes, otherEmail)
	status, response = candidateTestRequest(t, routes, otherToken, http.MethodGet, "/api/candidates/"+id, nil)
	responseJSON, _ := json.Marshal(response)
	if status != http.StatusNotFound || bytes.Contains(responseJSON, []byte(stored)) {
		t.Fatal("another team obtained the avatar")
	}
	tenant, err := server.tenants.store.GetOrCreateTenant(email)
	if err != nil {
		t.Fatal(err)
	}
	invite, _, err := server.tenants.store.InviteMember(tenant.ID, otherEmail, "member", email)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.tenants.store.AcceptInvitation(invite.ID, otherEmail); err != nil {
		t.Fatal(err)
	}
	status, response = candidateTestRequest(t, routes, otherToken, http.MethodGet, "/api/candidates/"+id, nil)
	if status != http.StatusNotFound {
		t.Fatalf("ordinary member obtained another creator's avatar: %d", status)
	}
	status, response = candidateTestRequest(t, routes, otherToken, http.MethodPost, "/api/candidates/duplicates", map[string]any{"phone": "13800000001"})
	if status != http.StatusOK || response["hidden_match"] != true || len(response["matches"].([]any)) != 0 {
		t.Fatal("duplicate query leaked avatar to ordinary member")
	}
}

// TestCandidateAvatarPostgresPersistence 验证真实 PostgreSQL 保存规范化头像到现有 JSONB，并保留版本及创建人权限。
func TestCandidateAvatarPostgresPersistence(t *testing.T) {
	db := candidatePostgresTestDB(t)
	var tenantID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	scope := CandidateLibraryScope{TenantID: tenantID, UserEmail: "avatar-pg-owner@example.com"}
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES($1,$2),($3,$2)`, scope.UserEmail, tenantID, "avatar-pg-member@example.com"); err != nil {
		t.Fatal(err)
	}
	fields, err := candidateMutationFields(map[string]any{"candidate_name": "PG合成头像", "phone": "13800000001", "avatar_data_url": candidateAvatarTestImage(t, "png", 512, 320, true)}, true)
	if err != nil {
		t.Fatal(err)
	}
	store := NewPostgresCandidateStore(db)
	item, err := store.SaveLibraryCandidate(scope, "", fields, 0, "avatar-create")
	if err != nil {
		t.Fatal(err)
	}
	avatar := candidateString(fields, "avatar_data_url")
	var persisted string
	if err = db.QueryRow(`SELECT library_fields->>'avatar_data_url' FROM candidate_profiles WHERE id=$1::uuid`, item.ID).Scan(&persisted); err != nil || persisted != avatar {
		t.Fatalf("JSONB avatar persistence differs: err=%v", err)
	}
	for _, input := range []map[string]any{{"personal_description": "头像继续保留"}, {"avatar_data_url": ""}, {"avatar_data_url": nil}} {
		update, err := candidateMutationFields(input, false)
		if err != nil {
			t.Fatal(err)
		}
		item, err = store.SaveLibraryCandidate(scope, item.ID, update, item.RowVersion, "")
		if err != nil || candidateString(item.LibraryFields, "avatar_data_url") != avatar {
			t.Fatalf("JSONB PATCH cleared avatar: err=%v", err)
		}
	}
	if publicPositionCandidate(item)["avatar_data_url"] != avatar {
		t.Fatal("avatar missing from public candidate contract")
	}
	if _, err = store.GetPositionCandidate(tenantID, item.ID, "", "avatar-pg-member@example.com", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("member obtained private avatar: %v", err)
	}
}
