package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type UserPreferencesService struct {
	auth  *AuthService
	store UserPreferencesStore
}

type userPreferencesRequest struct {
	AIModel                string  `json:"ai_model"`
	ClickFrequency         int     `json:"click_frequency"`
	ScrollDelayMin         int     `json:"scroll_delay_min"`
	ScrollDelayMax         int     `json:"scroll_delay_max"`
	ListViewDelayMin       float64 `json:"list_view_delay_min"`
	ListViewDelayMax       float64 `json:"list_view_delay_max"`
	DetailViewDelayMin     float64 `json:"detail_view_delay_min"`
	DetailViewDelayMax     float64 `json:"detail_view_delay_max"`
	GreetDelayMin          float64 `json:"greet_delay_min"`
	GreetDelayMax          float64 `json:"greet_delay_max"`
	RestAfterCandidatesMin int     `json:"rest_after_candidates_min"`
	RestAfterCandidatesMax int     `json:"rest_after_candidates_max"`
	RestTimesMin           int     `json:"rest_times_min"`
	RestTimesMax           int     `json:"rest_times_max"`
	RestDurationMin        float64 `json:"rest_duration_min"`
	RestDurationMax        float64 `json:"rest_duration_max"`
}

func NewUserPreferencesService(auth *AuthService, store UserPreferencesStore) *UserPreferencesService {
	return &UserPreferencesService{auth: auth, store: store}
}

func (s *UserPreferencesService) User(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		s.UpdateUser(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	prefs, err := s.store.UserPreferences(session.Email)
	if errors.Is(err, ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"config": publicUserPreferences(DefaultUserPreferences()),
		})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user preferences")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": publicUserPreferences(prefs),
	})
}

func (s *UserPreferencesService) UpdateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	var req userPreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	prefs, ok := req.toPreferences(w)
	if !ok {
		return
	}
	saved, err := s.store.SaveUserPreferences(session.Email, prefs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save user preferences")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"config": publicUserPreferences(saved),
	})
}

func (s *UserPreferencesService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return Session{}, false
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return Session{}, false
	}
	return session, true
}

func (r userPreferencesRequest) toPreferences(w http.ResponseWriter) (UserPreferences, bool) {
	prefs := DefaultUserPreferences()
	prefs.AIModel = strings.TrimSpace(r.AIModel)
	prefs.ClickFrequency = r.ClickFrequency
	prefs.ScrollDelayMin = r.ScrollDelayMin
	prefs.ScrollDelayMax = r.ScrollDelayMax
	prefs.ListViewDelayMin = r.ListViewDelayMin
	prefs.ListViewDelayMax = r.ListViewDelayMax
	prefs.DetailViewDelayMin = r.DetailViewDelayMin
	prefs.DetailViewDelayMax = r.DetailViewDelayMax
	prefs.GreetDelayMin = r.GreetDelayMin
	prefs.GreetDelayMax = r.GreetDelayMax
	prefs.RestAfterCandidatesMin = r.RestAfterCandidatesMin
	prefs.RestAfterCandidatesMax = r.RestAfterCandidatesMax
	prefs.RestTimesMin = r.RestTimesMin
	prefs.RestTimesMax = r.RestTimesMax
	prefs.RestDurationMin = r.RestDurationMin
	prefs.RestDurationMax = r.RestDurationMax
	if prefs.ClickFrequency < 0 || prefs.ClickFrequency > 100 {
		writeError(w, http.StatusBadRequest, "click_frequency must be between 0 and 100")
		return UserPreferences{}, false
	}
	if prefs.ScrollDelayMin < 0 || prefs.ScrollDelayMax < prefs.ScrollDelayMin {
		writeError(w, http.StatusBadRequest, "invalid scroll delay range")
		return UserPreferences{}, false
	}
	if prefs.GreetDelayMin < 0 || prefs.GreetDelayMax < prefs.GreetDelayMin {
		writeError(w, http.StatusBadRequest, "invalid greet delay range")
		return UserPreferences{}, false
	}
	return prefs, true
}

func publicUserPreferences(prefs UserPreferences) map[string]any {
	return map[string]any{
		"ai_model":                  prefs.AIModel,
		"click_frequency":           prefs.ClickFrequency,
		"scroll_delay_min":          prefs.ScrollDelayMin,
		"scroll_delay_max":          prefs.ScrollDelayMax,
		"list_view_delay_min":       prefs.ListViewDelayMin,
		"list_view_delay_max":       prefs.ListViewDelayMax,
		"detail_view_delay_min":     prefs.DetailViewDelayMin,
		"detail_view_delay_max":     prefs.DetailViewDelayMax,
		"greet_delay_min":           prefs.GreetDelayMin,
		"greet_delay_max":           prefs.GreetDelayMax,
		"rest_after_candidates_min": prefs.RestAfterCandidatesMin,
		"rest_after_candidates_max": prefs.RestAfterCandidatesMax,
		"rest_times_min":            prefs.RestTimesMin,
		"rest_times_max":            prefs.RestTimesMax,
		"rest_duration_min":         prefs.RestDurationMin,
		"rest_duration_max":         prefs.RestDurationMax,
		"updated_at":                prefs.UpdatedAt,
	}
}
