package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
)

func runCapacity(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("capacity", flag.ContinueOnError)
	natsOptions := addNATSFlags(flags)
	failOn := flags.String("fail-on", "critical", "退出非零的最低级别: warning, critical, full, none")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if output == nil {
		return fmt.Errorf("capacity 输出不能为空")
	}
	threshold, err := parseCapacityLevel(*failOn)
	if err != nil {
		return err
	}
	nc, err := natsOptions.connect("vastplan-shared-state-capacity")
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	kv, err := js.KeyValue(ctx, controlplane.SharedStateBucket)
	if err != nil {
		return err
	}
	snapshot, err := sharedstate.InspectCapacity(ctx, kv)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(snapshot); err != nil {
		return err
	}
	if threshold != "none" && capacityRank(snapshot.Level) >= capacityRank(sharedstate.CapacityLevel(threshold)) {
		return fmt.Errorf("Shared State 容量达到 %s，告警阈值为 %s", snapshot.Level, threshold)
	}
	return nil
}

func parseCapacityLevel(value string) (string, error) {
	switch value {
	case "none", string(sharedstate.CapacityWarning), string(sharedstate.CapacityCritical), string(sharedstate.CapacityFull):
		return value, nil
	default:
		return "", fmt.Errorf("未知 capacity fail-on %q", value)
	}
}

func capacityRank(level sharedstate.CapacityLevel) int {
	switch level {
	case sharedstate.CapacityReady:
		return 0
	case sharedstate.CapacityWarning:
		return 1
	case sharedstate.CapacityCritical:
		return 2
	case sharedstate.CapacityFull:
		return 3
	default:
		return 4
	}
}
