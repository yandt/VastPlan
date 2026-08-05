package platformcontrol

import (
	"context"
	"errors"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	platformcontrolport "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
)

func (c *Controller) prepareCandidateSecret(ctx context.Context, request platformcontrolv1.ChangeRequest) (platformcontrolv1.Profile, platformcontrolport.SecretSource, PreparedSecret, error) {
	if request.SecretMaterial == "" {
		source, err := c.resolveSecret(request.Profile.SecretRef)
		return request.Profile, source, nil, err
	}
	material := []byte(request.SecretMaterial)
	defer clear(material)
	prepared, err := c.materials.Prepare(ctx, request.Profile.Generation, material)
	if err != nil {
		return platformcontrolv1.Profile{}, nil, nil, err
	}
	profile := request.Profile
	profile.SecretRef = prepared.Ref()
	if err := platformcontrolv1.ValidateProfile(profile); err != nil {
		_ = prepared.Rollback()
		return platformcontrolv1.Profile{}, nil, nil, err
	}
	return profile, prepared.Source(), prepared, nil
}

func (c *Controller) resolveSecret(ref platformcontrolv1.SecretRef) (platformcontrolport.SecretSource, error) {
	if c.resolve == nil {
		return nil, errors.New("Bootstrap external Secret Provider 不可用")
	}
	return c.resolve(ref)
}
