package authorizationv1

const (
	MaxAuthorizationIRBytes    = 64 << 20
	MaxStoreDocumentBytes      = 16 << 20
	MaxExchangeDocumentBytes   = 4 << 20
	MaxAuditDetailsBytes       = 64 << 10
	MaxProviderMessageBytes    = 4 << 20
	MaxProviderDescriptorBytes = 128 << 10
)

func providerMessageLimit(protocol, operation string) int {
	if protocol == ProtocolEngine && operation == "prepare" {
		return MaxAuthorizationIRBytes
	}
	if protocol == ProtocolStore && (operation == "compareAndSwap" || operation == "load") {
		return MaxStoreDocumentBytes
	}
	if protocol == ProtocolExchange && operation == "export" {
		return MaxAuthorizationIRBytes
	}
	return MaxProviderMessageBytes
}
