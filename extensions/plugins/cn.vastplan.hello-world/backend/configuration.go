package main

import (
	"context"
	"errors"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	configurationscoped "cdsoft.com.cn/VastPlan/extensions/sdk/go/configurationscoped"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type greetingConfiguration struct {
	GreetingTemplate string `json:"greetingTemplate"`
}

func resolveGreetingConfiguration(ctx context.Context, host sdk.Host, call *contractv1.CallContext) (greetingConfiguration, error) {
	var configuration greetingConfiguration
	if _, err := configurationscoped.Resolve(ctx, host, call, &configuration); err != nil {
		return greetingConfiguration{}, err
	}
	if err := configuration.validate(); err != nil {
		return greetingConfiguration{}, err
	}
	return configuration, nil
}

func (c greetingConfiguration) validate() error {
	if len(c.GreetingTemplate) < 10 || len(c.GreetingTemplate) > 256 || !strings.Contains(c.GreetingTemplate, "{{name}}") {
		return errors.New("greetingTemplate 必须包含 {{name}} 且长度受限")
	}
	return nil
}

func (c greetingConfiguration) render(name string) string {
	return strings.ReplaceAll(c.GreetingTemplate, "{{name}}", name)
}
