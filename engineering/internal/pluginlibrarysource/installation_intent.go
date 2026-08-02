package pluginlibrarysource

import (
	"context"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

// InstallationIntent is the only output from source discovery that may alter a
// running development service. The source controller still owns build and
// repository publication; the trusted deployment adapter owns target binding,
// planning and activation.
type InstallationIntent struct {
	Action   plugininstallation.Action
	PluginID string
	Artifact *pluginv1.ArtifactRef
}

type InstallationIntentApplier interface {
	ApplyInstallationIntent(context.Context, InstallationIntent) error
}

func (i InstallationIntent) validate() error {
	if i.PluginID == "" {
		return plugininstallation.ErrInvalid
	}
	switch i.Action {
	case plugininstallation.ActionInstall, plugininstallation.ActionUpgrade:
		if i.Artifact == nil || i.Artifact.PluginID != i.PluginID || i.Artifact.Version == "" || i.Artifact.Channel != "workspace" {
			return plugininstallation.ErrInvalid
		}
	case plugininstallation.ActionRemove:
		if i.Artifact != nil {
			return plugininstallation.ErrInvalid
		}
	default:
		return plugininstallation.ErrInvalid
	}
	return nil
}
