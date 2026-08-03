package portalcomposer

import (
	"context"
	"fmt"

	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const composerStateFormat = "cn.vastplan.platform.portal-composer.shared"

// MigrateState authorizes the v4 to v5 transition. The data rewrite itself is
// performed by the shared-state repository on first CAS: retired Shell
// navigation fields are removed, frozen configuration digests are renewed and
// dataFormatVersion=5 prevents the compatibility path from running again.
func MigrateState(_ context.Context, _ sdk.MigrationPhase, request sdk.MigrationRequest) error {
	if request.From.Format != composerStateFormat || request.From.FormatVersion != 4 ||
		request.To.Format != composerStateFormat || request.To.FormatVersion != 5 {
		return fmt.Errorf("Portal Composer 不支持状态迁移 %s@%d -> %s@%d",
			request.From.Format, request.From.FormatVersion, request.To.Format, request.To.FormatVersion)
	}
	return nil
}
