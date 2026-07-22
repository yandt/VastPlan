package commonv1

// Ed25519Signature is the shared detached-signature envelope used by signed
// policy snapshots and authentication assertions. Domain packages remain
// responsible for payload canonicalization and trust verification.
type Ed25519Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Value     string `json:"value"`
}
