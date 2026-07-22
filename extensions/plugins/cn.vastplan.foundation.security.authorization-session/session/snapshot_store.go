package session

import (
	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/authorizationtrust"
)

type SnapshotStore interface {
	Load() (authorizationv1.SignedPolicySnapshot, error)
}

type FileSnapshotStore = authorizationtrust.FileSnapshotStore
