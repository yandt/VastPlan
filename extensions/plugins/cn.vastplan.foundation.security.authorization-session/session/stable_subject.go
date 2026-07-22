package session

import authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"

const StableSubjectIssuer = authenticationv1.StableSubjectIssuer

func StableSubjectID(providerProfileID, issuer, subject string) string {
	return authenticationv1.StableSubjectID(providerProfileID, issuer, subject)
}
