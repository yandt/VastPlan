package plugininstallation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
)

// BuildSchemaImpact compares two trusted deployment catalogs. It is purely a
// governance projection: the Database Runtime repeats the plan against its
// durable migration ledger before any candidate route becomes visible.
func BuildSchemaImpact(current, target deploymentpublication.DataModelCatalog) SchemaImpact {
	impact := SchemaImpact{CurrentCatalogDigest: current.Digest, TargetCatalogDigest: target.Digest, RollbackMode: "generation"}
	currentByID := make(map[string]deploymentpublication.DataModelDescriptor, len(current.Models))
	for _, model := range current.Models {
		currentByID[model.Ref.ID] = model
	}
	migrations := make(map[string]deploymentpublication.DataMigrationDescriptor, len(target.Migrations))
	for _, migration := range target.Migrations {
		key := migration.Ref.ModelID + "/" + uintString(migration.Ref.FromVersion) + "/" + uintString(migration.Ref.ToVersion)
		migrations[key] = migration
	}
	for _, next := range target.Models {
		previous, exists := currentByID[next.Ref.ID]
		if exists && previous.Ref == next.Ref {
			delete(currentByID, next.Ref.ID)
			continue
		}
		var old *datamodelv1.Model
		change := SchemaChange{OwnerPluginID: next.OwnerPluginID, ModelID: next.Ref.ID, StorageKind: next.Model.Storage.Kind, Storage: next.Storage, To: next.Ref}
		if exists {
			old = &previous.Model
			change.FromVersion = previous.Ref.SchemaVersion
		}
		evolution := datamodelv1.ClassifyEvolution(old, next.Model)
		change.Kind, change.Reasons = string(evolution.Kind), append([]string(nil), evolution.Reasons...)
		if evolution.Kind == datamodelv1.EvolutionManual && exists {
			key := next.Ref.ID + "/" + uintString(previous.Ref.SchemaVersion) + "/" + uintString(next.Ref.SchemaVersion)
			if migration, ok := migrations[key]; ok && migration.OwnerPluginID == next.OwnerPluginID {
				change.Kind, change.MigrationID = "signed", migration.Ref.ID
				impact.RequiresBackup = true
				impact.RollbackMode = "forward-fix"
			}
		}
		impact.Changes = append(impact.Changes, change)
		delete(currentByID, next.Ref.ID)
	}
	// Removing a plugin never drops its tables automatically. Keeping the data
	// is an explicit impact but does not require a destructive migration.
	for _, previous := range currentByID {
		impact.Changes = append(impact.Changes, SchemaChange{OwnerPluginID: previous.OwnerPluginID, ModelID: previous.Ref.ID,
			StorageKind: previous.Model.Storage.Kind, Storage: previous.Storage, FromVersion: previous.Ref.SchemaVersion, To: previous.Ref, Kind: "retained",
			Reasons: []string{"目标版本不再声明该模型；数据表保留，需由独立退役流程处理"}})
	}
	sort.Slice(impact.Changes, func(i, j int) bool { return impact.Changes[i].ModelID < impact.Changes[j].ModelID })
	for _, change := range impact.Changes {
		if change.Kind != "retained" && change.Kind != "none" {
			impact.RequiresMigration = true
			impact.RequiresConfirmation = true
		}
	}
	impact.Digest = schemaImpactDigest(impact)
	return impact
}

func schemaImpactDigest(impact SchemaImpact) string {
	impact.Digest = ""
	raw, _ := json.Marshal(impact)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func uintString(value uint64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
