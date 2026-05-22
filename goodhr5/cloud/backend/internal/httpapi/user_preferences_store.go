package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

type UserPreferences struct {
	AIModel                string
	ClickFrequency         int
	DetailOpenProbability  int
	ScrollDelayMin         int
	ScrollDelayMax         int
	ListViewDelayMin       float64
	ListViewDelayMax       float64
	DetailViewDelayMin     float64
	DetailViewDelayMax     float64
	GreetDelayMin          float64
	GreetDelayMax          float64
	RestAfterCandidatesMin int
	RestAfterCandidatesMax int
	RestTimesMin           int
	RestTimesMax           int
	RestDurationMin        float64
	RestDurationMax        float64
	UpdatedAt              time.Time
}

func DefaultUserPreferences() UserPreferences {
	return UserPreferences{
		AIModel:                "",
		ClickFrequency:         80,
		DetailOpenProbability:  30,
		ScrollDelayMin:         3,
		ScrollDelayMax:         8,
		ListViewDelayMin:       1,
		ListViewDelayMax:       2,
		DetailViewDelayMin:     1,
		DetailViewDelayMax:     2,
		GreetDelayMin:          1,
		GreetDelayMax:          2,
		RestAfterCandidatesMin: 0,
		RestAfterCandidatesMax: 0,
		RestTimesMin:           0,
		RestTimesMax:           0,
		RestDurationMin:        0,
		RestDurationMax:        0,
	}
}

type UserPreferencesStore interface {
	UserPreferences(userEmail string) (UserPreferences, error)
	SaveUserPreferences(userEmail string, prefs UserPreferences) (UserPreferences, error)
}

type MemoryUserPreferencesStore struct {
	mu    sync.Mutex
	items map[string]UserPreferences
	now   func() time.Time
}

func NewMemoryUserPreferencesStore() *MemoryUserPreferencesStore {
	return &MemoryUserPreferencesStore{
		items: map[string]UserPreferences{},
		now:   time.Now,
	}
}

func (s *MemoryUserPreferencesStore) UserPreferences(userEmail string) (UserPreferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[userEmail]
	if !ok {
		return UserPreferences{}, ErrNotFound
	}
	return item, nil
}

func (s *MemoryUserPreferencesStore) SaveUserPreferences(userEmail string, prefs UserPreferences) (UserPreferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefs.UpdatedAt = s.now()
	s.items[userEmail] = prefs
	return prefs, nil
}

type PostgresUserPreferencesStore struct {
	db *sql.DB
}

func NewPostgresUserPreferencesStore(db *sql.DB) *PostgresUserPreferencesStore {
	return &PostgresUserPreferencesStore{db: db}
}

func (s *PostgresUserPreferencesStore) UserPreferences(userEmail string) (UserPreferences, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var prefs UserPreferences
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT up.ai_model, up.click_frequency, up.detail_open_probability,
		       up.scroll_delay_min, up.scroll_delay_max,
		       up.list_view_delay_min, up.list_view_delay_max,
		       up.detail_view_delay_min, up.detail_view_delay_max,
		       up.greet_delay_min, up.greet_delay_max,
		       up.rest_after_candidates_min, up.rest_after_candidates_max,
		       up.rest_times_min, up.rest_times_max,
		       up.rest_duration_min, up.rest_duration_max,
		       up.updated_at
		FROM user_preferences up
		INNER JOIN users u ON u.id = up.user_id
		WHERE u.email = $1
		`,
		userEmail,
	).Scan(
		&prefs.AIModel,
		&prefs.ClickFrequency,
		&prefs.DetailOpenProbability,
		&prefs.ScrollDelayMin,
		&prefs.ScrollDelayMax,
		&prefs.ListViewDelayMin,
		&prefs.ListViewDelayMax,
		&prefs.DetailViewDelayMin,
		&prefs.DetailViewDelayMax,
		&prefs.GreetDelayMin,
		&prefs.GreetDelayMax,
		&prefs.RestAfterCandidatesMin,
		&prefs.RestAfterCandidatesMax,
		&prefs.RestTimesMin,
		&prefs.RestTimesMax,
		&prefs.RestDurationMin,
		&prefs.RestDurationMax,
		&prefs.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return UserPreferences{}, ErrNotFound
	}
	if err != nil {
		return UserPreferences{}, err
	}
	return prefs, nil
}

func (s *PostgresUserPreferencesStore) SaveUserPreferences(userEmail string, prefs UserPreferences) (UserPreferences, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, userEmail)
	if err != nil {
		return UserPreferences{}, err
	}

	var saved UserPreferences
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO user_preferences (
			user_id, ai_model, click_frequency, scroll_delay_min, scroll_delay_max,
			detail_open_probability,
			list_view_delay_min, list_view_delay_max,
			detail_view_delay_min, detail_view_delay_max,
			greet_delay_min, greet_delay_max,
			rest_after_candidates_min, rest_after_candidates_max,
			rest_times_min, rest_times_max,
			rest_duration_min, rest_duration_max
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (user_id)
		DO UPDATE SET
			ai_model = EXCLUDED.ai_model,
			click_frequency = EXCLUDED.click_frequency,
			scroll_delay_min = EXCLUDED.scroll_delay_min,
			scroll_delay_max = EXCLUDED.scroll_delay_max,
			detail_open_probability = EXCLUDED.detail_open_probability,
			list_view_delay_min = EXCLUDED.list_view_delay_min,
			list_view_delay_max = EXCLUDED.list_view_delay_max,
			detail_view_delay_min = EXCLUDED.detail_view_delay_min,
			detail_view_delay_max = EXCLUDED.detail_view_delay_max,
			greet_delay_min = EXCLUDED.greet_delay_min,
			greet_delay_max = EXCLUDED.greet_delay_max,
			rest_after_candidates_min = EXCLUDED.rest_after_candidates_min,
			rest_after_candidates_max = EXCLUDED.rest_after_candidates_max,
			rest_times_min = EXCLUDED.rest_times_min,
			rest_times_max = EXCLUDED.rest_times_max,
			rest_duration_min = EXCLUDED.rest_duration_min,
			rest_duration_max = EXCLUDED.rest_duration_max,
			updated_at = now()
		RETURNING ai_model, click_frequency, detail_open_probability,
		          scroll_delay_min, scroll_delay_max,
		          list_view_delay_min, list_view_delay_max,
		          detail_view_delay_min, detail_view_delay_max,
		          greet_delay_min, greet_delay_max,
		          rest_after_candidates_min, rest_after_candidates_max,
		          rest_times_min, rest_times_max,
		          rest_duration_min, rest_duration_max,
		          updated_at
		`,
		userID,
		prefs.AIModel,
		prefs.ClickFrequency,
		prefs.ScrollDelayMin,
		prefs.ScrollDelayMax,
		prefs.DetailOpenProbability,
		prefs.ListViewDelayMin,
		prefs.ListViewDelayMax,
		prefs.DetailViewDelayMin,
		prefs.DetailViewDelayMax,
		prefs.GreetDelayMin,
		prefs.GreetDelayMax,
		prefs.RestAfterCandidatesMin,
		prefs.RestAfterCandidatesMax,
		prefs.RestTimesMin,
		prefs.RestTimesMax,
		prefs.RestDurationMin,
		prefs.RestDurationMax,
	).Scan(
		&saved.AIModel,
		&saved.ClickFrequency,
		&saved.DetailOpenProbability,
		&saved.ScrollDelayMin,
		&saved.ScrollDelayMax,
		&saved.ListViewDelayMin,
		&saved.ListViewDelayMax,
		&saved.DetailViewDelayMin,
		&saved.DetailViewDelayMax,
		&saved.GreetDelayMin,
		&saved.GreetDelayMax,
		&saved.RestAfterCandidatesMin,
		&saved.RestAfterCandidatesMax,
		&saved.RestTimesMin,
		&saved.RestTimesMax,
		&saved.RestDurationMin,
		&saved.RestDurationMax,
		&saved.UpdatedAt,
	)
	if err != nil {
		return UserPreferences{}, err
	}
	return saved, nil
}
