package authorizationpolicy

import (
	"errors"
	"fmt"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

func (s *Service) createBinding(subject string, request CreateBindingRequest, decodeErr error) (any, error) {
	if decodeErr != nil {
		return nil, decodeErr
	}
	if err := validateManagedID(request.ID, "Binding ID"); err != nil {
		return nil, err
	}
	if err := validateManagedID(request.Subject.ID, "Subject ID"); err != nil {
		return nil, err
	}
	if err := validWindow(request.NotBefore, request.ExpiresAt); err != nil {
		return nil, err
	}
	state, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if err := ensureExpected(state, request.ExpectedGeneration); err != nil {
		return nil, err
	}
	role, exists := publishedRole(state, request.RoleID, request.RoleRevision)
	if !exists || role.DomainID != request.DomainID {
		return nil, errors.New("Binding 必须引用同 Domain 的 Published Role revision")
	}
	if (request.Subject.Kind != authorizationv1.SubjectUser && request.Subject.Kind != authorizationv1.SubjectGroup) || request.Subject.Issuer == "" {
		return nil, errors.New("Binding Subject 只允许携带稳定 issuer 的用户或外部组")
	}
	revision, err := nextBindingRevision(state, request.ID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	binding := BindingRevision{ID: request.ID, Revision: revision, DomainID: request.DomainID, Subject: request.Subject, RoleID: request.RoleID, RoleRevision: request.RoleRevision, NotBefore: request.NotBefore.UTC(), ExpiresAt: request.ExpiresAt.UTC(), State: StateDraft, CreatedBy: subject, CreatedAt: now, UpdatedAt: now}
	state.Bindings = append(state.Bindings, binding)
	committed, err := s.commit(state, request.ExpectedGeneration, s.audit(subject, "create", "binding", binding.ID, binding.Revision, ""))
	if err != nil {
		return nil, err
	}
	return struct {
		Binding    BindingRevision `json:"binding"`
		Generation uint64          `json:"generation"`
	}{binding, committed.Generation}, nil
}

func (s *Service) updateBinding(subject string, request UpdateBindingRequest, decodeErr error) (any, error) {
	if decodeErr != nil {
		return nil, decodeErr
	}
	if err := validWindow(request.NotBefore, request.ExpiresAt); err != nil {
		return nil, err
	}
	state, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if err := ensureExpected(state, request.ExpectedGeneration); err != nil {
		return nil, err
	}
	index := bindingIndex(state, request.ID, request.Revision)
	if index < 0 || state.Bindings[index].State != StateDraft {
		return nil, errors.New("只能修改 Draft Binding revision")
	}
	role, exists := publishedRole(state, request.RoleID, request.RoleRevision)
	if !exists || role.DomainID != request.DomainID {
		return nil, errors.New("Binding 必须引用同 Domain 的 Published Role revision")
	}
	if err := validateManagedID(request.Subject.ID, "Subject ID"); err != nil {
		return nil, err
	}
	if (request.Subject.Kind != authorizationv1.SubjectUser && request.Subject.Kind != authorizationv1.SubjectGroup) || request.Subject.Issuer == "" {
		return nil, errors.New("Binding Subject 只允许携带稳定 issuer 的用户或外部组")
	}
	binding := state.Bindings[index]
	binding.DomainID = request.DomainID
	binding.Subject = request.Subject
	binding.RoleID = request.RoleID
	binding.RoleRevision = request.RoleRevision
	binding.NotBefore = request.NotBefore.UTC()
	binding.ExpiresAt = request.ExpiresAt.UTC()
	binding.UpdatedAt = s.now().UTC()
	state.Bindings[index] = binding
	committed, err := s.commit(state, request.ExpectedGeneration, s.audit(subject, "update", "binding", binding.ID, binding.Revision, ""))
	if err != nil {
		return nil, err
	}
	return struct {
		Binding    BindingRevision `json:"binding"`
		Generation uint64          `json:"generation"`
	}{binding, committed.Generation}, nil
}

func (s *Service) transitionBinding(subject, operation string, request TransitionRequest, decodeErr error) (any, error) {
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
	index := bindingIndex(state, request.ID, request.Revision)
	if index < 0 {
		return nil, fmt.Errorf("Binding revision 不存在")
	}
	binding := state.Bindings[index]
	switch operation {
	case "submitBinding":
		if binding.State != StateDraft {
			return nil, errors.New("只有 Draft Binding 可提交")
		}
		binding.State = StatePendingApproval
	case "approveBinding":
		if binding.State != StatePendingApproval {
			return nil, errors.New("只有 PendingApproval Binding 可批准")
		}
		if subject == binding.CreatedBy {
			return nil, errors.New("Binding 创建人与审批人必须不同")
		}
		binding.State, binding.ApprovedBy = StateApproved, subject
	case "publishBinding":
		if binding.State != StateApproved || binding.ApprovedBy == "" {
			return nil, errors.New("只有已批准 Binding 可发布")
		}
		if _, exists := publishedRole(state, binding.RoleID, binding.RoleRevision); !exists {
			return nil, errors.New("Binding 引用的 Role 已不可发布")
		}
		binding.State = StatePublished
	case "retireBinding":
		if binding.State != StatePublished {
			return nil, errors.New("只有 Published Binding 可退役")
		}
		binding.State = StateRetired
	default:
		return nil, fmt.Errorf("未知 Binding transition %s", operation)
	}
	binding.UpdatedAt = s.now().UTC()
	state.Bindings[index] = binding
	committed, err := s.commit(state, request.ExpectedGeneration, s.audit(subject, operation, "binding", binding.ID, binding.Revision, request.Reason))
	if err != nil {
		return nil, err
	}
	return struct {
		Binding    BindingRevision `json:"binding"`
		Generation uint64          `json:"generation"`
	}{binding, committed.Generation}, nil
}

func publishedRole(state State, id string, revision uint64) (RoleRevision, bool) {
	for _, role := range state.Roles {
		if role.ID == id && role.Revision == revision && role.State == StatePublished {
			return role, true
		}
	}
	return RoleRevision{}, false
}

func bindingIndex(state State, id string, revision uint64) int {
	for index := range state.Bindings {
		if state.Bindings[index].ID == id && state.Bindings[index].Revision == revision {
			return index
		}
	}
	return -1
}
