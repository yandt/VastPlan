package enforcer

import (
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	"cdsoft.com.cn/VastPlan/extensions/sdk/go/authorizationnative"
)

type Evaluation = authorizationnative.Evaluation

func Evaluate(policy authorizationv1.AuthorizationIR, input authorizationv1.EvaluationInput, now time.Time) Evaluation {
	return authorizationnative.Evaluate(policy, input, now)
}
