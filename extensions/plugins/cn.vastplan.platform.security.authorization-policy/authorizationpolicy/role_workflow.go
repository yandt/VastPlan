package authorizationpolicy

import (
	"errors"
	"fmt"
)

func (s *Service) createRole(subject string, request CreateRoleRequest, decodeErr error) (any, error) {
	if decodeErr != nil {
		return nil, decodeErr
	}
	if err := validateManagedID(request.ID, "Role ID"); err != nil {
		return nil, err
	}
	if request.Title == "" || len(request.Title) > 160 || len(request.Statements) == 0 {
		return nil, errors.New("Role 标题或 Statements 无效")
	}
	state, err := s.store.Load()
	if err != nil || ensureExpected(state, request.ExpectedGeneration) != nil {
		if err != nil {
			return nil, err
		}
		return nil, ensureExpected(state, request.ExpectedGeneration)
	}
	revision, err := nextRoleRevision(state, request.ID)
	if err != nil {
		return nil, err
	}
	role := RoleRevision{ID: request.ID, Revision: revision, DomainID: request.DomainID, Title: request.Title, Description: request.Description, Statements: cloneStatements(request.Statements), State: StateDraft, CreatedBy: subject, CreatedAt: s.now().UTC(), UpdatedAt: s.now().UTC()}
	if err := validateRole(role, state.Domains, catalogPermissions(state.Catalog)); err != nil {
		return nil, err
	}
	state.Roles = append(state.Roles, role)
	committed, err := s.commit(state, request.ExpectedGeneration, s.audit(subject, "create", "role", role.ID, role.Revision, ""))
	if err != nil {
		return nil, err
	}
	return struct {
		Role       RoleRevision `json:"role"`
		Generation uint64       `json:"generation"`
	}{role, committed.Generation}, nil
}

func (s *Service) updateRole(subject string, request UpdateRoleRequest, decodeErr error) (any, error) {
	if decodeErr != nil {
		return nil, decodeErr
	}
	state, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if err := ensureExpected(state, request.ExpectedGeneration); err != nil {
		return nil, err
	}
	index := roleIndex(state, request.ID, request.Revision)
	if index < 0 || state.Roles[index].State != StateDraft {
		return nil, errors.New("只能修改 Draft Role revision")
	}
	role := state.Roles[index]
	role.Title, role.Description, role.Statements, role.UpdatedAt = request.Title, request.Description, cloneStatements(request.Statements), s.now().UTC()
	if err := validateRole(role, state.Domains, catalogPermissions(state.Catalog)); err != nil {
		return nil, err
	}
	state.Roles[index] = role
	committed, err := s.commit(state, request.ExpectedGeneration, s.audit(subject, "update", "role", role.ID, role.Revision, ""))
	if err != nil {
		return nil, err
	}
	return struct {
		Role       RoleRevision `json:"role"`
		Generation uint64       `json:"generation"`
	}{role, committed.Generation}, nil
}

func (s *Service) transitionRole(subject, operation string, request TransitionRequest, decodeErr error) (any, error) {
	if decodeErr != nil {
		return nil, decodeErr
	}
	state, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if err := ensureExpected(state, request.ExpectedGeneration); err != nil {
		return nil, err
	}
	index := roleIndex(state, request.ID, request.Revision)
	if index < 0 {
		return nil, fmt.Errorf("Role revision 不存在")
	}
	role := state.Roles[index]
	switch operation {
	case "submitRole":
		if role.State != StateDraft {
			return nil, errors.New("只有 Draft Role 可提交")
		}
		role.State = StatePendingApproval
	case "approveRole":
		if role.State != StatePendingApproval {
			return nil, errors.New("只有 PendingApproval Role 可批准")
		}
		if subject == role.CreatedBy {
			return nil, errors.New("Role 创建人与审批人必须不同")
		}
		role.State, role.ApprovedBy = StateApproved, subject
	case "publishRole":
		if role.State != StateApproved || role.ApprovedBy == "" {
			return nil, errors.New("只有已批准 Role 可发布")
		}
		role.State = StatePublished
	case "retireRole":
		if role.State != StatePublished {
			return nil, errors.New("只有 Published Role 可退役")
		}
		for _, binding := range state.Bindings {
			if binding.State == StatePublished && binding.RoleID == role.ID && binding.RoleRevision == role.Revision {
				return nil, errors.New("仍有 Published Binding 引用该 Role")
			}
		}
		role.State = StateRetired
	default:
		return nil, fmt.Errorf("未知 Role transition %s", operation)
	}
	role.UpdatedAt = s.now().UTC()
	state.Roles[index] = role
	committed, err := s.commit(state, request.ExpectedGeneration, s.audit(subject, operation, "role", role.ID, role.Revision, request.Reason))
	if err != nil {
		return nil, err
	}
	return struct {
		Role       RoleRevision `json:"role"`
		Generation uint64       `json:"generation"`
	}{role, committed.Generation}, nil
}

func roleIndex(state State, id string, revision uint64) int {
	for index := range state.Roles {
		if state.Roles[index].ID == id && state.Roles[index].Revision == revision {
			return index
		}
	}
	return -1
}
