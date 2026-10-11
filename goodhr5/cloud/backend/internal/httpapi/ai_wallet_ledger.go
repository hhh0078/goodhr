// 本文件负责 AI 钱包分笔本金与赠送账本，以及内存环境的原子充值、消费、退款预占与失败返还。
package httpapi

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// ErrAIWalletRefundBlocked 表示历史资金无法完整核对，禁止自动退款但保留余额使用。
var ErrAIWalletRefundBlocked = errors.New("历史余额还需要核对，暂时不能自动退款，请联系我帮你查账。")

const aiWalletLedgerVersion = 1

// aiWalletLot 保存一笔充值或赠送中尚未使用的本金和赠送，按 ID 先后消费。
type aiWalletLot struct {
	ID                      int64
	OrderNo                 string
	SourceCategory          string
	PrincipalUnits          int64
	BonusUnits              int64
	RemainingPrincipalUnits int64
	RemainingBonusUnits     int64
	CreatedAt               time.Time
	PendingRefundNo         string
}

// aiWalletLedger 保存用户总余额、赠送缓存与分笔资金，用于内存和数据库共用资金算法。
type aiWalletLedger struct {
	BalanceUnits      int64
	BonusUnits        int64
	RefundBlockReason string
	Lots              []aiWalletLot
}

// clone 创建独立分笔快照，保证后续存储失败不会改动已生效的钱包。
func (l aiWalletLedger) clone() aiWalletLedger {
	l.Lots = append([]aiWalletLot(nil), l.Lots...)
	return l
}

// checkedAIWalletAdd 检查整数金额相加，阻止溢出后把负值变成可用余额。
func checkedAIWalletAdd(left, right int64) (int64, error) {
	if (right > 0 && left > math.MaxInt64-right) || (right < 0 && left < math.MinInt64-right) {
		return 0, errors.New("余额金额超出了可记录范围。")
	}
	return left + right, nil
}

// validate 校验分笔合计与总余额缓存，异常账本不允许凭空增加可退本金。
func (l aiWalletLedger) validate() error {
	var principal, bonus int64
	for _, lot := range l.Lots {
		if lot.RemainingPrincipalUnits < 0 || lot.RemainingBonusUnits < 0 || lot.RemainingPrincipalUnits > lot.PrincipalUnits || lot.RemainingBonusUnits > lot.BonusUnits {
			return errors.New("钱包分笔金额不一致。")
		}
		var err error
		if principal, err = checkedAIWalletAdd(principal, lot.RemainingPrincipalUnits); err != nil {
			return err
		}
		if bonus, err = checkedAIWalletAdd(bonus, lot.RemainingBonusUnits); err != nil {
			return err
		}
	}
	total, err := checkedAIWalletAdd(principal, bonus)
	if err != nil {
		return err
	}
	if total != max(int64(0), l.BalanceUnits) || bonus != l.BonusUnits {
		return errors.New("钱包合计和分笔金额不一致。")
	}
	return nil
}

// applyRecord 按所有本金 FIFO、所有赠送 FIFO 的顺序调整余额，完成的 AI 费用允许形成负债。
func (l *aiWalletLedger) applyRecord(record *AIWalletRecord) error {
	if strings.TrimSpace(record.UserEmail) == "" {
		return errors.New("钱包账号不能为空。")
	}
	if err := l.validate(); err != nil {
		return err
	}
	newBalance, err := checkedAIWalletAdd(l.BalanceUnits, record.ChangeUnits)
	if err != nil {
		return err
	}
	beforeBonus := l.BonusUnits
	if record.ChangeUnits > 0 {
		principal, bonus := int64(0), record.ChangeUnits
		if record.Category == "recharge" {
			if record.BonusChangeUnits < 0 || record.BonusChangeUnits > record.ChangeUnits {
				return errors.New("充值赠送金额不正确。")
			}
			bonus = record.BonusChangeUnits
			principal = record.ChangeUnits - bonus
		}
		cashRemaining, bonusRemaining := l.creditAfterDebt(principal, bonus)
		orderNo := ""
		if record.Category == "recharge" {
			orderNo = strings.TrimSpace(record.RelatedOrderNo)
		}
		l.Lots = append(l.Lots, aiWalletLot{
			OrderNo: orderNo, SourceCategory: record.Category,
			PrincipalUnits: principal, BonusUnits: bonus, RemainingPrincipalUnits: cashRemaining,
			RemainingBonusUnits: bonusRemaining, CreatedAt: record.CreatedAt,
		})
	} else if record.ChangeUnits < 0 {
		if record.ChangeUnits == math.MinInt64 {
			return errors.New("扣费金额超出了可记录范围。")
		}
		l.consume(-record.ChangeUnits)
	}
	l.BalanceUnits = newBalance
	l.recalculateBonus()
	record.BonusChangeUnits = l.BonusUnits - beforeBonus
	record.BonusBalanceAfterUnits = l.BonusUnits
	record.BalanceAfterUnits = l.BalanceUnits
	return l.validate()
}

// creditAfterDebt 先用新本金抵欠费，再用新赠送抵欠费，返回真正可用的两部分金额。
func (l aiWalletLedger) creditAfterDebt(principal, bonus int64) (int64, int64) {
	debt := int64(0)
	if l.BalanceUnits < 0 {
		// 最小整数的债务无法在单次正数入账中还清，避免取反溢出。
		if l.BalanceUnits == math.MinInt64 {
			return 0, 0
		}
		debt = -l.BalanceUnits
	}
	cashUsed := min(principal, debt)
	debt -= cashUsed
	return principal - cashUsed, bonus - min(bonus, debt)
}

// consume 先按分笔顺序扣完所有现金本金，再按相同顺序扣赠送。
func (l *aiWalletLedger) consume(units int64) {
	for index := range l.Lots {
		used := min(l.Lots[index].RemainingPrincipalUnits, units)
		l.Lots[index].RemainingPrincipalUnits -= used
		units -= used
		if units == 0 {
			return
		}
	}
	for index := range l.Lots {
		used := min(l.Lots[index].RemainingBonusUnits, units)
		l.Lots[index].RemainingBonusUnits -= used
		units -= used
		if units == 0 {
			return
		}
	}
}

// recalculateBonus 从分笔账本同步不可退款赠送缓存。
func (l *aiWalletLedger) recalculateBonus() {
	l.BonusUnits = 0
	for _, lot := range l.Lots {
		l.BonusUnits += lot.RemainingBonusUnits
	}
}

// refundable 查找指定订单剩余本金，异常账本和负余额均不能用于退款。
func (l aiWalletLedger) refundable(orderNo string) (int64, error) {
	if l.RefundBlockReason != "" {
		return 0, ErrAIWalletRefundBlocked
	}
	if err := l.validate(); err != nil {
		return 0, ErrAIWalletRefundBlocked
	}
	if l.BalanceUnits <= 0 {
		return 0, nil
	}
	for _, lot := range l.Lots {
		if lot.SourceCategory == "recharge" && lot.OrderNo == orderNo && orderNo != "" {
			if lot.PendingRefundNo != "" {
				return 0, nil
			}
			return min(lot.RemainingPrincipalUnits, l.BalanceUnits-l.BonusUnits), nil
		}
	}
	return 0, nil
}

// reserve 验证订单剩余本金，预扣退款额并收回这笔充值尚未使用的全部赠送。
func (l *aiWalletLedger) reserve(refund *PaymentRefund) (AIWalletRecord, error) {
	available, err := l.refundable(refund.OrderNo)
	if err != nil {
		return AIWalletRecord{}, err
	}
	units := centsToAIUnits(refund.AmountCents)
	if refund.AmountCents <= 0 || units <= 0 || units > available {
		return AIWalletRecord{}, errors.New("这笔充值剩余本金不够退款，我先替你拦下了。")
	}
	for index := range l.Lots {
		lot := &l.Lots[index]
		if lot.SourceCategory != "recharge" || lot.OrderNo != refund.OrderNo {
			continue
		}
		refund.RevokedBonusUnits = lot.RemainingBonusUnits
		refund.RightsReserved = true
		lot.RemainingPrincipalUnits -= units
		lot.RemainingBonusUnits = 0
		lot.PendingRefundNo = refund.RefundNo
		l.BalanceUnits -= units + refund.RevokedBonusUnits
		l.recalculateBonus()
		return AIWalletRecord{
			UserEmail: refund.UserEmail, ChangeUnits: -units - refund.RevokedBonusUnits,
			BalanceAfterUnits: l.BalanceUnits, BonusChangeUnits: -refund.RevokedBonusUnits,
			BonusBalanceAfterUnits: l.BonusUnits, Category: "refund", Reason: "AI余额退款并收回该笔充值赠送",
			RelatedOrderNo: refund.RefundNo, CreatedAt: time.Now(),
		}, l.validate()
	}
	return AIWalletRecord{}, errors.New("没有找到这笔充值的本金记录。")
}

// release 将明确失败退款预扣的本金和赠送归回原订单，先抵期间新产生的 AI 欠费。
func (l *aiWalletLedger) release(refund PaymentRefund) (AIWalletRecord, error) {
	if err := l.validate(); err != nil {
		return AIWalletRecord{}, err
	}
	principal := centsToAIUnits(refund.AmountCents)
	change, err := checkedAIWalletAdd(principal, refund.RevokedBonusUnits)
	if err != nil {
		return AIWalletRecord{}, err
	}
	balance, err := checkedAIWalletAdd(l.BalanceUnits, change)
	if err != nil {
		return AIWalletRecord{}, err
	}
	cashRemaining, bonusRemaining := l.creditAfterDebt(principal, refund.RevokedBonusUnits)
	beforeBonus := l.BonusUnits
	for index := range l.Lots {
		lot := &l.Lots[index]
		if lot.SourceCategory != "recharge" || lot.OrderNo != refund.OrderNo {
			continue
		}
		lot.RemainingPrincipalUnits += cashRemaining
		lot.RemainingBonusUnits += bonusRemaining
		lot.PendingRefundNo = ""
		l.BalanceUnits = balance
		l.recalculateBonus()
		return AIWalletRecord{
			UserEmail: refund.UserEmail, ChangeUnits: change, BalanceAfterUnits: balance,
			BonusChangeUnits: l.BonusUnits - beforeBonus, BonusBalanceAfterUnits: l.BonusUnits,
			Category: "refund_release", Reason: "退款未完成，已返还预扣本金及赠送",
			RelatedOrderNo: refund.RefundNo, CreatedAt: time.Now(),
		}, l.validate()
	}
	return AIWalletRecord{}, errors.New("退款原充值账本未找到，暂不能自动返还。")
}

// validateWalletRefundOrder 防止越权退款、会员订单退款或超过订单累计实付金额。
func validateWalletRefundOrder(order PaymentOrder, refund PaymentRefund, occupiedCents int) error {
	if refund.RefundType != refundTypeAIBalance || refund.RefundNo == "" || refund.UserEmail == "" || order.UserEmail != refund.UserEmail || order.OrderType != "ai_balance" || order.Status != "paid" {
		return errors.New("这笔充值订单暂时不能退款。")
	}
	if refund.AmountCents <= 0 || int64(refund.AmountCents) > math.MaxInt64/aiWalletUnitsPerCent || order.AmountCents <= 0 || occupiedCents < 0 || occupiedCents > order.AmountCents || refund.AmountCents > order.AmountCents-occupiedCents {
		return errors.New("退款金额超过了这笔订单尚可退的实付金额。")
	}
	return nil
}

// walletRefundTerminal 判断已确定成功或已经返还的退款，阻止迟到状态回退造成重复扣返。
func walletRefundTerminal(status string) bool {
	return status == "success" || status == "failed" || status == "closed"
}

// normalizeWalletRefundStatus 将渠道状态规范为本地状态；不明确的异常保持占用。
func normalizeWalletRefundStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success":
		return "success"
	case "failed":
		return "failed"
	case "closed":
		return "closed"
	case "processing":
		return "processing"
	case "pending":
		return "pending"
	case "action_required", "abnormal":
		return "action_required"
	default:
		return "unknown"
	}
}

// memoryLedgerLocked 读取内存账本，旧式直接写余额的测试数据暂停自动退款。
func (s *MemoryAIWalletStore) memoryLedgerLocked(email string) aiWalletLedger {
	l, exists := s.ledgers[email]
	if !exists {
		l.BalanceUnits = s.balances[email]
		if l.BalanceUnits > 0 {
			l.BonusUnits = l.BalanceUnits
			l.RefundBlockReason = "余额没有分笔账本"
			l.Lots = []aiWalletLot{{SourceCategory: "legacy_unreconciled", BonusUnits: l.BalanceUnits, RemainingBonusUnits: l.BalanceUnits}}
		}
	}
	return l.clone()
}

// saveMemoryLedgerLocked 保存资金缓存与流水，调用方必须已经持有钱包锁。
func (s *MemoryAIWalletStore) saveMemoryLedgerLocked(email string, ledger aiWalletLedger, record *AIWalletRecord) {
	if s.ledgers == nil {
		s.ledgers = make(map[string]aiWalletLedger)
	}
	if s.balances == nil {
		s.balances = make(map[string]int64)
	}
	for index := range ledger.Lots {
		if ledger.Lots[index].ID == 0 {
			ledger.Lots[index].ID = int64(index + 1)
		}
	}
	s.ledgers[email] = ledger
	s.balances[email] = ledger.BalanceUnits
	if record != nil {
		record.WalletLedgerVersion = aiWalletLedgerVersion
		record.ID = fmt.Sprintf("wallet-%d", len(s.records)+1)
		s.records = append(s.records, *record)
	}
}

// adjustLedgerBalance 在同一内存锁中进行现金赠送分桶及重复支付去重。
func (s *MemoryAIWalletStore) adjustLedgerBalance(record AIWalletRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.Category == "recharge" && strings.TrimSpace(record.RelatedOrderNo) != "" {
		for _, previous := range s.records {
			if previous.Category == "recharge" && previous.RelatedOrderNo == record.RelatedOrderNo {
				if previous.UserEmail != record.UserEmail {
					return 0, errors.New("充值订单所属账号不一致。")
				}
				return s.balances[record.UserEmail], nil
			}
		}
	}
	l := s.memoryLedgerLocked(record.UserEmail)
	record.CreatedAt = time.Now()
	if err := l.applyRecord(&record); err != nil {
		return 0, err
	}
	s.saveMemoryLedgerLocked(record.UserEmail, l, &record)
	return l.BalanceUnits, nil
}

// BonusBalanceUnits 读取内存赠送缓存。
func (s *MemoryAIWalletStore) BonusBalanceUnits(email string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryLedgerLocked(email).BonusUnits, nil
}

// BalanceSnapshot 在同一内存锁内读取钱包总额与赠送缓存。
func (s *MemoryAIWalletStore) BalanceSnapshot(email string) (int64, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ledger := s.memoryLedgerLocked(email)
	return ledger.BalanceUnits, ledger.BonusUnits, nil
}

// RefundableUnits 读取内存订单剩余本金。
func (s *MemoryAIWalletStore) RefundableUnits(email, orderNo string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.memoryLedgerLocked(email).refundable(orderNo)
}

// ReserveRefund 将内存退款记录和钱包预扣放在同一钱包锁内，阻止消费与退款同时花同一份本金。
func (s *MemoryAIWalletStore) ReserveRefund(refund PaymentRefund, orders PaymentStore) (PaymentRefund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if orders == nil {
		return PaymentRefund{}, errors.New("退款记录存储未配置。")
	}
	if existing, err := orders.ByRefundNo(refund.RefundNo); err == nil {
		if existing.OrderNo != refund.OrderNo || existing.UserEmail != refund.UserEmail || existing.AmountCents != refund.AmountCents {
			return PaymentRefund{}, errors.New("退款单号与原请求不一致。")
		}
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return PaymentRefund{}, err
	}
	order, err := orders.ByOrderNo(refund.OrderNo)
	if err != nil {
		return PaymentRefund{}, err
	}
	occupied, err := orders.RefundedCentsByOrder(refund.OrderNo)
	if err != nil {
		return PaymentRefund{}, err
	}
	if err = validateWalletRefundOrder(order, refund, occupied); err != nil {
		return PaymentRefund{}, err
	}
	openRefunds, err := orders.ListOpenRefunds(refund.UserEmail)
	if err != nil {
		return PaymentRefund{}, err
	}
	for _, item := range openRefunds {
		if item.OrderNo == refund.OrderNo {
			return PaymentRefund{}, errors.New("这笔充值已有退款在处理中，等它确认后再申请。")
		}
	}
	l := s.memoryLedgerLocked(refund.UserEmail)
	record, err := l.reserve(&refund)
	if err != nil {
		return PaymentRefund{}, err
	}
	refund.Status = "pending"
	stored, err := orders.CreateRefund(refund)
	if err != nil {
		return PaymentRefund{}, err
	}
	s.saveMemoryLedgerLocked(refund.UserEmail, l, &record)
	return stored, nil
}

// ResolveRefund 原子处理内存退款结果，成功终态和已返还终态不会被重复通知改写。
func (s *MemoryAIWalletStore) ResolveRefund(refundNo, status, providerRefundID, responseData string, orders PaymentStore) (PaymentRefund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if orders == nil {
		return PaymentRefund{}, errors.New("退款记录存储未配置。")
	}
	refund, err := orders.ByRefundNo(refundNo)
	if err != nil {
		return PaymentRefund{}, err
	}
	if refund.RefundType != refundTypeAIBalance {
		return PaymentRefund{}, errors.New("退款类型与钱包不一致。")
	}
	if walletRefundTerminal(refund.Status) {
		return refund, nil
	}
	status = normalizeWalletRefundStatus(status)
	l := s.memoryLedgerLocked(refund.UserEmail)
	var record *AIWalletRecord
	if (status == "failed" || status == "closed") && refund.RightsReserved {
		var hasReservation bool
		for _, item := range s.records {
			if item.Category == "refund" && item.RelatedOrderNo == refundNo && item.UserEmail == refund.UserEmail {
				hasReservation = true
			}
		}
		if !hasReservation {
			return PaymentRefund{}, errors.New("退款预扣流水未核对，暂不能自动返还。")
		}
		item, releaseErr := l.release(refund)
		if releaseErr != nil {
			return PaymentRefund{}, releaseErr
		}
		record = &item
	}
	if providerRefundID == "" {
		providerRefundID = refund.ProviderRefundID
	}
	stored, err := orders.UpdateRefund(refundNo, status, providerRefundID, responseData)
	if err != nil {
		return PaymentRefund{}, err
	}
	if walletRefundTerminal(status) {
		for index := range l.Lots {
			if l.Lots[index].PendingRefundNo == refundNo {
				l.Lots[index].PendingRefundNo = ""
			}
		}
	}
	s.saveMemoryLedgerLocked(refund.UserEmail, l, record)
	return stored, nil
}
