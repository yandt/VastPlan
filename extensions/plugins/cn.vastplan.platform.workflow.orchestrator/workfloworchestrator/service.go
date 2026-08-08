package workfloworchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
)

const (
	PluginID      = workflowv1.OrchestratorPluginID
	PluginVersion = "0.1.0"
	Capability    = workflowv1.OrchestrationCapability
)

type Actor struct {
	ID     string
	Roles  []string
	System bool
}

type featureRecord struct {
	Descriptor      workflowv1.FeatureDescriptor    `json:"descriptor"`
	Owner           pluginv1.PluginArtifactIdentity `json:"owner"`
	Generation      uint64                          `json:"generation"`
	InventoryDigest string                          `json:"inventoryDigest"`
}

type nodeTemplateRecord struct {
	Descriptor      workflowv1.NodeTemplateDescriptor `json:"descriptor"`
	Owner           pluginv1.PluginArtifactIdentity   `json:"owner"`
	Generation      uint64                            `json:"generation"`
	InventoryDigest string                            `json:"inventoryDigest"`
}

type nodeProviderRecord struct {
	Descriptor      workflowv1.NodeProviderDescriptor `json:"descriptor"`
	Owner           pluginv1.PluginArtifactIdentity   `json:"owner"`
	Generation      uint64                            `json:"generation"`
	InventoryDigest string                            `json:"inventoryDigest"`
}

type CatalogRegistration struct {
	Features  int `json:"features"`
	Templates int `json:"templates"`
	Providers int `json:"providers"`
}

type definitionRecord struct {
	Definition  workflowv1.Definition `json:"definition"`
	Digest      string                `json:"digest"`
	PublishedBy string                `json:"publishedBy"`
	PublishedAt time.Time             `json:"publishedAt"`
}

type instanceRecord struct {
	Instance workflowv1.Instance          `json:"instance"`
	Feature  workflowv1.FeatureDescriptor `json:"feature"`
	Facts    json.RawMessage              `json:"facts,omitempty"`
	StartKey string                       `json:"startKey"`
}

type Service struct{ now func() time.Time }

func New() *Service { return &Service{now: time.Now} }

func (s *Service) RegisterCatalog(ctx context.Context, repository Repository, actor Actor, index pluginv1.ContributionIndexSnapshot) (CatalogRegistration, error) {
	if !actor.System || strings.TrimSpace(actor.ID) == "" {
		return CatalogRegistration{}, ErrForbidden
	}
	if err := pluginv1.ValidateContributionIndex(index); err != nil {
		return CatalogRegistration{}, fmt.Errorf("invalid contribution index: %w", err)
	}
	result := CatalogRegistration{}
	for _, contribution := range index.Contributions {
		switch contribution.Kind {
		case workflowv1.FeatureContributionKind:
			var descriptor workflowv1.FeatureDescriptor
			if err := strictDecode(contribution.Descriptor, &descriptor); err != nil || descriptor.ID != contribution.ID || descriptor.Contract != contribution.Contract {
				return CatalogRegistration{}, fmt.Errorf("invalid workflow feature %q", contribution.ID)
			}
			if err := workflowv1.ValidateFeature(descriptor); err != nil {
				return CatalogRegistration{}, err
			}
			value := featureRecord{Descriptor: descriptor, Owner: contribution.Owner, Generation: index.Generation, InventoryDigest: index.InventoryDigest}
			if err := putCatalogRecord(ctx, repository, kindFeature, featureKey(descriptor.ID), descriptor.ID, value, index.Generation, index.InventoryDigest); err != nil {
				return CatalogRegistration{}, err
			}
			result.Features++
		case workflowv1.TemplateContributionKind:
			var descriptor workflowv1.NodeTemplateDescriptor
			if err := strictDecode(contribution.Descriptor, &descriptor); err != nil || descriptor.ID != contribution.ID || descriptor.Contract != contribution.Contract {
				return CatalogRegistration{}, fmt.Errorf("invalid workflow node template %q", contribution.ID)
			}
			if err := workflowv1.ValidateNodeTemplate(descriptor); err != nil {
				return CatalogRegistration{}, err
			}
			value := nodeTemplateRecord{Descriptor: descriptor, Owner: contribution.Owner, Generation: index.Generation, InventoryDigest: index.InventoryDigest}
			if err := putCatalogRecord(ctx, repository, kindNodeTemplate, nodeTemplateKey(descriptor.ID), descriptor.ID, value, index.Generation, index.InventoryDigest); err != nil {
				return CatalogRegistration{}, err
			}
			result.Templates++
		case workflowv1.ProviderContributionKind:
			var descriptor workflowv1.NodeProviderDescriptor
			if err := strictDecode(contribution.Descriptor, &descriptor); err != nil || descriptor.ID != contribution.ID || descriptor.Contract != contribution.Contract {
				return CatalogRegistration{}, fmt.Errorf("invalid workflow node provider %q", contribution.ID)
			}
			if err := workflowv1.ValidateNodeProvider(descriptor); err != nil {
				return CatalogRegistration{}, err
			}
			value := nodeProviderRecord{Descriptor: descriptor, Owner: contribution.Owner, Generation: index.Generation, InventoryDigest: index.InventoryDigest}
			if err := putCatalogRecord(ctx, repository, kindNodeProvider, nodeProviderKey(descriptor.ID), descriptor.ID, value, index.Generation, index.InventoryDigest); err != nil {
				return CatalogRegistration{}, err
			}
			result.Providers++
		}
	}
	return result, nil
}

func putCatalogRecord(ctx context.Context, repository Repository, kind recordKind, id, catalogID string, value any, generation uint64, inventoryDigest string) error {
	document, err := encodeDocument(value)
	if err != nil {
		return err
	}
	record, err := repository.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		_, err = repository.Create(ctx, storedRecord{ID: id, Kind: kind, FeatureID: catalogID, Document: document}, "register:"+id+":"+inventoryDigest)
		return err
	}
	if err != nil {
		return err
	}
	var current struct {
		Generation      uint64 `json:"generation"`
		InventoryDigest string `json:"inventoryDigest"`
	}
	if err := json.Unmarshal(record.Document, &current); err != nil {
		return err
	}
	if generation < current.Generation {
		return ErrConflict
	}
	if generation == current.Generation && inventoryDigest == current.InventoryDigest {
		return nil
	}
	record.Document = document
	_, err = repository.Update(ctx, record, record.Revision, "register:"+id+":"+inventoryDigest)
	return err
}

func (s *Service) PublishDefinition(ctx context.Context, repository Repository, actor Actor, definition workflowv1.Definition) (workflowv1.DefinitionRef, error) {
	if strings.TrimSpace(actor.ID) == "" {
		return workflowv1.DefinitionRef{}, ErrForbidden
	}
	feature, err := s.feature(ctx, repository, definition.FeatureID)
	if err != nil {
		return workflowv1.DefinitionRef{}, err
	}
	if err := workflowv1.ValidateDefinition(definition, feature.Descriptor); err != nil {
		return workflowv1.DefinitionRef{}, err
	}
	if definition.Revision > 1 {
		if _, err := repository.Get(ctx, definitionKey(definition.ID, definition.Revision-1)); err != nil {
			return workflowv1.DefinitionRef{}, fmt.Errorf("workflow revisions must be continuous: %w", err)
		}
	}
	digest, err := workflowv1.DefinitionDigest(definition)
	if err != nil {
		return workflowv1.DefinitionRef{}, err
	}
	value := definitionRecord{Definition: definition, Digest: digest, PublishedBy: actor.ID, PublishedAt: s.now().UTC()}
	document, _ := encodeDocument(value)
	_, err = repository.Create(ctx, storedRecord{ID: definitionKey(definition.ID, definition.Revision), Kind: kindDefinition, FeatureID: definition.FeatureID, Document: document}, "publish:"+definition.ID+":"+digest)
	if err != nil {
		return workflowv1.DefinitionRef{}, err
	}
	return workflowv1.DefinitionRef{ID: definition.ID, Revision: definition.Revision, Digest: digest}, nil
}

func (s *Service) BindDefinition(ctx context.Context, repository Repository, actor Actor, serviceID, featureID string, definition workflowv1.DefinitionRef, expectedRevision int64) (workflowv1.Binding, error) {
	if !boundedText(actor.ID, 160) || !boundedText(serviceID, 160) {
		return workflowv1.Binding{}, ErrForbidden
	}
	storedDefinition, err := s.definition(ctx, repository, definition)
	if err != nil || storedDefinition.Definition.FeatureID != featureID {
		return workflowv1.Binding{}, ErrInvalidState
	}
	now := s.now().UTC()
	id := bindingKey(serviceID, featureID)
	record, err := repository.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		if expectedRevision != 0 {
			return workflowv1.Binding{}, ErrConflict
		}
		binding := workflowv1.Binding{ServiceID: serviceID, FeatureID: featureID, Definition: definition, Revision: 1, UpdatedAt: now, UpdatedBy: actor.ID}
		document, _ := encodeDocument(binding)
		created, createErr := repository.Create(ctx, storedRecord{ID: id, Kind: kindBinding, ServiceID: serviceID, FeatureID: featureID, Document: document}, "bind:"+id+":1")
		if createErr != nil {
			return workflowv1.Binding{}, createErr
		}
		binding.Revision = created.Revision
		return binding, nil
	}
	if err != nil || record.Revision != expectedRevision {
		return workflowv1.Binding{}, ErrConflict
	}
	binding := workflowv1.Binding{ServiceID: serviceID, FeatureID: featureID, Definition: definition, Revision: record.Revision + 1, UpdatedAt: now, UpdatedBy: actor.ID}
	record.Document, _ = encodeDocument(binding)
	updated, err := repository.Update(ctx, record, record.Revision, fmt.Sprintf("bind:%s:%d", id, binding.Revision))
	if err != nil {
		return workflowv1.Binding{}, err
	}
	binding.Revision = updated.Revision
	return binding, nil
}

func (s *Service) Start(ctx context.Context, repository Repository, actor Actor, request workflowv1.StartRequest) (workflowv1.Instance, error) {
	if !boundedText(actor.ID, 160) || !boundedText(request.ID, 160) || !boundedText(request.ServiceID, 160) || !boundedText(request.Resource.ID, 256) || !boundedText(request.IdempotencyKey, 256) || request.FeatureID == "" {
		return workflowv1.Instance{}, ErrForbidden
	}
	feature, err := s.feature(ctx, repository, request.FeatureID)
	if err != nil {
		return workflowv1.Instance{}, err
	}
	if request.Resource.Kind != feature.Descriptor.ResourceKind || feature.Descriptor.DigestRequired && len(request.ResourceDigest) != 64 {
		return workflowv1.Instance{}, ErrInvalidState
	}
	id := instanceKey(request.ID)
	if existing, getErr := repository.Get(ctx, id); getErr == nil {
		value, decodeErr := decodeDocument[instanceRecord](existing)
		if decodeErr == nil && value.StartKey == request.IdempotencyKey {
			return value.Instance, nil
		}
		return workflowv1.Instance{}, ErrConflict
	} else if !errors.Is(getErr, ErrNotFound) {
		return workflowv1.Instance{}, getErr
	}
	bindingRecord, err := repository.Get(ctx, bindingKey(request.ServiceID, request.FeatureID))
	if errors.Is(err, ErrNotFound) && feature.Descriptor.UnboundPolicy == workflowv1.UnboundDirect {
		return s.startDirect(ctx, repository, actor, request, feature.Descriptor)
	}
	if err != nil {
		return workflowv1.Instance{}, err
	}
	binding, err := decodeDocument[workflowv1.Binding](bindingRecord)
	if err != nil {
		return workflowv1.Instance{}, err
	}
	definition, err := s.definition(ctx, repository, binding.Definition)
	if err != nil {
		return workflowv1.Instance{}, err
	}
	now := s.now().UTC()
	instance := workflowv1.Instance{ID: request.ID, ServiceID: request.ServiceID, FeatureID: request.FeatureID, Definition: binding.Definition, Resource: request.Resource, ResourceDigest: request.ResourceDigest, Mode: workflowv1.ExecutionWorkflow, Status: workflowv1.InstanceRunning, CurrentNodeID: definition.Definition.EntryNodeID, Revision: 1, StartedBy: actor.ID, StartedAt: now, UpdatedAt: now, Audit: []workflowv1.AuditEvent{{Action: "started", NodeID: definition.Definition.EntryNodeID, ActorID: actor.ID, Outcome: string(workflowv1.ExecutionWorkflow), At: now}}}
	err = repository.UnitOfWork(ctx, func(tx Repository) error {
		value := instanceRecord{Instance: instance, Feature: feature.Descriptor, Facts: slices.Clone(request.Facts), StartKey: request.IdempotencyKey}
		document, _ := encodeDocument(value)
		created, createErr := tx.Create(ctx, storedRecord{ID: id, Kind: kindInstance, ServiceID: request.ServiceID, FeatureID: request.FeatureID, Status: string(instance.Status), Document: document}, "start:"+request.IdempotencyKey)
		if createErr != nil {
			return createErr
		}
		value.Instance.Revision = created.Revision
		return s.advance(ctx, tx, value, definition.Definition, actor.ID)
	})
	if err != nil {
		return workflowv1.Instance{}, err
	}
	return s.GetInstance(ctx, repository, request.ID)
}

func (s *Service) startDirect(ctx context.Context, repository Repository, actor Actor, request workflowv1.StartRequest, feature workflowv1.FeatureDescriptor) (workflowv1.Instance, error) {
	action, ok := featureAction(feature, feature.UnboundActionID)
	if !ok || !action.Terminal {
		return workflowv1.Instance{}, ErrInvalidState
	}
	now := s.now().UTC()
	const directNodeID = "direct"
	instance := workflowv1.Instance{ID: request.ID, ServiceID: request.ServiceID, FeatureID: request.FeatureID, Resource: request.Resource, ResourceDigest: request.ResourceDigest, Mode: workflowv1.ExecutionDirect, Status: workflowv1.InstanceRunning, CurrentNodeID: directNodeID, Revision: 1, StartedBy: actor.ID, StartedAt: now, UpdatedAt: now, Audit: []workflowv1.AuditEvent{{Action: "started", NodeID: directNodeID, ActorID: actor.ID, Outcome: string(workflowv1.ExecutionDirect), At: now}}}
	err := repository.UnitOfWork(ctx, func(tx Repository) error {
		value := instanceRecord{Instance: instance, Feature: feature, Facts: slices.Clone(request.Facts), StartKey: request.IdempotencyKey}
		document, _ := encodeDocument(value)
		if _, err := tx.Create(ctx, storedRecord{ID: instanceKey(request.ID), Kind: kindInstance, ServiceID: request.ServiceID, FeatureID: request.FeatureID, Status: string(instance.Status), Document: document}, "start:"+request.IdempotencyKey); err != nil {
			return err
		}
		work := workflowv1.ActionWork{ID: actionKey(instance.ID, directNodeID), InstanceID: instance.ID, NodeID: directNodeID, ActionID: action.ID, Attempt: 1, IdempotencyKey: instance.ID + "/direct/1", Status: workflowv1.WorkPending, Revision: 1, CreatedAt: now}
		workDocument, _ := encodeDocument(work)
		_, err := tx.Create(ctx, storedRecord{ID: work.ID, Kind: kindAction, ServiceID: instance.ServiceID, FeatureID: instance.FeatureID, Status: string(work.Status), Document: workDocument}, "action:"+work.ID)
		return err
	})
	if err != nil {
		return workflowv1.Instance{}, err
	}
	return s.GetInstance(ctx, repository, request.ID)
}

func (s *Service) GetInstance(ctx context.Context, repository Repository, id string) (workflowv1.Instance, error) {
	record, err := repository.Get(ctx, instanceKey(id))
	if err != nil {
		return workflowv1.Instance{}, err
	}
	value, err := decodeDocument[instanceRecord](record)
	if err != nil {
		return workflowv1.Instance{}, err
	}
	value.Instance.Revision = record.Revision
	return value.Instance, nil
}

func (s *Service) ListTasks(ctx context.Context, repository Repository, actor Actor) ([]workflowv1.Task, error) {
	return s.listTasks(ctx, repository, actor, "")
}

func (s *Service) ListTasksForService(ctx context.Context, repository Repository, actor Actor, serviceID string) ([]workflowv1.Task, error) {
	if !boundedText(serviceID, 160) {
		return nil, ErrForbidden
	}
	return s.listTasks(ctx, repository, actor, serviceID)
}

func (s *Service) listTasks(ctx context.Context, repository Repository, actor Actor, serviceID string) ([]workflowv1.Task, error) {
	if actor.ID == "" {
		return nil, ErrForbidden
	}
	records, err := repository.List(ctx, kindTask, string(workflowv1.WorkPending))
	if err != nil {
		return nil, err
	}
	result := []workflowv1.Task{}
	for _, record := range records {
		if serviceID != "" && record.ServiceID != serviceID {
			continue
		}
		task, decodeErr := decodeDocument[workflowv1.Task](record)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if actor.System || len(task.Roles) == 0 || intersects(task.Roles, actor.Roles) {
			task.Revision = record.Revision
			result = append(result, task)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (s *Service) CompleteTask(ctx context.Context, repository Repository, actor Actor, request workflowv1.CompleteTaskRequest) (workflowv1.Instance, error) {
	if actor.ID == "" || request.TaskID == "" || request.ExpectedRevision < 1 {
		return workflowv1.Instance{}, ErrForbidden
	}
	var instanceID string
	err := repository.UnitOfWork(ctx, func(tx Repository) error {
		record, err := tx.Get(ctx, request.TaskID)
		if err != nil || record.Kind != kindTask || record.Revision != request.ExpectedRevision {
			return ErrConflict
		}
		task, err := decodeDocument[workflowv1.Task](record)
		if err != nil {
			return err
		}
		if task.Status != workflowv1.WorkPending || !slices.Contains(task.AllowedOutcomes, request.Outcome) || !actor.System && len(task.Roles) > 0 && !intersects(task.Roles, actor.Roles) {
			return ErrForbidden
		}
		instanceID = task.InstanceID
		instanceStored, err := tx.Get(ctx, instanceKey(instanceID))
		if err != nil {
			return err
		}
		value, err := decodeDocument[instanceRecord](instanceStored)
		if err != nil || value.Instance.CurrentNodeID != task.NodeID || value.Instance.Status != workflowv1.InstanceRunning {
			return ErrConflict
		}
		definition, err := s.definition(ctx, tx, value.Instance.Definition)
		if err != nil {
			return err
		}
		node := findNode(definition.Definition, task.NodeID)
		next, ok := node.Outcomes[request.Outcome]
		if !ok {
			return ErrInvalidState
		}
		now := s.now().UTC()
		task.Status, task.CompletedBy, task.CompletedOutcome, task.CompletedAt = workflowv1.WorkCompleted, actor.ID, request.Outcome, &now
		record.Status = string(task.Status)
		record.Document, _ = encodeDocument(task)
		if _, err = tx.Update(ctx, record, record.Revision, fmt.Sprintf("task:%s:%d", task.ID, record.Revision)); err != nil {
			return err
		}
		value.Instance.CurrentNodeID, value.Instance.UpdatedAt = next, now
		value.Instance.Audit = append(value.Instance.Audit, workflowv1.AuditEvent{Action: "task.completed", NodeID: task.NodeID, ActorID: actor.ID, Outcome: request.Outcome, At: now})
		instanceStored.Document, _ = encodeDocument(value)
		updated, err := tx.Update(ctx, instanceStored, instanceStored.Revision, fmt.Sprintf("advance:%s:%d", instanceID, instanceStored.Revision))
		if err != nil {
			return err
		}
		value.Instance.Revision = updated.Revision
		return s.advance(ctx, tx, value, definition.Definition, actor.ID)
	})
	if err != nil {
		return workflowv1.Instance{}, err
	}
	return s.GetInstance(ctx, repository, instanceID)
}

func (s *Service) Cancel(ctx context.Context, repository Repository, actor Actor, request workflowv1.CancelRequest) (workflowv1.Instance, error) {
	if !boundedText(actor.ID, 160) || !boundedText(request.InstanceID, 160) || request.ExpectedRevision < 1 || !boundedText(request.Reason, 512) {
		return workflowv1.Instance{}, ErrForbidden
	}
	err := repository.UnitOfWork(ctx, func(tx Repository) error {
		record, err := tx.Get(ctx, instanceKey(request.InstanceID))
		if err != nil || record.Revision != request.ExpectedRevision {
			return ErrConflict
		}
		value, err := decodeDocument[instanceRecord](record)
		if err != nil {
			return err
		}
		if value.Instance.Status != workflowv1.InstanceRunning && value.Instance.Status != workflowv1.InstanceSuspended {
			return ErrConflict
		}
		nodeID := value.Instance.CurrentNodeID
		if value.Instance.Status == workflowv1.InstanceRunning {
			if value.Instance.Mode == workflowv1.ExecutionDirect {
				if err := completeCancelledWork(ctx, tx, actionKey(value.Instance.ID, nodeID), actor.ID, s.now().UTC()); err != nil {
					return err
				}
			} else {
				definition, err := s.definition(ctx, tx, value.Instance.Definition)
				if err != nil {
					return err
				}
				node := findNode(definition.Definition, nodeID)
				if node.Type.ID == workflowv1.NodeManual {
					if err := completeCancelledWork(ctx, tx, taskKey(value.Instance.ID, node.ID), actor.ID, s.now().UTC()); err != nil {
						return err
					}
				} else if node.Type.ID == workflowv1.NodeAction {
					if err := completeCancelledWork(ctx, tx, actionKey(value.Instance.ID, node.ID), actor.ID, s.now().UTC()); err != nil {
						return err
					}
				}
			}
		}
		now := s.now().UTC()
		value.Instance.Status, value.Instance.CurrentNodeID, value.Instance.UpdatedAt = workflowv1.InstanceCancelled, "", now
		value.Instance.Audit = append(value.Instance.Audit, workflowv1.AuditEvent{Action: "cancelled", NodeID: nodeID, ActorID: actor.ID, Outcome: request.Reason, At: now})
		record.Status, record.Document = string(value.Instance.Status), mustDocument(value)
		_, err = tx.Update(ctx, record, record.Revision, fmt.Sprintf("cancel:%s:%d", value.Instance.ID, record.Revision))
		return err
	})
	if err != nil {
		return workflowv1.Instance{}, err
	}
	return s.GetInstance(ctx, repository, request.InstanceID)
}

func completeCancelledWork(ctx context.Context, repository Repository, id, actorID string, now time.Time) error {
	record, err := repository.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Status != string(workflowv1.WorkPending) {
		return ErrConflict
	}
	if record.Kind == kindTask {
		task, decodeErr := decodeDocument[workflowv1.Task](record)
		if decodeErr != nil {
			return decodeErr
		}
		task.Status, task.CompletedAt, task.CompletedBy, task.CompletedOutcome = workflowv1.WorkCompleted, &now, actorID, "cancelled"
		record.Document = mustDocument(task)
	} else {
		work, decodeErr := decodeDocument[workflowv1.ActionWork](record)
		if decodeErr != nil {
			return decodeErr
		}
		work.Status, work.CompletedAt = workflowv1.WorkCompleted, &now
		record.Document = mustDocument(work)
	}
	record.Status = string(workflowv1.WorkCompleted)
	_, err = repository.Update(ctx, record, record.Revision, fmt.Sprintf("cancel-work:%s:%d", id, record.Revision))
	return err
}

func (s *Service) feature(ctx context.Context, repository Repository, id string) (featureRecord, error) {
	record, err := repository.Get(ctx, featureKey(id))
	if err != nil {
		return featureRecord{}, err
	}
	return decodeDocument[featureRecord](record)
}

func (s *Service) definition(ctx context.Context, repository Repository, ref workflowv1.DefinitionRef) (definitionRecord, error) {
	record, err := repository.Get(ctx, definitionKey(ref.ID, ref.Revision))
	if err != nil {
		return definitionRecord{}, err
	}
	value, err := decodeDocument[definitionRecord](record)
	if err != nil || value.Digest != ref.Digest {
		return definitionRecord{}, ErrConflict
	}
	return value, nil
}

func strictDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("workflow descriptor must contain one JSON value")
	}
	return nil
}

func intersects(left, right []string) bool {
	for _, value := range left {
		if slices.Contains(right, value) {
			return true
		}
	}
	return false
}

func featureAction(feature workflowv1.FeatureDescriptor, id string) (workflowv1.ActionDescriptor, bool) {
	for _, action := range feature.Actions {
		if action.ID == id {
			return action, true
		}
	}
	return workflowv1.ActionDescriptor{}, false
}

func boundedText(value string, maximum int) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && len(trimmed) <= maximum
}

func featureKey(id string) string      { return "feature/" + id }
func nodeTemplateKey(id string) string { return "node-template/" + id }
func nodeProviderKey(id string) string { return "node-provider/" + id }
func definitionKey(id string, revision int64) string {
	return fmt.Sprintf("definition/%s/%020d", id, revision)
}
func bindingKey(serviceID, featureID string) string { return "binding/" + serviceID + "/" + featureID }
func instanceKey(id string) string                  { return "instance/" + id }
func taskKey(instanceID, nodeID string) string      { return "task/" + instanceID + "/" + nodeID }
func actionKey(instanceID, nodeID string) string    { return "action/" + instanceID + "/" + nodeID }
