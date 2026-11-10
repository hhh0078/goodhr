// Package httpapi 本文件校验系统话术并将岗位的修改和删除与最新默认配置合并。
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const autoReplyLibraryKey = "system.auto_reply_library"

// ReplyPhrase 表示一个稳定场景的可编辑话术，岗位可以仅按编号隐藏默认话术。
type ReplyPhrase struct {
	ID       string `json:"id"`
	Category string `json:"category,omitempty"`
	Title    string `json:"title,omitempty"`
	Scene    string `json:"scene,omitempty"`
	Content  string `json:"content"`
	Deleted  bool   `json:"deleted,omitempty"`
}

// autoReplyLibrary 保存管理员维护的有序话术和常用可选条件。
type autoReplyLibrary struct {
	Phrases          []ReplyPhrase `json:"phrases"`
	ConditionOptions []string      `json:"condition_options"`
}

var errAutoReplyLibraryConflict = errors.New("默认配置刚有更新，请重新打开后再保存")

var replyPhraseID = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,79}$`)

// parseAutoReplyLibrary 只接受显式列表，空列表保留管理员清空的决定。
func parseAutoReplyLibrary(raw string) (autoReplyLibrary, error) {
	var lib autoReplyLibrary
	if err := json.Unmarshal([]byte(raw), &lib); err != nil || lib.Phrases == nil || lib.ConditionOptions == nil {
		return lib, newAutoReplyValidationError("请提供话术和常用条件两个列表")
	}
	active := 0
	for _, p := range lib.Phrases {
		if !p.Deleted {
			active++
		}
	}
	if active > 64 || len(lib.Phrases) > 128 || len(lib.ConditionOptions) > 80 {
		return lib, newAutoReplyValidationError("可用话术最多64条，含已移除场景最多128条；常用条件最多80条")
	}
	if err := validateReplyPhrases(lib.Phrases, true, ""); err != nil {
		return lib, err
	}
	seen := map[string]bool{}
	for _, v := range lib.ConditionOptions {
		key := normalizeAutoReplyDedupeKey(v)
		if strings.TrimSpace(v) == "" || utf8.RuneCountInString(v) > 1000 || seen[key] {
			return lib, newAutoReplyValidationError("常用条件不能为空或重复，每项最多1000字")
		}
		seen[key] = true
	}
	return lib, nil
}

// validateReplyPhrases 拒绝重复编号、无场景或过长话术；旧版已存的索简历长句可以原样保存。
func validateReplyPhrases(items []ReplyPhrase, defaults bool, legacyResume string) error {
	if len(items) > 128 {
		return newAutoReplyValidationError("岗位话术调整最多128条")
	}
	seen := map[string]bool{}
	for _, p := range items {
		if !replyPhraseID.MatchString(p.ID) || seen[p.ID] || (defaults && strings.HasPrefix(p.ID, "job_")) {
			return newAutoReplyValidationError("话术编号无效或重复，请重新打开后再试")
		}
		seen[p.ID] = true
		if p.Deleted {
			if p.Content != "" {
				return newAutoReplyValidationError("已移除话术只保留场景，不保留回复正文")
			}
			// 兼容旧岗位仅有编号的默认删除标记，保存边界会从已知配置补回场景。
			if !defaults && !strings.HasPrefix(p.ID, "job_") && p.Title == "" && p.Scene == "" && p.Category == "" {
				continue
			}
		}
		contentLimit := 200
		if !defaults && p.ID == "request_resume" && legacyResume != "" && p.Content == legacyResume {
			contentLimit = 500
		}
		for _, field := range []struct {
			label, value string
			limit        int
		}{{"分类", p.Category, 24}, {"标题", p.Title, 60}, {"适用场景", p.Scene, 200}} {
			if strings.TrimSpace(field.value) == "" || utf8.RuneCountInString(field.value) > field.limit {
				return newAutoReplyValidationErrorf("话术%s不能为空，最多%d字", field.label, field.limit)
			}
		}
		if utf8.RuneCountInString(p.Content) > contentLimit {
			return newAutoReplyValidationErrorf("话术内容最多%d字；留空表示该场景不发文字", contentLimit)
		}
	}
	return nil
}

// applyReplyLibrary 合并配置，为 Agent 输出已生效话术；编辑器还需要看到当前默认值、覆盖和常用条件。
func applyReplyLibrary(config *PositionAutoReplyConfig, lib autoReplyLibrary) {
	changes := make(map[string]ReplyPhrase, len(config.PhraseOverrides))
	for _, p := range config.PhraseOverrides {
		changes[p.ID] = p
	}
	config.Phrases = make([]ReplyPhrase, 0, len(lib.Phrases)+len(changes))
	for _, base := range lib.Phrases {
		p, changed := changes[base.ID]
		if !changed {
			if !base.Deleted {
				config.Phrases = append(config.Phrases, base)
			}
			continue
		}
		delete(changes, base.ID)
		if !p.Deleted {
			config.Phrases = append(config.Phrases, p)
		}
	}
	for _, p := range config.PhraseOverrides {
		if _, left := changes[p.ID]; left && !p.Deleted {
			config.Phrases = append(config.Phrases, p)
		}
	}
	config.DefaultPhrases = lib.Phrases
	config.ConditionOptions = lib.ConditionOptions
	config.PhrasesVersion = lib.revision()
	config.ResumeRequestMessage = ""
	for _, p := range config.Phrases {
		if p.ID == "request_resume" {
			config.ResumeRequestMessage = p.Content
			break
		}
	}
}

// revision 按有效设置计算稳定摘要，忽略数据库 JSON 的缩进和键顺序。
func (lib autoReplyLibrary) revision() string {
	raw, _ := json.Marshal(lib)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// readAutoReplyLibrary 动态读取已迁移的系统配置；读取失败时由调用方停止准备回复。
func readAutoReplyLibrary(store SystemConfigStore) (autoReplyLibrary, error) {
	record, err := store.Get(autoReplyLibraryKey)
	if err != nil {
		return autoReplyLibrary{}, fmt.Errorf("读取默认话术失败：%w", err)
	}
	return parseAutoReplyLibrary(record.ConfigValue)
}

// forAgent 下发有效话术与已移除场景的空正文标记，保留禁发线索，不向编辑器重新显示删除项。
func (config PositionAutoReplyConfig) forAgent() PositionAutoReplyConfig {
	config.Phrases = append([]ReplyPhrase{}, config.Phrases...)
	seen := make(map[string]bool, len(config.Phrases))
	for _, p := range config.Phrases {
		seen[p.ID] = true
	}
	known := make(map[string]ReplyPhrase, len(config.DefaultPhrases))
	for _, p := range config.DefaultPhrases {
		known[p.ID] = p
	}
	for _, p := range config.PhraseOverrides {
		if p.Deleted {
			known[p.ID] = deletedReplyPhrase(p, known[p.ID])
		} else {
			known[p.ID] = p
		}
	}
	for _, p := range append(slices.Clone(config.DefaultPhrases), config.PhraseOverrides...) {
		current := known[p.ID]
		if current.Deleted && !seen[p.ID] {
			config.Phrases = append(config.Phrases, deletedReplyPhrase(current, p))
			seen[p.ID] = true
		}
	}
	config.DefaultPhrases = nil
	config.PhraseOverrides = nil
	config.ConditionOptions = nil
	return config
}

// prepareReplyOverrides 在岗位行锁内兼容旧版单句话术，校验修改对象并保留独立岗位版本。
func prepareReplyOverrides(item *PositionAutoReplyConfig, previous []ReplyPhrase, lib autoReplyLibrary) error {
	known := make(map[string]ReplyPhrase, len(previous)+len(lib.Phrases))
	old := PositionAutoReplyConfig{PhraseOverrides: previous}
	applyReplyLibrary(&old, lib)
	for _, p := range lib.Phrases {
		known[p.ID] = p
	}
	for _, p := range previous {
		if p.Deleted {
			known[p.ID] = deletedReplyPhrase(p, known[p.ID])
		} else {
			known[p.ID] = p
		}
	}
	legacyAllowed := old.ResumeRequestMessage
	if item.PhraseOverrides == nil {
		// 旧客户端没有话术列表：保留所有设置，只处理它明确提交的索简历内容。
		item.PhraseOverrides = append([]ReplyPhrase{}, previous...)
		text := strings.TrimSpace(item.ResumeRequestMessage)
		if text != "" && text != AutoReplyDefaultResumeRequestMessage && text != old.ResumeRequestMessage {
			p, ok := known["request_resume"]
			if !ok || p.Deleted {
				p = ReplyPhrase{ID: "request_resume", Category: "简历", Title: "索要简历", Scene: "沟通需要简历，候选人尚未提交也未拒绝"}
			}
			p.Content = text
			known[p.ID] = p
			legacyAllowed = text
			index := slices.IndexFunc(item.PhraseOverrides, func(entry ReplyPhrase) bool { return entry.ID == p.ID })
			if index < 0 {
				item.PhraseOverrides = append(item.PhraseOverrides, p)
			} else {
				item.PhraseOverrides[index] = p
			}
		}
	}
	for i, p := range item.PhraseOverrides {
		if _, ok := known[p.ID]; !ok && !strings.HasPrefix(p.ID, "job_") {
			return newAutoReplyValidationError("默认话术已变化，请重新打开后再保存")
		}
		if p.Deleted {
			base, exists := known[p.ID]
			if !exists {
				// 同一份草稿中新建后移除的自建话术尚无旧记录，仍须通过完整场景校验。
				if !strings.HasPrefix(p.ID, "job_") {
					return newAutoReplyValidationError("已移除话术缺少原场景，请重新打开后再保存")
				}
				base = p
			}
			item.PhraseOverrides[i] = deletedReplyPhrase(base, p)
		}
	}
	// 新旧客户端直接移除自建条目时，在服务器保留原场景；去掉默认覆盖仍代表恢复继承。
	present := make(map[string]bool, len(item.PhraseOverrides))
	activeDefaults := make(map[string]bool, len(lib.Phrases))
	for _, p := range item.PhraseOverrides {
		present[p.ID] = true
	}
	for _, p := range lib.Phrases {
		activeDefaults[p.ID] = !p.Deleted
	}
	for _, p := range previous {
		if !present[p.ID] && !activeDefaults[p.ID] {
			item.PhraseOverrides = append(item.PhraseOverrides, deletedReplyPhrase(known[p.ID], p))
			present[p.ID] = true
		}
	}
	if err := validateReplyPhrases(item.PhraseOverrides, false, legacyAllowed); err != nil {
		return err
	}
	applyReplyLibrary(item, lib)
	return nil
}

// deletedReplyPhrase 保留已知编号与场景元数据，只清回复正文，不从旧配置复活已移除的文字。
func deletedReplyPhrase(phrase, fallback ReplyPhrase) ReplyPhrase {
	if phrase.Category == "" {
		phrase.Category = fallback.Category
	}
	if phrase.Title == "" {
		phrase.Title = fallback.Title
	}
	if phrase.Scene == "" {
		phrase.Scene = fallback.Scene
	}
	phrase.Content, phrase.Deleted = "", true
	return phrase
}

// preserveDeletedDefaultPhrases 兼容管理员直接移除旧条目，保存空正文场景供服务器判断，界面仍只显示有效条目。
func preserveDeletedDefaultPhrases(previous, incoming string) (string, error) {
	old, err := parseAutoReplyLibrary(previous)
	if err != nil {
		return "", err
	}
	next, err := parseAutoReplyLibrary(incoming)
	if err != nil {
		return "", err
	}
	present := make(map[string]bool, len(next.Phrases))
	for _, p := range next.Phrases {
		present[p.ID] = true
	}
	changed := false
	for _, p := range old.Phrases {
		if !present[p.ID] {
			next.Phrases = append(next.Phrases, deletedReplyPhrase(p, ReplyPhrase{}))
			changed = true
		}
	}
	if !changed {
		return incoming, nil
	}
	if err = validateReplyPhrases(next.Phrases, true, ""); err != nil {
		return "", err
	}
	// 系统配置允许扩展字段；只替换已验证的话术列表，不能因保留删除场景而抹掉其它设置。
	var document map[string]json.RawMessage
	if err = json.Unmarshal([]byte(incoming), &document); err != nil {
		return "", err
	}
	phrases, err := json.Marshal(next.Phrases)
	if err != nil {
		return "", err
	}
	document["phrases"] = phrases
	raw, err := json.Marshal(document)
	return string(raw), err
}
