// 文件作用说明：统一简历来源的平台别名和多来源筛选，保留历史插件与自动任务数据。
package httpapi

import (
	"fmt"
	"strings"
)

// candidateSourcePlatforms 返回插件筛选的可信平台别名，未知平台不扩大查询范围。
func candidateSourcePlatforms(value string) []string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "boss":
		return []string{"boss", "zhipin"}
	case "liepin", "hliepin", "liepin_hr", "liepin_headhunter":
		return []string{"liepin", "hliepin", "liepin_hr", "liepin_headhunter"}
	case "zhaopin", "zhilian":
		return []string{"zhaopin", "zhilian"}
	case "51job", "wuyou":
		return []string{"51job", "wuyou"}
	}
	return nil
}

// candidateSourceParts 拆分来源种类和平台；平台值只用于插件来源细分。
func candidateSourceParts(value string) (string, []string) {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(value)), ":", 2)
	if len(parts) == 2 && parts[0] == "browser_extension" {
		return parts[0], candidateSourcePlatforms(parts[1])
	}
	return parts[0], nil
}

// candidateMatchesSource 在全部已保存来源中匹配，兼容旧数据仅有主来源和平台的情况。
func candidateMatchesSource(item PositionCandidate, value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	kind, platforms := candidateSourceParts(value)
	if strings.Contains(value, ":") && len(platforms) == 0 {
		return false
	}
	match := func(source, platform string) bool {
		if firstNonEmpty(strings.ToLower(strings.TrimSpace(source)), "automatic") != kind {
			return false
		}
		if len(platforms) == 0 {
			return true
		}
		for _, alias := range platforms {
			if strings.EqualFold(strings.TrimSpace(platform), alias) {
				return true
			}
		}
		return false
	}
	if match(candidateString(item.LibraryFields, "source_type"), item.PlatformID) {
		return true
	}
	for _, source := range item.Sources {
		if match(firstNonEmpty(source.SourceType, firstNonEmpty(candidateString(item.LibraryFields, "source_type"), "automatic")), source.PlatformID) {
			return true
		}
	}
	return false
}

// candidateSourceFilterSQL 用参数化条件匹配主来源和来源表，不把用户输入拼进 SQL。
func candidateSourceFilterSQL(value string, args *[]any) string {
	kind, platforms := candidateSourceParts(value)
	if strings.Contains(value, ":") && len(platforms) == 0 {
		return "FALSE"
	}
	*args = append(*args, kind)
	kindArg := fmt.Sprintf("$%d", len(*args))
	mainKind := "COALESCE(NULLIF(LOWER(BTRIM(cp.library_fields->>'source_type')),''),'automatic')=" + kindArg
	sourceKind := "COALESCE(NULLIF(LOWER(BTRIM(sf.source_type)),''),NULLIF(LOWER(BTRIM(cp.library_fields->>'source_type')),''),'automatic')=" + kindArg
	mainPlatform, sourcePlatform := "", ""
	if len(platforms) > 0 {
		placeholders := make([]string, 0, len(platforms))
		for _, alias := range platforms {
			*args = append(*args, alias)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(*args)))
		}
		in := " IN (" + strings.Join(placeholders, ",") + ")"
		mainPlatform = " AND LOWER(BTRIM(cp.source_platform_id))" + in
		sourcePlatform = " AND LOWER(BTRIM(sf.platform_id))" + in
	}
	return "((" + mainKind + mainPlatform + ") OR EXISTS(SELECT 1 FROM candidate_profile_sources sf WHERE sf.tenant_id=cp.tenant_id AND sf.candidate_id=cp.id AND " + sourceKind + sourcePlatform + "))"
}
