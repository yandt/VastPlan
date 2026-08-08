package workfloworchestrator

import (
	"context"
	"fmt"
	"sort"

	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
)

func (s *Service) advance(ctx context.Context, repository Repository, value instanceRecord, definition workflowv1.Definition, actorID string) error {
	for value.Instance.Status == workflowv1.InstanceRunning {
		node := findNode(definition, value.Instance.CurrentNodeID)
		switch node.Type.ID {
		case workflowv1.NodeManual:
			outcomes := make([]string, 0, len(node.Outcomes))
			for outcome := range node.Outcomes {
				outcomes = append(outcomes, outcome)
			}
			sort.Strings(outcomes)
			task := workflowv1.Task{ID: taskKey(value.Instance.ID, node.ID), InstanceID: value.Instance.ID, NodeID: node.ID, Title: node.Title, Roles: append([]string(nil), node.Roles...), AllowedOutcomes: outcomes, Status: workflowv1.WorkPending, Revision: 1, CreatedAt: s.now().UTC()}
			document, _ := encodeDocument(task)
			_, err := repository.Create(ctx, storedRecord{ID: task.ID, Kind: kindTask, ServiceID: value.Instance.ServiceID, FeatureID: value.Instance.FeatureID, Status: string(task.Status), Document: document}, "task:"+task.ID)
			if err == ErrConflict {
				return nil
			}
			return err
		case workflowv1.NodeAction:
			work := workflowv1.ActionWork{ID: actionKey(value.Instance.ID, node.ID), InstanceID: value.Instance.ID, NodeID: node.ID, ActionID: node.ActionID, Attempt: 1, IdempotencyKey: value.Instance.ID + "/" + node.ID + "/1", Status: workflowv1.WorkPending, Revision: 1, CreatedAt: s.now().UTC()}
			document, _ := encodeDocument(work)
			_, err := repository.Create(ctx, storedRecord{ID: work.ID, Kind: kindAction, ServiceID: value.Instance.ServiceID, FeatureID: value.Instance.FeatureID, Status: string(work.Status), Document: document}, "action:"+work.ID)
			if err == ErrConflict {
				return nil
			}
			return err
		case workflowv1.NodeEnd:
			value.Instance.Status = workflowv1.InstanceStatus(node.Result)
			value.Instance.CurrentNodeID = ""
			value.Instance.UpdatedAt = s.now().UTC()
			value.Instance.Audit = append(value.Instance.Audit, workflowv1.AuditEvent{Action: "completed", NodeID: node.ID, ActorID: actorID, Outcome: string(node.Result), At: value.Instance.UpdatedAt})
			record, err := repository.Get(ctx, instanceKey(value.Instance.ID))
			if err != nil {
				return err
			}
			record.Status = string(value.Instance.Status)
			record.Document, _ = encodeDocument(value)
			_, err = repository.Update(ctx, record, record.Revision, fmt.Sprintf("complete:%s:%d", value.Instance.ID, record.Revision))
			return err
		default:
			return ErrInvalidState
		}
	}
	return nil
}

func findNode(definition workflowv1.Definition, id string) workflowv1.Node {
	for _, node := range definition.Nodes {
		if node.ID == id {
			return node
		}
	}
	return workflowv1.Node{}
}

func (s *Service) PendingAction(ctx context.Context, repository Repository, instanceID string) (workflowv1.ActionWork, workflowv1.ActionDescriptor, workflowv1.ActionRequest, error) {
	instanceStored, err := repository.Get(ctx, instanceKey(instanceID))
	if err != nil {
		return workflowv1.ActionWork{}, workflowv1.ActionDescriptor{}, workflowv1.ActionRequest{}, err
	}
	value, err := decodeDocument[instanceRecord](instanceStored)
	if err != nil {
		return workflowv1.ActionWork{}, workflowv1.ActionDescriptor{}, workflowv1.ActionRequest{}, err
	}
	workStored, err := repository.Get(ctx, actionKey(instanceID, value.Instance.CurrentNodeID))
	if err != nil {
		return workflowv1.ActionWork{}, workflowv1.ActionDescriptor{}, workflowv1.ActionRequest{}, err
	}
	work, err := decodeDocument[workflowv1.ActionWork](workStored)
	if err != nil {
		return workflowv1.ActionWork{}, workflowv1.ActionDescriptor{}, workflowv1.ActionRequest{}, err
	}
	for _, action := range value.Feature.Actions {
		if action.ID == work.ActionID {
			request := workflowv1.ActionRequest{InstanceID: instanceID, Definition: value.Instance.Definition, FeatureID: value.Instance.FeatureID, ActionID: action.ID, Resource: value.Instance.Resource, ResourceDigest: value.Instance.ResourceDigest, Attempt: work.Attempt, IdempotencyKey: work.IdempotencyKey}
			work.Revision = workStored.Revision
			return work, action, request, nil
		}
	}
	return workflowv1.ActionWork{}, workflowv1.ActionDescriptor{}, workflowv1.ActionRequest{}, ErrInvalidState
}

func (s *Service) CompleteAction(ctx context.Context, repository Repository, request workflowv1.CompleteActionRequest) (workflowv1.Instance, error) {
	var instanceID string
	err := repository.UnitOfWork(ctx, func(tx Repository) error {
		record, err := tx.Get(ctx, request.WorkID)
		if err != nil || record.Kind != kindAction || record.Revision != request.ExpectedRevision {
			return ErrConflict
		}
		work, err := decodeDocument[workflowv1.ActionWork](record)
		if err != nil {
			return err
		}
		if work.Status != workflowv1.WorkPending {
			return ErrConflict
		}
		instanceID = work.InstanceID
		instanceStored, err := tx.Get(ctx, instanceKey(instanceID))
		if err != nil {
			return err
		}
		value, err := decodeDocument[instanceRecord](instanceStored)
		if err != nil || value.Instance.CurrentNodeID != work.NodeID {
			return ErrConflict
		}
		now := s.now().UTC()
		if !request.Succeeded {
			value.Instance.Status, value.Instance.UpdatedAt = workflowv1.InstanceSuspended, now
			value.Instance.Audit = append(value.Instance.Audit, workflowv1.AuditEvent{Action: "action.failed", NodeID: work.NodeID, ActorID: PluginID, Outcome: request.ErrorCode, At: now})
		} else if value.Instance.Mode == workflowv1.ExecutionDirect {
			value.Instance.Status, value.Instance.CurrentNodeID, value.Instance.UpdatedAt = workflowv1.InstanceSucceeded, "", now
			value.Instance.Audit = append(value.Instance.Audit,
				workflowv1.AuditEvent{Action: "action.completed", NodeID: work.NodeID, ActorID: PluginID, At: now},
				workflowv1.AuditEvent{Action: "completed", NodeID: work.NodeID, ActorID: PluginID, Outcome: string(workflowv1.ResultSucceeded), At: now},
			)
		} else {
			definition, findErr := s.definition(ctx, tx, value.Instance.Definition)
			if findErr != nil {
				return findErr
			}
			value.Instance.CurrentNodeID, value.Instance.UpdatedAt = findNode(definition.Definition, work.NodeID).Next, now
			value.Instance.Audit = append(value.Instance.Audit, workflowv1.AuditEvent{Action: "action.completed", NodeID: work.NodeID, ActorID: PluginID, At: now})
		}
		work.Status, work.CompletedAt, work.Result = workflowv1.WorkCompleted, &now, request.Result
		record.Status, record.Document = string(work.Status), mustDocument(work)
		if _, err = tx.Update(ctx, record, record.Revision, fmt.Sprintf("action:%s:%d", work.ID, record.Revision)); err != nil {
			return err
		}
		instanceStored.Status, instanceStored.Document = string(value.Instance.Status), mustDocument(value)
		updated, err := tx.Update(ctx, instanceStored, instanceStored.Revision, fmt.Sprintf("action-advance:%s:%d", instanceID, instanceStored.Revision))
		if err != nil {
			return err
		}
		value.Instance.Revision = updated.Revision
		if request.Succeeded && value.Instance.Mode == workflowv1.ExecutionWorkflow {
			definition, _ := s.definition(ctx, tx, value.Instance.Definition)
			return s.advance(ctx, tx, value, definition.Definition, PluginID)
		}
		return nil
	})
	if err != nil {
		return workflowv1.Instance{}, err
	}
	return s.GetInstance(ctx, repository, instanceID)
}

func mustDocument(value any) []byte { raw, _ := encodeDocument(value); return raw }
