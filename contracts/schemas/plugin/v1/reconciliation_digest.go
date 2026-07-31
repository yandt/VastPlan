package pluginv1

func activationSelectionDigest(selection ActivationSelection) (string, error) {
	payload := struct {
		SchemaVersion      int                      `json:"schemaVersion"`
		PolicyID           string                   `json:"policyId"`
		Target             string                   `json:"target"`
		Generation         uint64                   `json:"generation"`
		InventoryDigest    string                   `json:"inventoryDigest"`
		ContributionDigest string                   `json:"contributionDigest"`
		Artifacts          []PluginArtifactIdentity `json:"artifacts"`
	}{selection.SchemaVersion, selection.PolicyID, selection.Target, selection.Generation, selection.InventoryDigest, selection.ContributionDigest, selection.Artifacts}
	return jsonDigest(payload)
}

func reconciliationPlanDigest(plan ReconciliationPlan) (string, error) {
	payload := struct {
		SchemaVersion      int                    `json:"schemaVersion"`
		Target             string                 `json:"target"`
		Generation         uint64                 `json:"generation"`
		SelectionDigest    string                 `json:"selectionDigest"`
		ContributionDigest string                 `json:"contributionDigest"`
		Actions            []ReconciliationAction `json:"actions"`
	}{plan.SchemaVersion, plan.Target, plan.Generation, plan.SelectionDigest, plan.ContributionDigest, plan.Actions}
	return jsonDigest(payload)
}
