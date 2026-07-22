package authenticationv1

const (
	OperationDescribe = "describe"
	OperationBegin    = "begin"
	OperationContinue = "continue"
	OperationResend   = "resend"
	OperationCancel   = "cancel"
	OperationHealth   = "health"
)

var protocolOperations = []string{
	OperationDescribe, OperationBegin, OperationContinue,
	OperationResend, OperationCancel, OperationHealth,
}

type DescribeRequest struct{}

type DescribeResult struct {
	Protocol string             `json:"protocol"`
	Methods  []MethodDescriptor `json:"methods"`
}

type BeginRequest struct {
	TransactionID       string `json:"transactionId"`
	MethodID            string `json:"methodId"`
	Audience            string `json:"audience"`
	TenantID            string `json:"tenantId"`
	PortalID            string `json:"portalId"`
	Locale              string `json:"locale"`
	ClientContextDigest string `json:"clientContextDigest"`
}

type BeginResult struct {
	Result MethodResult `json:"result"`
}

type ContinueRequest struct {
	TransactionID string          `json:"transactionId"`
	StepID        string          `json:"stepId"`
	Responses     []FieldResponse `json:"responses"`
}

type ContinueResult struct {
	Result MethodResult `json:"result"`
}

type ResendRequest struct {
	TransactionID string `json:"transactionId"`
	StepID        string `json:"stepId"`
}

type ResendResult struct {
	Result MethodResult `json:"result"`
}

type CancelRequest struct {
	TransactionID string `json:"transactionId"`
}

type CancelResult struct {
	Cancelled bool `json:"cancelled"`
}

type HealthRequest struct{}

type HealthResult struct {
	Ready      bool   `json:"ready"`
	ProviderID string `json:"providerId"`
}

func ProtocolOperations() []string {
	return append([]string(nil), protocolOperations...)
}
