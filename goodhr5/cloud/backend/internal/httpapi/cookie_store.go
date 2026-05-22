// cookie 加密存储
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type CookieRecord struct {
	ID, TenantID, UserID, PlatformID, DisplayName, CookieType, Status, FileName string
	EncryptedData                                                               []byte
	EncryptedKeys                                                               map[string]string
	UsedByTaskID                                                                sql.NullString
	SizeBytes                                                                   int64
	CreatedAt, UpdatedAt                                                        time.Time
}

type CookieStore interface {
	Create(rec CookieRecord) (CookieRecord, error)
	Update(rec CookieRecord) (CookieRecord, error)
	List(tenantID string) ([]CookieRecord, error)
	GetByID(tenantID, cookieID string) (CookieRecord, error)
	UpdateStatus(tenantID, cookieID, status, taskID string) error
	AddEncryptedKey(tenantID, cookieID, agentID, encKey string) error
	Delete(tenantID, cookieID string) error
}

var ErrCookieNotFound = errors.New("cookie not found")

// ---------- Memory ----------
type MemoryCookieStore struct {
	mu     sync.Mutex
	items  map[string]CookieRecord
	now    func() time.Time
	nextID func() string
}

func NewMemoryCookieStore() *MemoryCookieStore {
	seq := 0
	return &MemoryCookieStore{items: map[string]CookieRecord{}, now: time.Now, nextID: func() string { seq++; return fmt.Sprintf("cookie_%d", seq) }}
}
func (s *MemoryCookieStore) Create(rec CookieRecord) (CookieRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec.ID = s.nextID()
	rec.CreatedAt = s.now()
	rec.UpdatedAt = s.now()
	if rec.EncryptedKeys == nil {
		rec.EncryptedKeys = map[string]string{}
	}
	if rec.Status == "" {
		rec.Status = "available"
	}
	s.items[rec.ID] = rec
	return rec, nil
}
func (s *MemoryCookieStore) Update(rec CookieRecord) (CookieRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.items[rec.ID]
	if !ok || current.TenantID != rec.TenantID {
		return CookieRecord{}, ErrCookieNotFound
	}
	current.UserID = rec.UserID
	current.PlatformID = rec.PlatformID
	current.DisplayName = rec.DisplayName
	current.CookieType = rec.CookieType
	current.EncryptedData = rec.EncryptedData
	current.EncryptedKeys = rec.EncryptedKeys
	current.Status = rec.Status
	current.FileName = rec.FileName
	current.SizeBytes = rec.SizeBytes
	current.UpdatedAt = s.now()
	s.items[rec.ID] = current
	return current, nil
}
func (s *MemoryCookieStore) List(tenantID string) ([]CookieRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []CookieRecord
	for _, r := range s.items {
		if r.TenantID == tenantID {
			result = append(result, r)
		}
	}
	if result == nil {
		result = []CookieRecord{}
	}
	return result, nil
}
func (s *MemoryCookieStore) GetByID(tenantID, cookieID string) (CookieRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[cookieID]
	if !ok || r.TenantID != tenantID {
		return CookieRecord{}, ErrCookieNotFound
	}
	return r, nil
}
func (s *MemoryCookieStore) UpdateStatus(tenantID, cookieID, status, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[cookieID]
	if !ok || r.TenantID != tenantID {
		return ErrCookieNotFound
	}
	r.Status = status
	if taskID == "" {
		r.UsedByTaskID = sql.NullString{}
	} else {
		r.UsedByTaskID = sql.NullString{String: taskID, Valid: true}
	}
	r.UpdatedAt = s.now()
	s.items[cookieID] = r
	return nil
}
func (s *MemoryCookieStore) AddEncryptedKey(tenantID, cookieID, agentID, encKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[cookieID]
	if !ok || r.TenantID != tenantID {
		return ErrCookieNotFound
	}
	r.EncryptedKeys[agentID] = encKey
	s.items[cookieID] = r
	return nil
}
func (s *MemoryCookieStore) Delete(tenantID, cookieID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[cookieID]
	if !ok || r.TenantID != tenantID {
		return ErrCookieNotFound
	}
	delete(s.items, cookieID)
	_ = r
	return nil
}

// ---------- PostgreSQL ----------
type PostgresCookieStore struct{ db *sql.DB }

func NewPostgresCookieStore(db *sql.DB) *PostgresCookieStore { return &PostgresCookieStore{db: db} }
func (s *PostgresCookieStore) Create(rec CookieRecord) (CookieRecord, error) {
	if rec.EncryptedData == nil {
		rec.EncryptedData = []byte{}
	}
	if rec.EncryptedKeys == nil {
		rec.EncryptedKeys = map[string]string{}
	}
	if rec.Status == "" {
		rec.Status = "available"
	}
	userID := sql.NullString{}
	if rec.UserID != "" {
		id, err := ensureUserID(context.Background(), s.db, rec.UserID)
		if err != nil {
			return CookieRecord{}, err
		}
		userID = sql.NullString{String: id, Valid: true}
	}
	keysJSON, _ := json.Marshal(rec.EncryptedKeys)
	var id string
	err := s.db.QueryRow(`INSERT INTO cookie_data(tenant_id,user_id,platform_id,display_name,cookie_type,encrypted_data,encrypted_keys,status,file_name,size_bytes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,created_at`,
		rec.TenantID, userID, rec.PlatformID, rec.DisplayName, rec.CookieType, rec.EncryptedData, keysJSON, rec.Status, rec.FileName, rec.SizeBytes).Scan(&id, &rec.CreatedAt)
	if err != nil {
		return CookieRecord{}, err
	}
	rec.ID = id
	rec.UpdatedAt = rec.CreatedAt
	return rec, nil
}
func (s *PostgresCookieStore) Update(rec CookieRecord) (CookieRecord, error) {
	if rec.EncryptedData == nil {
		rec.EncryptedData = []byte{}
	}
	if rec.EncryptedKeys == nil {
		rec.EncryptedKeys = map[string]string{}
	}
	if rec.Status == "" {
		rec.Status = "available"
	}
	userID := sql.NullString{}
	if rec.UserID != "" {
		id, err := ensureUserID(context.Background(), s.db, rec.UserID)
		if err != nil {
			return CookieRecord{}, err
		}
		userID = sql.NullString{String: id, Valid: true}
	}
	keysJSON, _ := json.Marshal(rec.EncryptedKeys)
	var updated CookieRecord
	updated.TenantID = rec.TenantID
	err := s.db.QueryRow(`
		UPDATE cookie_data
		SET user_id=$3, platform_id=$4, display_name=$5, cookie_type=$6,
		    encrypted_data=$7, encrypted_keys=$8, status=$9, file_name=$10, size_bytes=$11, updated_at=NOW()
		WHERE tenant_id=$1 AND id=$2
		RETURNING id, tenant_id, COALESCE(user_id::text,''), platform_id, display_name, cookie_type, status,
		          COALESCE(used_by_task_id::text,''), file_name, size_bytes, encrypted_data, encrypted_keys, created_at, updated_at
	`,
		rec.TenantID, rec.ID, userID, rec.PlatformID, rec.DisplayName, rec.CookieType,
		rec.EncryptedData, keysJSON, rec.Status, rec.FileName, rec.SizeBytes,
	).Scan(
		&updated.ID, &updated.TenantID, &updated.UserID, &updated.PlatformID, &updated.DisplayName, &updated.CookieType, &updated.Status,
		&updated.UsedByTaskID, &updated.FileName, &updated.SizeBytes, &updated.EncryptedData, &keysJSON, &updated.CreatedAt, &updated.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CookieRecord{}, ErrCookieNotFound
	}
	if err != nil {
		return CookieRecord{}, err
	}
	if err := json.Unmarshal(keysJSON, &updated.EncryptedKeys); err != nil {
		return CookieRecord{}, err
	}
	if updated.EncryptedKeys == nil {
		updated.EncryptedKeys = map[string]string{}
	}
	return updated, nil
}
func (s *PostgresCookieStore) List(tenantID string) ([]CookieRecord, error) {
	rows, err := s.db.Query(`SELECT id,tenant_id,COALESCE(user_id::text,''),platform_id,display_name,cookie_type,status,COALESCE(used_by_task_id::text,''),file_name,size_bytes,created_at,updated_at FROM cookie_data WHERE tenant_id=$1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []CookieRecord
	for rows.Next() {
		var r CookieRecord
		var uid, tid sql.NullString
		rows.Scan(&r.ID, &r.TenantID, &uid, &r.PlatformID, &r.DisplayName, &r.CookieType, &r.Status, &tid, &r.FileName, &r.SizeBytes, &r.CreatedAt, &r.UpdatedAt)
		if uid.Valid {
			r.UserID = uid.String
		}
		if tid.Valid {
			r.UsedByTaskID = tid
		}
		result = append(result, r)
	}
	if result == nil {
		result = []CookieRecord{}
	}
	return result, rows.Err()
}
func (s *PostgresCookieStore) GetByID(tenantID, cookieID string) (CookieRecord, error) {
	var r CookieRecord
	var uid, tid sql.NullString
	var keysJSON []byte
	err := s.db.QueryRow(`
		SELECT id,tenant_id,COALESCE(user_id::text,''),platform_id,display_name,cookie_type,status,
		       COALESCE(used_by_task_id::text,''),file_name,size_bytes,encrypted_data,encrypted_keys,created_at,updated_at
		FROM cookie_data
		WHERE tenant_id=$1 AND id=$2
	`, tenantID, cookieID).Scan(
		&r.ID, &r.TenantID, &uid, &r.PlatformID, &r.DisplayName, &r.CookieType, &r.Status,
		&tid, &r.FileName, &r.SizeBytes, &r.EncryptedData, &keysJSON, &r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CookieRecord{}, ErrCookieNotFound
	}
	if err != nil {
		return CookieRecord{}, err
	}
	if uid.Valid {
		r.UserID = uid.String
	}
	if tid.Valid {
		r.UsedByTaskID = tid
	}
	if err := json.Unmarshal(keysJSON, &r.EncryptedKeys); err != nil {
		return CookieRecord{}, err
	}
	if r.EncryptedKeys == nil {
		r.EncryptedKeys = map[string]string{}
	}
	return r, nil
}
func (s *PostgresCookieStore) UpdateStatus(tenantID, cookieID, status, taskID string) error {
	result, err := s.db.Exec(`UPDATE cookie_data SET status=$1,used_by_task_id=NULLIF($2,'')::uuid,updated_at=NOW() WHERE tenant_id=$3 AND id=$4`, status, taskID, tenantID, cookieID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err == nil && affected == 0 {
		return ErrCookieNotFound
	}
	return nil
}
func (s *PostgresCookieStore) AddEncryptedKey(tenantID, cookieID, agentID, encKey string) error {
	_, err := s.db.Exec(`UPDATE cookie_data SET encrypted_keys=COALESCE(encrypted_keys,'{}') || jsonb_build_object($1,$2),updated_at=NOW() WHERE tenant_id=$3 AND id=$4`, agentID, encKey, tenantID, cookieID)
	return err
}
func (s *PostgresCookieStore) Delete(tenantID, cookieID string) error {
	_, err := s.db.Exec(`DELETE FROM cookie_data WHERE tenant_id=$1 AND id=$2`, tenantID, cookieID)
	return err
}
