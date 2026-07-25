package commonv1

// IsSHA256 reports whether value is exactly one lowercase SHA-256 digest.
func IsSHA256(value string) bool {
	return len(value) == 64 && isLowerHex(value)
}

// IsHex reports whether value contains exactly hexLength hexadecimal
// characters. Digits may use either case.
func IsHex(value string, hexLength int) bool {
	return hexLength > 0 && len(value) == hexLength && isHex(value)
}

// IsLowerHex reports whether value contains exactly hexLength lowercase
// hexadecimal characters.
func IsLowerHex(value string, hexLength int) bool {
	return hexLength > 0 && len(value) == hexLength && isLowerHex(value)
}

// IsPrefixedHex reports whether value contains prefix followed by exactly
// hexLength hexadecimal characters. Prefix comparison is case-sensitive;
// hexadecimal digits may use either case to preserve existing ID contracts.
func IsPrefixedHex(value, prefix string, hexLength int) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix && IsHex(value[len(prefix):], hexLength)
}

// IsPrefixedLowerHex reports whether value contains prefix followed by exactly
// hexLength lowercase hexadecimal characters.
func IsPrefixedLowerHex(value, prefix string, hexLength int) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix && IsLowerHex(value[len(prefix):], hexLength)
}

func isHex(value string) bool {
	for i := range value {
		character := value[i]
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}

func isLowerHex(value string) bool {
	for i := range value {
		character := value[i]
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
