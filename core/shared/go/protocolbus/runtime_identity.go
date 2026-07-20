package protocolbus

import (
	"context"
	"fmt"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
)

func launchRuntimeIdentity(policy LaunchPolicy) (runtimeidentity.Identity, error) {
	identity := runtimeidentity.Identity{
		PluginID: policy.PluginID, Publisher: policy.Publisher, Version: policy.Version,
		ArtifactSHA256: policy.ArtifactSHA256, NodeID: policy.NodeID,
		RuntimeScope: policy.RuntimeScope, InstanceID: policy.RuntimeInstanceID,
	}
	if err := identity.Validate(); err != nil {
		return runtimeidentity.Identity{}, err
	}
	return identity, nil
}

func runtimeAudienceEnvironment(policy LaunchPolicy) (string, error) {
	audience, err := runtimeAudience(policy)
	if err != nil {
		return "", err
	}
	return protocol.RuntimeAudienceEnvKey + "=" + audience, nil
}

func runtimeAudience(policy LaunchPolicy) (string, error) {
	identity, err := launchRuntimeIdentity(policy)
	if err != nil {
		return "", err
	}
	audience, err := identity.Audience()
	if err != nil {
		return "", err
	}
	return audience, nil
}

func withLaunchRuntimeIdentity(ctx context.Context, policy LaunchPolicy) (context.Context, error) {
	identity, err := launchRuntimeIdentity(policy)
	if err != nil {
		return nil, fmt.Errorf("宿主启动身份无效: %w", err)
	}
	return runtimeidentity.WithIdentity(ctx, identity)
}
