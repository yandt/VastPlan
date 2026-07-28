// Command pluginrelease is the only supported entry point for synchronized
// Contract Registry and plugin release preparation. Production mode emits an
// approval plan and never publishes or activates artifacts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"cdsoft.com.cn/VastPlan/engineering/internal/releaseorchestrator"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "contracts":
		err = runContracts(os.Args[2:])
	case "plan":
		err = runPlan(os.Args[2:], false)
	case "prepare":
		err = runPlan(os.Args[2:], true)
	case "execute":
		err = runExecute(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "插件发布编排失败: %v\n", err)
		os.Exit(1)
	}
}

func runContracts(arguments []string) error {
	set := flag.NewFlagSet("contracts", flag.ContinueOnError)
	root := set.String("root", ".", "VastPlan 仓库根目录")
	write := set.Bool("write", false, "写入 Contract Registry 派生文件；默认只检查")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	changes, err := releaseorchestrator.SyncContracts(absolute, *write)
	if err != nil {
		return err
	}
	if !*write && len(changes) != 0 {
		return fmt.Errorf("Contract Registry 派生文件未同步: %v", changes)
	}
	if len(changes) == 0 {
		fmt.Println("Contract Registry 全链条已同步")
		return nil
	}
	for _, change := range changes {
		fmt.Printf("已同步 %s (%s)\n", change.Path, change.Reason)
	}
	return nil
}

func runPlan(arguments []string, prepare bool) error {
	set := flag.NewFlagSet("plan", flag.ContinueOnError)
	root := set.String("root", ".", "VastPlan 仓库根目录")
	specFile := set.String("file", "", "Release Spec YAML")
	out := set.String("out", "", "可选：写入确定性 Release Plan JSON")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if *specFile == "" {
		return errors.New("必须提供 -file")
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	specPath := *specFile
	if !filepath.IsAbs(specPath) {
		specPath = filepath.Join(absolute, specPath)
	}
	spec, err := releaseorchestrator.LoadReleaseSpec(specPath)
	if err != nil {
		return err
	}
	var plan releaseorchestrator.ReleasePlan
	if prepare {
		plan, err = releaseorchestrator.PrepareRelease(absolute, spec)
	} else {
		plan, err = releaseorchestrator.BuildReleasePlan(absolute, spec)
	}
	if err != nil {
		return err
	}
	if *out != "" {
		outPath := *out
		if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(absolute, outPath)
		}
		if err := releaseorchestrator.WriteReleasePlan(outPath, plan); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func runExecute(arguments []string) error {
	set := flag.NewFlagSet("execute", flag.ContinueOnError)
	root := set.String("root", ".", "VastPlan 仓库根目录")
	specFile := set.String("file", "", "Release Spec YAML")
	out := set.String("out", "", "可选：写入确定性 Release Plan JSON")
	stateRoot := set.String("state-root", ".vastplan/dev-platform", "本地开发状态根")
	statusURL := set.String("status-url", "http://127.0.0.1:18080/__vastplan_dev/status", "本地平台状态端点")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if *specFile == "" {
		return errors.New("必须提供 -file")
	}
	absolute, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	specPath := *specFile
	if !filepath.IsAbs(specPath) {
		specPath = filepath.Join(absolute, specPath)
	}
	spec, err := releaseorchestrator.LoadReleaseSpec(specPath)
	if err != nil {
		return err
	}
	if spec.Mode == releaseorchestrator.ReleaseModeProduction {
		plan, err := releaseorchestrator.PrepareRelease(absolute, spec)
		if err != nil {
			return err
		}
		if err := writeOptionalPlan(absolute, *out, plan); err != nil {
			return err
		}
		fmt.Println("生产发布计划已准备，未构建、上传、激活任何制品；请完成审批后交给正式发布控制器。")
		return printJSON(plan)
	}
	plan, results, err := releaseorchestrator.ExecuteDevelopmentRelease(context.Background(), absolute, spec, releaseorchestrator.DevelopmentExecutionOptions{
		StateRoot: *stateRoot, StatusURL: *statusURL, Logf: func(format string, values ...any) { fmt.Printf(format+"\n", values...) },
	})
	if err != nil {
		return err
	}
	if err := writeOptionalPlan(absolute, *out, plan); err != nil {
		return err
	}
	return printJSON(map[string]any{"plan": plan, "results": results})
}

func writeOptionalPlan(root, output string, plan releaseorchestrator.ReleasePlan) error {
	if output == "" {
		return nil
	}
	if !filepath.IsAbs(output) {
		output = filepath.Join(root, output)
	}
	return releaseorchestrator.WriteReleasePlan(output, plan)
}

func printJSON(value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: pluginrelease contracts [-write] | plan -file <release.yaml> | prepare -file <release.yaml> | execute -file <release.yaml>")
	os.Exit(2)
}
