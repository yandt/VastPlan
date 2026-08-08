package authenticationv1

// Stable plugin and capability identities shared by trusted callers, policy
// enforcement, and the implementing plugins.
const (
	BrokerPluginID          = "cn.vastplan.foundation.security.authentication-broker"
	BrokerCapability        = "foundation.security.authentication.broker"
	OIDCProviderPluginID    = "cn.vastplan.foundation.security.authentication.provider.oidc"
	WebhookDeliveryPluginID = "cn.vastplan.platform.security.authentication.delivery.webhook"
)
