package plugindev

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Publisher interface {
	Publish(context.Context, Candidate) error
}

type CommandPublisher struct {
	RepositoryRoot string
	StateRoot      string
	StatusURL      string
	BackendTarget  string
	BackendBinding string
	GoCache        string
	Logf           func(string, ...any)
}

func (p CommandPublisher) Publish(ctx context.Context, candidate Candidate) error {
	arguments := []string{
		"run", "./engineering/tools/testpublish",
		"-package", candidate.PackageFile,
		"-channel", "workspace",
		"-state-root", p.StateRoot,
		"-status-url", p.StatusURL,
	}
	if strings.TrimSpace(p.BackendTarget) != "" {
		arguments = append(arguments, "-backend-target", p.BackendTarget)
	}
	if strings.TrimSpace(p.BackendBinding) != "" {
		arguments = append(arguments, "-backend-binding", p.BackendBinding)
	}
	command := exec.CommandContext(ctx, "go", arguments...)
	command.Dir = p.RepositoryRoot
	command.Env = append([]string(nil), os.Environ()...)
	if p.GoCache != "" {
		command.Env = append(command.Env, "GOCACHE="+p.GoCache)
	}
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("发布 workspace 候选: %w\n%s", err, strings.TrimSpace(output.String()))
	}
	if p.Logf != nil && strings.TrimSpace(output.String()) != "" {
		p.Logf("%s", strings.TrimSpace(output.String()))
	}
	return nil
}
