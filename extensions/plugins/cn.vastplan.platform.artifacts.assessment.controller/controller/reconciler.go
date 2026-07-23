package controller

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Controller struct {
	config   Config
	ports    Ports
	store    PlanStore
	now      func() time.Time
	statsMu  sync.RWMutex
	stats    Stats
	runMu    sync.Mutex
	cancelMu sync.Mutex
	cancel   context.CancelFunc
}

func New(config Config) (*Controller, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Controller{config: config, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (c *Controller) Bind(host sdk.Host) error {
	store, err := newSharedPlanStore(host)
	if err != nil {
		return err
	}
	c.ports, c.store = hostPorts{host: host}, store
	return nil
}

func (c *Controller) ReconcileOnce(ctx context.Context) (Stats, error) {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if c.ports == nil || c.store == nil {
		return Stats{}, errors.New("Assessment Controller ports/store 未绑定")
	}
	now := c.now().UTC()
	stats := Stats{LastRunAt: now}
	call := &contractv1.CallContext{TenantId: c.config.TenantID}
	providerStatus, err := c.ports.ProviderStatus(ctx, call)
	if err != nil {
		c.setStats(stats)
		return stats, err
	}
	for _, channel := range c.config.Channels {
		for pageNumber := 1; ; pageNumber++ {
			page, err := c.ports.ListCatalog(ctx, call, channel, pageNumber, c.config.PageSize)
			if err != nil {
				c.setStats(stats)
				return stats, err
			}
			if page.Revision > stats.CatalogRevision {
				stats.CatalogRevision = page.Revision
			}
			for _, entry := range page.Items {
				if entry.LifecycleStatus != "active" || entry.SBOM == nil || entry.SecurityAdmission == nil {
					continue
				}
				stats.Eligible++
				outcome := c.reconcileEntry(ctx, call, entry, providerStatus, now)
				switch outcome {
				case "deferred":
					stats.Deferred++
				case "succeeded":
					stats.Succeeded++
				case "conflict":
					stats.Conflicts++
				default:
					stats.Failed++
				}
			}
			if pageNumber*page.PageSize >= page.Total || len(page.Items) == 0 {
				break
			}
		}
	}
	c.setStats(stats)
	return stats, nil
}

func (c *Controller) reconcileEntry(ctx context.Context, call *contractv1.CallContext, entry platformadminapi.ArtifactCatalogEntry, providerStatus artifactassessment.ProviderRuntimeStatus, now time.Time) string {
	key := planKey(entry.Ref)
	plan, revision, exists, err := c.store.Load(ctx, call, key)
	if err != nil {
		return "failed"
	}
	fresh, err := planForEntry(entry, c.config, now)
	if err != nil {
		return "failed"
	}
	changed := false
	if !exists || plan.SubjectSHA256 != fresh.SubjectSHA256 || plan.SBOMSHA256 != fresh.SBOMSHA256 || plan.AdmissionSHA256 != fresh.AdmissionSHA256 {
		plan = fresh
		revision = 0
		changed = true
	} else {
		before := plan
		hadPending := len(plan.PendingRecord) != 0
		convergeCatalogHead(&plan, entry, c.config)
		if hadPending && len(plan.PendingRecord) == 0 && plan.DatabaseRevision == providerStatus.Scanner.DatabaseRevision {
			plan.AssessmentRevision = providerStatus.AssessmentRevision
		}
		changed = !reflect.DeepEqual(before, plan)
	}
	if len(plan.PendingRecord) == 0 && (plan.DatabaseRevision != providerStatus.Scanner.DatabaseRevision || plan.AssessmentRevision != providerStatus.AssessmentRevision) {
		plan.NextScanAt = now
		changed = true
	}
	if changed {
		plan.UpdatedAt = now
		if _, err := c.store.Save(ctx, call, key, plan, revision); err != nil {
			return "conflict"
		}
		// Reload revision after create/update so every subsequent mutation is CAS.
		plan, revision, _, err = c.store.Load(ctx, call, key)
		if err != nil {
			return "failed"
		}
	}
	if now.Before(plan.NextScanAt) {
		return "deferred"
	}
	if len(plan.PendingRecord) == 0 {
		request := artifactassessment.ProviderStatusRequest{ProviderAssessmentRequest: artifactassessment.ProviderAssessmentRequest{
			ScanLeaseRequest: artifactassessment.ScanLeaseRequest{Ref: plan.Ref, SubjectSHA256: plan.SubjectSHA256, SBOMSHA256: plan.SBOMSHA256}, PolicyID: plan.PolicyID,
		}, AdmissionSHA256: plan.AdmissionSHA256, Sequence: plan.LastSequence + 1, PreviousSHA256: plan.LastRecordSHA256}
		raw, err := c.ports.AssessStatus(ctx, call, request)
		if err != nil {
			return c.failPlan(ctx, call, key, plan, revision, now, "provider_unavailable")
		}
		plan.PendingRecord = append(json.RawMessage(nil), raw...)
		plan.UpdatedAt = now
		if _, err := c.store.Save(ctx, call, key, plan, revision); err != nil {
			return "conflict"
		}
		plan, revision, _, err = c.store.Load(ctx, call, key)
		if err != nil {
			return "failed"
		}
	}
	if err := c.ports.AppendStatus(ctx, call, artifactassessment.AppendStatusRequest{Ref: plan.Ref, Record: plan.PendingRecord}); err != nil {
		return c.failPlan(ctx, call, key, plan, revision, now, "append_failed")
	}
	status, digest, err := artifactassessment.InspectStatus(plan.PendingRecord)
	if err != nil {
		return c.failPlan(ctx, call, key, plan, revision, now, "pending_invalid")
	}
	plan.LastSequence, plan.LastRecordSHA256, plan.DatabaseRevision = status.Sequence, digest, status.Evaluation.Scanner.DatabaseRevision
	plan.AssessmentRevision = providerStatus.AssessmentRevision
	plan.NextScanAt = scheduledAt(plan.Ref, status.Evaluation.ExpiresAt, c.config.LeadTime(), c.config.Jitter())
	plan.Attempts, plan.LastErrorCode, plan.PendingRecord, plan.UpdatedAt = 0, "", nil, now
	if _, err := c.store.Save(ctx, call, key, plan, revision); err != nil {
		return "conflict"
	}
	return "succeeded"
}

func (c *Controller) failPlan(ctx context.Context, call *contractv1.CallContext, key string, plan Plan, revision uint64, now time.Time, code string) string {
	if plan.Attempts < 31 {
		plan.Attempts++
	}
	plan.LastErrorCode, plan.NextScanAt, plan.UpdatedAt = code, retryAt(plan.Ref, now, plan.Attempts, c.config.RetryBase(), c.config.RetryMax()), now
	if _, err := c.store.Save(ctx, call, key, plan, revision); err != nil {
		return "conflict"
	}
	return "failed"
}

func planForEntry(entry platformadminapi.ArtifactCatalogEntry, config Config, now time.Time) (Plan, error) {
	admission := entry.SecurityAdmission
	expiresAt, err := time.Parse(time.RFC3339Nano, admission.ExpiresAt)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{SchemaVersion: planSchemaVersion, Ref: entry.Ref, SubjectSHA256: entry.SHA256, SBOMSHA256: entry.SBOM.SHA256, AdmissionSHA256: admission.AdmissionSHA256, PolicyID: admission.PolicyID, LastRecordSHA256: admission.AdmissionSHA256, DatabaseRevision: admission.DatabaseRevision, NextScanAt: scheduledAt(entry.Ref, expiresAt.UTC(), config.LeadTime(), config.Jitter()), UpdatedAt: now}
	convergeCatalogHead(&plan, entry, config)
	return plan, plan.validate()
}

func convergeCatalogHead(plan *Plan, entry platformadminapi.ArtifactCatalogEntry, config Config) {
	status := entry.SecurityStatus
	if status == nil || status.Sequence < plan.LastSequence {
		return
	}
	if len(plan.PendingRecord) != 0 {
		pending, _, err := artifactassessment.InspectStatus(plan.PendingRecord)
		if err == nil && status.Sequence >= pending.Sequence {
			plan.PendingRecord = nil
		}
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, status.ExpiresAt)
	if err != nil {
		return
	}
	plan.LastSequence, plan.LastRecordSHA256, plan.DatabaseRevision = status.Sequence, status.RecordSHA256, status.DatabaseRevision
	plan.NextScanAt = scheduledAt(plan.Ref, expiresAt.UTC(), config.LeadTime(), config.Jitter())
	plan.Attempts, plan.LastErrorCode = 0, ""
}

func (c *Controller) setStats(value Stats) { c.statsMu.Lock(); c.stats = value; c.statsMu.Unlock() }
func (c *Controller) Stats() Stats         { c.statsMu.RLock(); defer c.statsMu.RUnlock(); return c.stats }
