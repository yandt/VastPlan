package pluginv1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func validateContextAccess(access *ContextAccess) error {
	if access == nil {
		return nil
	}
	seen := map[string]string{}
	for group, fields := range map[string][]string{"required": access.Required, "optional": access.Optional} {
		for _, field := range fields {
			if previous, exists := seen[field]; exists {
				return fmt.Errorf("contextAccess 字段 %q 同时出现在 %s 和 %s", field, previous, group)
			}
			seen[field] = group
		}
	}
	if len(access.Baggage) != 0 {
		if _, requested := seen["baggage"]; !requested {
			return fmt.Errorf("contextAccess.baggage 声明前缀时必须申请 baggage 字段")
		}
		for _, prefix := range access.Baggage {
			if strings.HasPrefix(prefix, "vastplan.internal.") || strings.HasPrefix(prefix, "vastplan.transport.") {
				return fmt.Errorf("contextAccess.baggage 不得申请宿主保留前缀 %q", prefix)
			}
		}
	}
	return nil
}

// ValidateDescriptor 校验插件通过协议总线注册的一条运行态 descriptor。
// 它把 extension point 和 descriptor 一起送入 Schema，避免只校验 JSON 语法而放过
// 诸如 hook phase 拼错这类会让分发语义失真的错误。
func ValidateDescriptor(extensionPoint string, raw []byte) error {
	if err := schemas(); err != nil {
		return err
	}
	var descriptor any
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		return fmt.Errorf("解析 %s descriptor JSON: %w", extensionPoint, err)
	}
	instance := map[string]any{"extensionPoint": extensionPoint, "descriptor": descriptor}
	if err := descriptorSch.Validate(instance); err != nil {
		return fmt.Errorf("%s descriptor 不符合 Schema: %w", extensionPoint, err)
	}
	return nil
}

// ValidateArtifactMetadata 校验制品索引 JSON；仓库发布和读取都调用它，避免索引
// 被手工写坏后仍被下游 reconcile 采用。
func ValidateArtifactMetadata(raw []byte) error {
	if err := schemas(); err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析制品元数据 JSON: %w", err)
	}
	if err := artifactSch.Validate(instance); err != nil {
		return fmt.Errorf("制品元数据不符合 Schema: %w", err)
	}
	return nil
}

// ValidateArtifactLock validates the immutable lock shared by Backend, Portal,
// Runner, Mobile and offline Bundle importers.
func ValidateArtifactLock(raw []byte) error {
	if err := schemas(); err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析制品锁 JSON: %w", err)
	}
	if err := artifactLockSch.Validate(instance); err != nil {
		return fmt.Errorf("制品锁不符合 Schema: %w", err)
	}
	return nil
}

// ValidateArtifactResolveRequest validates the cross-client resolver input.
func ValidateArtifactResolveRequest(raw []byte) error {
	if err := schemas(); err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析制品解析输入 JSON: %w", err)
	}
	if err := artifactResolveSch.Validate(instance); err != nil {
		return fmt.Errorf("制品解析输入不符合 Schema: %w", err)
	}
	return nil
}
