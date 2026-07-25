package enforcer

import (
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/authorizationeval"
)

type Evaluation = authorizationeval.Evaluation

func Evaluate(policy authorizationv1.AuthorizationIR, input authorizationv1.EvaluationInput, now time.Time) Evaluation {
	return authorizationeval.Evaluate(policy, input, now)
}
