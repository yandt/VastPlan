package addressing

import "fmt"

func validateAllowedSystemCallers(identity TransportIdentity) error {
	seen := map[string]struct{}{}
	for _, callerID := range identity.AllowedSystemCallers {
		if !validTransportWorkloadID(callerID) {
			return fmt.Errorf("传输信任身份 %s 的 allowedSystemCallers 非法: %q", identity.Name, callerID)
		}
		if _, exists := seen[callerID]; exists {
			return fmt.Errorf("传输信任身份 %s 的 allowedSystemCallers 重复: %q", identity.Name, callerID)
		}
		seen[callerID] = struct{}{}
	}
	return nil
}

func validTransportWorkloadID(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for index, char := range value {
		alpha := char >= 'a' && char <= 'z'
		digit := char >= '0' && char <= '9'
		separator := char == '.' || char == '_' || char == ':' || char == '/' || char == '-'
		if !alpha && !digit && !separator || index == 0 && !alpha && !digit {
			return false
		}
	}
	return true
}

func containsExactIdentityValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
