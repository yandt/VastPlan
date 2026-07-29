package recoverycontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/nats-io/nats.go/jetstream"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

const maxClusterNodes = 4096

func (c *Controller) clusterReports(ctx context.Context) ([]recoveryv1.NodeReport, error) {
	if c.Nodes == nil || c.Verify == nil {
		return nil, nil
	}
	keys, err := c.Nodes.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("列出 Recovery 节点租约: %w", err)
	}
	if len(keys) > maxClusterNodes {
		return nil, errors.New("Recovery 节点租约数量超限")
	}
	reports := make([]recoveryv1.NodeReport, 0, len(keys))
	seenNodes := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		entry, err := c.Nodes.Get(ctx, key)
		if err != nil {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(entry.Value()))
		decoder.DisallowUnknownFields()
		var record controlplane.NodeRecord
		if decoder.Decode(&record) != nil {
			continue
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			continue
		}
		if record.TenantID != c.TenantID || record.Deployment != c.Deployment || record.Recovery == nil ||
			key != controlplane.NodeKey(record.TenantID, record.Deployment, record.NodeID) {
			continue
		}
		if err := c.Verify(record); err != nil {
			continue
		}
		if _, duplicate := seenNodes[record.NodeID]; duplicate {
			continue
		}
		seenNodes[record.NodeID] = struct{}{}
		reports = append(reports, recoveryv1.CloneNodeReport(*record.Recovery))
	}
	return reports, nil
}
