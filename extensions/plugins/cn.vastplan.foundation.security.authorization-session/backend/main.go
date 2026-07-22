// Package main starts the trusted Authorization Session resolver runtime.
package main

import (
	"log"
	"os"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/authorizationdirectory"
	session "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authorization-session/session"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	directory := session.GroupDirectory(session.EmptyGroupDirectory{})
	if path := strings.TrimSpace(os.Getenv("VASTPLAN_AUTHORIZATION_DIRECTORY_GROUPS")); path != "" {
		directory = authorizationdirectory.FileDirectory{Path: path}
	}
	resolver, err := session.NewResolverWithDirectory(session.FileSnapshotStore{SnapshotPath: os.Getenv("VASTPLAN_AUTHORIZATION_POLICY_SNAPSHOT"), TrustPath: os.Getenv("VASTPLAN_AUTHORIZATION_POLICY_TRUST")}, directory)
	if err != nil {
		log.Fatalf("初始化 Authorization Session: %v", err)
	}
	plugin := sdk.New(session.PluginID, session.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(resolver.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Authorization Session 退出: %v", err)
	}
}
