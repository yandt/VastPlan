package portalcomposer

import (
	"context"
	"fmt"

	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const composerStateFormat = "cn.vastplan.platform.portal-composer.shared"

// MigrateState authorizes the current state transition. The repository fills
// newly introduced maps during its first CAS rewrite.
func MigrateState(_ context.Context, _ sdk.MigrationPhase, request sdk.MigrationRequest) error {
	if request.From.Format != composerStateFormat || request.From.FormatVersion != 5 ||
		request.To.Format != composerStateFormat || request.To.FormatVersion != 6 {
		return fmt.Errorf("Portal Composer 不支持状态迁移 %s@%d -> %s@%d",
			request.From.Format, request.From.FormatVersion, request.To.Format, request.To.FormatVersion)
	}
	return nil
}
