// Package model 文件作用：定义候选人列表失败时可随岗位日志展示的安全诊断字段。
package model

// CandidateListDiagnostic 仅记录动作和计数，可选字段用指针区分“为零”和“没有观察到”。
type CandidateListDiagnostic struct {
	Platform           string   `json:"platform"`
	Operation          string   `json:"operation"`
	Stage              string   `json:"stage"`
	LastSuccessStage   string   `json:"last_success_stage,omitempty"`
	ElapsedMS          int64    `json:"elapsed_ms"`
	StageElapsedMS     int64    `json:"stage_elapsed_ms"`
	SelectorKey        string   `json:"selector_key,omitempty"`
	BatchStartIndex    *int     `json:"batch_start_index,omitempty"`
	MaxItems           *int     `json:"max_items,omitempty"`
	ReadConcurrency    *int     `json:"read_concurrency,omitempty"`
	FieldCount         *int     `json:"field_count,omitempty"`
	CardsRead          *int     `json:"cards_read,omitempty"`
	MissingNames       *int     `json:"missing_names,omitempty"`
	ReadAttempt        *int     `json:"read_attempt,omitempty"`
	LoadingVisible     *bool    `json:"loading_visible,omitempty"`
	EmptyVisible       *bool    `json:"empty_visible,omitempty"`
	FirstCardCount     *int     `json:"first_card_count,omitempty"`
	TotalMatches       *int     `json:"total_matches,omitempty"`
	BeforeTotalMatches *int     `json:"before_total_matches,omitempty"`
	AfterTotalMatches  *int     `json:"after_total_matches,omitempty"`
	WindowPhase        string   `json:"window_phase,omitempty"`
	BeforeCount        *int     `json:"before_count,omitempty"`
	NextIndex          *int     `json:"next_index,omitempty"`
	ScrollRound        *int     `json:"scroll_round,omitempty"`
	PollRound          *int     `json:"poll_round,omitempty"`
	TailVisible        *bool    `json:"tail_visible,omitempty"`
	TailMoved          *bool    `json:"tail_moved,omitempty"`
	BottomRounds       *int     `json:"bottom_rounds,omitempty"`
	BlockedRounds      *int     `json:"blocked_rounds,omitempty"`
	NewCards           *int     `json:"new_cards,omitempty"`
	HeadChanged        *bool    `json:"head_changed,omitempty"`
	ParentStatus       string   `json:"parent_status,omitempty"`
	ScopeBudgetMS      *int64   `json:"scope_budget_ms,omitempty"`
	ScopeRemainingMS   *int64   `json:"scope_remaining_ms,omitempty"`
	Timeline           []string `json:"timeline"`
}
