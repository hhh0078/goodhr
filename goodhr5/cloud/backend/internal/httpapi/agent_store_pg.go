// 本文件负责提供 Agent 机器绑定的 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PostgresAgentStore 使用 PostgreSQL 持久化机器绑定记录。
type PostgresAgentStore struct {
	db *sql.DB
}

// NewPostgresAgentStore 创建 PostgreSQL Agent 机器绑定存储。
func NewPostgresAgentStore(db *sql.DB) *PostgresAgentStore {
	return &PostgresAgentStore{db: db}
}

// SaveBinding 保存或更新 PostgreSQL 中当前用户和机器的绑定关系。
func (s *PostgresAgentStore) SaveBinding(binding AgentBinding) (AgentBinding, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, binding.UserEmail)
	if err != nil {
		return AgentBinding{}, err
	}

	status := binding.BindStatus
	if status == "" {
		status = "active"
	}

	var activeMachineID string
	err = s.db.QueryRowContext(
		ctx,
		`
		SELECT machine_id
		FROM local_agents
		WHERE user_id = $1 AND bind_status = 'active' AND machine_id <> $2
		ORDER BY last_seen_at DESC NULLS LAST, created_at DESC
		LIMIT 1
		`,
		userID,
		binding.MachineID,
	).Scan(&activeMachineID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AgentBinding{}, err
	}
	if activeMachineID != "" && activeMachineID != binding.MachineID {
		return AgentBinding{}, ErrAgentAlreadyBound
	}

	var saved AgentBinding
	saved.UserEmail = binding.UserEmail
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO local_agents (user_id, machine_id, agent_version, public_key, bind_status, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (user_id, machine_id)
		DO UPDATE SET
			agent_version = EXCLUDED.agent_version,
			public_key = EXCLUDED.public_key,
			bind_status = EXCLUDED.bind_status,
			last_seen_at = now()
		RETURNING machine_id, agent_version, public_key, bind_status, last_seen_at, created_at
		`,
		userID,
		binding.MachineID,
		binding.AgentVersion,
		binding.PublicKey,
		status,
	).Scan(
		&saved.MachineID,
		&saved.AgentVersion,
		&saved.PublicKey,
		&saved.BindStatus,
		&saved.LastSeenAt,
		&saved.CreatedAt,
	)
	if err != nil {
		return AgentBinding{}, err
	}
	saved.LocalPort = binding.LocalPort
	return saved, nil
}

// CurrentBinding 读取 PostgreSQL 中当前用户最近活跃的一台本地机器。
func (s *PostgresAgentStore) CurrentBinding(userEmail string) (AgentBinding, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var binding AgentBinding
	binding.UserEmail = userEmail
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT la.machine_id, la.agent_version, la.public_key, la.bind_status, la.last_seen_at, la.created_at
		FROM local_agents la
		INNER JOIN users u ON u.id = la.user_id
		WHERE u.email = $1 AND la.bind_status = 'active'
		ORDER BY la.last_seen_at DESC NULLS LAST, la.created_at DESC
		LIMIT 1
		`,
		userEmail,
	).Scan(
		&binding.MachineID,
		&binding.AgentVersion,
		&binding.PublicKey,
		&binding.BindStatus,
		&binding.LastSeenAt,
		&binding.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentBinding{}, ErrNotFound
	}
	if err != nil {
		return AgentBinding{}, err
	}
	return binding, nil
}

// DisableBindings 解除 PostgreSQL 中指定用户的全部本地机器绑定。
func (s *PostgresAgentStore) DisableBindings(userEmail string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(
		ctx,
		`
		UPDATE local_agents la
		SET bind_status = 'disabled', last_seen_at = now()
		FROM users u
		WHERE la.user_id = u.id AND u.email = $1 AND la.bind_status = 'active'
		`,
		userEmail,
	)
	return err
}
