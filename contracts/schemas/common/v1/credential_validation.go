package commonv1

import (
	"errors"
	"regexp"
	"unicode/utf8"
)

var (
	managedCredentialHandle  = regexp.MustCompile(`^credential://managed/[A-Za-z0-9._~-]+$`)
	managedCredentialOwner   = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)+$`)
	managedCredentialPurpose = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
)

// ValidateManagedCredentialRef is the language-neutral safety baseline used by
// configuration, Material Lease and trusted data-plane adapters. It validates
// only the non-sensitive wire reference and never resolves credential material.
func ValidateManagedCredentialRef(ref ManagedCredentialRef) error {
	if !managedCredentialHandle.MatchString(ref.Handle) || utf8.RuneCountInString(ref.Handle) > 256 ||
		(ref.Scope != "tenant" && ref.Scope != "service") ||
		!managedCredentialOwner.MatchString(ref.Owner) || utf8.RuneCountInString(ref.Owner) > 160 ||
		!managedCredentialPurpose.MatchString(ref.Purpose) || utf8.RuneCountInString(ref.Purpose) > 160 ||
		ref.Version < 1 || (ref.Name != "" && utf8.RuneCountInString(ref.Name) > 160) {
		return errors.New("Managed CredentialRef 无效")
	}
	return nil
}
