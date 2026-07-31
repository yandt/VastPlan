package main

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/engineering/internal/plugindev"
	"cdsoft.com.cn/VastPlan/engineering/internal/pluginlibrarysource"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository/localtest"
)

type localWorkspaceWithdrawer struct{ client *localtest.Client }

func (w localWorkspaceWithdrawer) WithdrawWorkspace(ctx context.Context, ref pluginv1.ArtifactRef) error {
	_, err := w.client.WithdrawWorkspace(ctx, ref)
	return err
}

func (w localWorkspaceWithdrawer) WorkspaceCandidates(ctx context.Context, pluginID string) ([]artifactrepositoryv1.Receipt, error) {
	snapshot, err := w.client.CatalogSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]artifactrepositoryv1.Receipt, 0)
	for _, receipt := range snapshot.Items {
		if receipt.Ref.PluginID == pluginID && receipt.Ref.Channel == "workspace" {
			result = append(result, receipt)
		}
	}
	return result, nil
}

func (r *runtime) startPluginLibrarySource(ctx context.Context) error {
	if r.repositoryProfile.Workspace == nil {
		return nil
	}
	raw, err := os.ReadFile(r.testingRepositoryTokenFile())
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(raw))
	client, err := localtest.NewClient(r.repositoryProfile, token)
	if err != nil {
		return err
	}
	controller := &pluginlibrarysource.Controller{
		RepositoryRoot: r.options.root, ScanInterval: time.Second, Debounce: 800 * time.Millisecond,
		Builder: plugindev.CommandBuilder{
			RepositoryRoot: r.options.root, StateRoot: r.options.stateRoot,
			GoCache: filepath.Join(r.options.stateRoot, "go-cache"),
		},
		Publisher: plugindev.CommandPublisher{
			RepositoryRoot: r.options.root, StateRoot: r.options.stateRoot,
			StatusURL: "http://" + r.options.listen + "/__vastplan_dev/status",
			GoCache:   filepath.Join(r.options.stateRoot, "go-cache"), Logf: log.Printf,
		},
		Withdrawer: localWorkspaceWithdrawer{client: client},
		Store:      pluginlibrarysource.FileStateStore{Path: filepath.Join(r.persistentStateRoot(), "plugin-library-source", "state.json")},
		Logf:       log.Printf,
	}
	go func() {
		defer client.CloseIdleConnections()
		if err := controller.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Local Plugin Library 源控制器退出: %v", err)
		}
	}()
	return nil
}
