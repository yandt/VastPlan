// Package provider implements the local-file artifact storage provisioner. It
// creates and validates private volumes but never serves artifact objects over
// the plugin bus; the repository accesses the provisioned mount directly.
package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactstorage"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID      = "cn.vastplan.platform.artifacts.storage.file"
	PluginVersion = "0.2.0"
	Capability    = "platform.artifacts.storage.file"
)

type Service struct {
	root        string
	migrationMu sync.Mutex
}

func New(root string) (*Service, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("file storage provider root 必须是规范绝对路径")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("创建 file storage provider root: %w", err)
	}
	if err := secureDirectory(root); err != nil {
		return nil, err
	}
	service := &Service{root: root}
	for _, directory := range []string{service.controlRoot(), service.migrationRoot(), service.quarantineRoot()} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, err
		}
		if err := secureDirectory(directory); err != nil {
			return nil, err
		}
	}
	return service, nil
}

func (s *Service) Probe(volumeID string) artifactstorage.ProbeResult {
	if err := artifactstorage.ValidateVolumeID(volumeID); err != nil {
		return artifactstorage.ProbeResult{Message: err.Error()}
	}
	if err := secureDirectory(s.root); err != nil {
		return artifactstorage.ProbeResult{Message: err.Error()}
	}
	return artifactstorage.ProbeResult{Ready: true}
}

func (s *Service) Provision(volumeID string) (artifactstorage.Volume, error) {
	if err := artifactstorage.ValidateVolumeID(volumeID); err != nil {
		return artifactstorage.Volume{}, err
	}
	if err := secureDirectory(s.root); err != nil {
		return artifactstorage.Volume{}, err
	}
	directory := s.volumePath(volumeID)
	if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return artifactstorage.Volume{}, err
	}
	if err := secureDirectory(directory); err != nil {
		return artifactstorage.Volume{}, fmt.Errorf("制品存储 volume: %w", err)
	}
	digest := sha256.Sum256([]byte(PluginID + "\x00" + directory))
	return artifactstorage.Volume{
		Handle: "artifact-storage://file/" + hex.EncodeToString(digest[:]), ProviderID: Capability,
		VolumeID: volumeID, AccessMode: "filesystem", MountPath: directory, Generation: 1, Ready: true,
	}, nil
}

func (s *Service) Describe(volumeID string) (artifactstorage.Volume, error) {
	if err := artifactstorage.ValidateVolumeID(volumeID); err != nil {
		return artifactstorage.Volume{}, err
	}
	directory := s.volumePath(volumeID)
	if err := secureDirectory(directory); err != nil {
		return artifactstorage.Volume{}, fmt.Errorf("制品存储 volume: %w", err)
	}
	digest := sha256.Sum256([]byte(PluginID + "\x00" + directory))
	return artifactstorage.Volume{Handle: "artifact-storage://file/" + hex.EncodeToString(digest[:]), ProviderID: Capability, VolumeID: volumeID, AccessMode: "filesystem", MountPath: directory, Generation: 1, Ready: true}, nil
}

func (s *Service) volumePath(volumeID string) string { return filepath.Join(s.root, volumeID) }
func (s *Service) controlRoot() string               { return filepath.Join(s.root, ".vastplan-provider") }

func secureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("file storage 目录必须是仅属主可访问且非符号链接的目录")
	}
	return nil
}

func (s *Service) handler(operation string) sdk.Handler {
	return func(ctx context.Context, _ sdk.Host, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		var output any
		var err error
		switch operation {
		case "probe":
			var request artifactstorage.ProbeRequest
			if err = json.Unmarshal(payload, &request); err == nil {
				output = s.Probe(request.VolumeID)
			}
		case "provision":
			var request artifactstorage.ProvisionRequest
			if err = json.Unmarshal(payload, &request); err == nil {
				output, err = s.Provision(request.VolumeID)
			}
		case "describe":
			var request artifactstorage.DescribeRequest
			if err = json.Unmarshal(payload, &request); err == nil {
				output, err = s.Describe(request.VolumeID)
			}
		case "migrate":
			var request artifactstorage.VolumeMigrationRequest
			if err = json.Unmarshal(payload, &request); err == nil {
				output, err = s.Migrate(ctx, request)
			}
		case "release":
			var request artifactstorage.VolumeReleaseRequest
			if err = json.Unmarshal(payload, &request); err == nil {
				output, err = s.Release(request)
			}
		default:
			err = fmt.Errorf("不支持的 file storage provider 操作 %q", operation)
		}
		if err != nil {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "platform.artifacts.storage.invalid", Message: err.Error()}}, nil, nil
		}
		raw, err := json.Marshal(output)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

func (s *Service) Contribution() sdk.Contribution {
	descriptor := []byte(`{"title":"本地文件制品存储 Provider","subcommands":[{"name":"probe","description":"检查本地文件存储根目录","paramsSchema":{"type":"object","properties":{"volumeId":{"type":"string"}},"required":["volumeId"]}},{"name":"provision","description":"幂等供给一个私有制品卷","paramsSchema":{"type":"object","properties":{"volumeId":{"type":"string"}},"required":["volumeId"]}},{"name":"describe","description":"读取受控 volume 身份"},{"name":"migrate","description":"可重试地准备、同步或校验 volume"},{"name":"release","description":"将旧 volume 原子移入私有隔离区"}]}`)
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: descriptor, Handlers: map[string]sdk.Handler{"probe": s.handler("probe"), "provision": s.handler("provision"), "describe": s.handler("describe"), "migrate": s.handler("migrate"), "release": s.handler("release")}}
}
