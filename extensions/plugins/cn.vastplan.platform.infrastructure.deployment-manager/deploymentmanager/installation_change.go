package deploymentmanager

import (
	"errors"
	"reflect"
	"strings"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginid"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

var errInstallationConflict = errors.New("插件安装变更与当前根插件状态冲突")

func applyInstallationChange(intent *backendcompositionv1.ApplicationIntent, request plugininstallation.PreviewRequest) error {
	var service *backendcompositionv1.ServiceIntent
	for i := range intent.Services {
		if intent.Services[i].ID == request.Target.UnitID {
			service = &intent.Services[i]
			break
		}
	}
	if service == nil || !strings.HasPrefix(service.ServiceClass, "application.") {
		return errNotFound
	}
	index := -1
	for i := range service.RootPlugins {
		if service.RootPlugins[i].PluginID == request.Change.PluginID {
			index = i
			break
		}
	}
	switch request.Change.Action {
	case plugininstallation.ActionInstall:
		if index >= 0 {
			return errInstallationConflict
		}
		service.RootPlugins = append(service.RootPlugins, *request.Change.Requirement)
	case plugininstallation.ActionUpgrade:
		if index < 0 {
			return errInstallationConflict
		}
		service.RootPlugins[index] = *request.Change.Requirement
	case plugininstallation.ActionRemove:
		if index < 0 {
			return errInstallationConflict
		}
		service.RootPlugins = append(service.RootPlugins[:index], service.RootPlugins[index+1:]...)
	default:
		return plugininstallation.ErrInvalid
	}
	return nil
}

func installationRootChanged(before, after backendcompositionv1.ApplicationIntent, unitID, pluginID string) bool {
	return !reflect.DeepEqual(findRootRequirement(before, unitID, pluginID), findRootRequirement(after, unitID, pluginID))
}

func findRootRequirement(intent backendcompositionv1.ApplicationIntent, unitID, pluginID string) *pluginv1.ArtifactRequirement {
	for _, service := range intent.Services {
		if service.ID != unitID {
			continue
		}
		for _, requirement := range service.RootPlugins {
			if requirement.PluginID == pluginID {
				value := requirement
				return &value
			}
		}
	}
	return nil
}

func requireApplicationManagedPlugin(pluginID string, source plugininstallation.Source, locks ...*pluginv1.ArtifactLock) error {
	found := false
	for _, lock := range locks {
		if lock == nil {
			continue
		}
		for _, item := range lock.Packages {
			if item.Ref.PluginID != pluginID {
				continue
			}
			class, err := pluginid.ClassifyManagement(pluginID, item.Publisher)
			if err != nil {
				return err
			}
			if class == pluginid.ManagementApplication || source == plugininstallation.SourceDevelopment && class == pluginid.ManagementDevelopment {
				found = true
				continue
			}
			return errors.New("服务安装意图只能选择应用插件")
		}
	}
	if found {
		return nil
	}
	return errors.New("安装预览缺少目标插件的精确制品身份")
}
