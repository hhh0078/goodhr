// 本文件负责岗位的团队可见范围和管理员代改权限，始终按当前有效成员校验，不接受客户端指定归属。
package httpapi

import "strings"

// teamPositions 返回当前账号可查看的岗位及团队管理权限；无团队存储时仅返回本人岗位。
func (s *PositionService) teamPositions(email string) ([]Position, bool, error) {
	if s.auth == nil || s.auth.tenantStore == nil {
		items, err := s.store.ListPositions("", email, false)
		return items, false, err
	}
	tenants := s.auth.tenantStore
	tenant, err := tenants.GetOrCreateTenant(email)
	if err != nil {
		return nil, false, err
	}
	members, err := tenants.ListMembers(tenant.ID)
	if err != nil {
		return nil, false, err
	}
	active := make(map[string]bool)
	admin := false
	for _, member := range members {
		if member.Status != "active" {
			continue
		}
		active[strings.ToLower(member.Email)] = true
		if strings.EqualFold(member.Email, email) {
			admin = member.Role == "admin"
		}
	}
	if !active[strings.ToLower(email)] {
		return nil, false, ErrNotFound
	}
	// PostgreSQL按租户查询；内存存储的全部结果也必须再次按有效成员过滤，不能跨团队返回。
	items, err := s.store.ListPositions(tenant.ID, email, true)
	if err != nil {
		return nil, false, err
	}
	visible := make([]Position, 0, len(items))
	for _, item := range items {
		if active[strings.ToLower(item.UserEmail)] {
			visible = append(visible, item)
		}
	}
	return visible, admin, nil
}
