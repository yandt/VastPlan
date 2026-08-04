package platformcontrol

import (
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	platformcontrolport "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
)

type SecretResolver func(platformcontrolv1.SecretRef) (platformcontrolport.SecretSource, error)

func ResolveSecretSource(ref platformcontrolv1.SecretRef, credentialsDirectory string) (platformcontrolport.SecretSource, error) {
	return platformcontrolport.ResolveSecretSource(ref, credentialsDirectory)
}
