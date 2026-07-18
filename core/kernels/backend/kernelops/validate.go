package kernelops

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
)

const (
	ConfigKindDesiredV1           = "desired-v1"
	ConfigKindPlatformV1          = "platform-profile-v1"
	ConfigKindApplicationV1       = "application-composition-v1"
	ConfigKindBackendCatalogV1    = "backend-platform-catalog-v1"
	ConfigKindPortalPlatformV1    = "portal-platform-profile-v1"
	ConfigKindPortalCatalogV1     = "portal-platform-catalog-v1"
	ConfigKindPortalApplicationV1 = "portal-application-composition-v1"
	ConfigKindDeploymentV2        = "deployment-v2"
	ConfigKindActualState         = "actual-state"
)

type validationResult struct {
	Kind          string `json:"kind"`
	SchemaVersion int    `json:"schema_version"`
	Revision      uint64 `json:"revision,omitempty"`
	Digest        string `json:"digest,omitempty"`
	Units         int    `json:"units"`
	NodeID        string `json:"node_id,omitempty"`
	Valid         bool   `json:"valid"`
}

// RunValidate 对即将上线的配置执行与运行时完全相同的 Schema 和语义校验。
// actual-state 只做读取迁移演练，不写回原文件。
func RunValidate(output io.Writer, args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	kind := flags.String("kind", "", "desired-v1、Backend/Portal 双输入、deployment-v2 或 actual-state")
	filename := flags.String("file", "", "待校验 JSON 文件")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *kind == "" || *filename == "" {
		return errors.New("用法: validate -kind <desired-v1|platform-profile-v1|application-composition-v1|backend-platform-catalog-v1|portal-platform-profile-v1|portal-platform-catalog-v1|portal-application-composition-v1|deployment-v2|actual-state> -file <配置.json>")
	}

	result := validationResult{Kind: *kind, Valid: true}
	switch *kind {
	case ConfigKindDesiredV1:
		state, err := deploymentv1.ParseFile(*filename)
		if err != nil {
			return err
		}
		result.SchemaVersion = state.Version
		result.Revision = state.Revision
		result.Digest = state.Digest()
		result.Units = len(state.Units)
	case ConfigKindDeploymentV2:
		deployment, err := deploymentv2.ParseFile(*filename)
		if err != nil {
			return err
		}
		result.SchemaVersion = deployment.Version
		result.Revision = deployment.Revision
		result.Digest = deployment.Digest()
		result.Units = len(deployment.Units)
	case ConfigKindPlatformV1:
		profile, err := backendcompositionv1.ParsePlatformProfileFile(*filename)
		if err != nil {
			return err
		}
		result.SchemaVersion = profile.Version
		result.Revision = profile.Revision
		result.Digest = profile.Digest()
		result.Units = len(profile.Services)
	case ConfigKindApplicationV1:
		composition, err := backendcompositionv1.ParseApplicationCompositionFile(*filename)
		if err != nil {
			return err
		}
		result.SchemaVersion = composition.Version
		result.Revision = composition.Revision
		result.Digest = composition.Digest()
		result.Units = len(composition.Units)
	case ConfigKindBackendCatalogV1:
		catalog, err := backendcompositionv1.ParseBackendPlatformCatalogFile(*filename)
		if err != nil {
			return err
		}
		result.SchemaVersion, result.Revision, result.Digest, result.Units = catalog.Version, catalog.Revision, catalog.Digest(), len(catalog.Bindings)
	case ConfigKindPortalPlatformV1:
		profile, err := frontendcompositionv1.ParsePlatformProfileFile(*filename)
		if err != nil {
			return err
		}
		result.SchemaVersion, result.Revision, result.Digest, result.Units = profile.Version, profile.Revision, profile.Digest(), len(profile.Plugins)
	case ConfigKindPortalCatalogV1:
		catalog, err := frontendcompositionv1.ParsePortalPlatformCatalogFile(*filename)
		if err != nil {
			return err
		}
		result.SchemaVersion, result.Revision, result.Digest, result.Units = catalog.Version, catalog.Revision, catalog.Digest(), len(catalog.Bindings)
	case ConfigKindPortalApplicationV1:
		composition, err := frontendcompositionv1.ParseApplicationCompositionFile(*filename)
		if err != nil {
			return err
		}
		result.SchemaVersion, result.Revision, result.Digest, result.Units = composition.Version, composition.Revision, composition.Digest(), len(composition.Plugins)
	case ConfigKindActualState:
		if err := checkRegularFile(*filename, maxActualStateBytes); err != nil {
			return fmt.Errorf("实际态文件: %w", err)
		}
		state, err := (nodeagent.FileStateStore{Path: *filename}).Load()
		if err != nil {
			return err
		}
		result.SchemaVersion = state.Version
		result.Revision = state.AppliedRevision
		result.Units = len(state.Units)
		result.NodeID = state.NodeID
	default:
		return fmt.Errorf("未知配置类型 %q", *kind)
	}

	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
