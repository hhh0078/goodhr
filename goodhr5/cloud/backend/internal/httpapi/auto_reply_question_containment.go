// 本文件为同一候选人原话的长题和子题提供严格包含去重，不改写旧题目、HR答复或已领取的处理状态。
package httpapi

import (
	"context"
	"database/sql"
	"strings"
)

// findContainedSourceQuestion 在来源消息锁保护下锁定相含旧题，仅同团队、岗位、成员和真实消息号参与匹配。
// 调用方须先取得来源消息的排他行锁；问题行按编号加锁，HR开始处理后不再作为可复用待办。
func findContainedSourceQuestion(ctx context.Context, tx *sql.Tx, tenant, user, pos, messageID, question string) (replyQuestion, bool, error) {
	rows, err := tx.QueryContext(ctx, "SELECT "+questionCols+` FROM auto_reply_cloud_questions q
 WHERE q.tenant_id=$1 AND q.position_id=$2 AND q.question<>''
 AND (strpos(q.question,$5)>0 OR strpos($5,q.question)>0)
 AND EXISTS(SELECT 1 FROM auto_reply_cloud_question_sources s WHERE s.question_id=q.id AND s.tenant_id=$1 AND s.user_id=$3 AND s.message_id=$4)
 ORDER BY q.id FOR UPDATE OF q`, tenant, pos, user, messageID, question)
	if err != nil {
		return replyQuestion{}, false, err
	}
	defer rows.Close()
	var candidates []replyQuestion
	for rows.Next() {
		q, scanErr := scanReplyQuestion(rows)
		if scanErr != nil {
			return replyQuestion{}, false, scanErr
		}
		candidates = append(candidates, q)
	}
	if err = rows.Err(); err != nil {
		return replyQuestion{}, false, err
	}
	q, reused := uniqueContainedPendingQuestion(question, candidates)
	return q, reused, nil
}

// uniqueContainedPendingQuestion 仅复用唯一相含且未填写答复的pending旧题，多条相含或任何处理中状态均不强并。
// candidates 必须已由数据库限定为同一来源；独立子题不相含时保持各自记录。
func uniqueContainedPendingQuestion(question string, candidates []replyQuestion) (replyQuestion, bool) {
	question = strings.TrimSpace(question)
	if question == "" {
		return replyQuestion{}, false
	}
	var match replyQuestion
	for _, q := range candidates {
		if q.ID == "" || q.Question == "" || !strings.Contains(question, q.Question) && !strings.Contains(q.Question, question) {
			continue
		}
		if match.ID != "" {
			return replyQuestion{}, false
		}
		match = q
	}
	return match, match.ID != "" && match.Status == "pending" && match.Answer == ""
}
