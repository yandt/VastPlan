// pluginsbom derives a deterministic CycloneDX SBOM from plugin build facts.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"cdsoft.com.cn/VastPlan/engineering/internal/pluginsbom"
)

type stringList []string

func (values *stringList) String() string { return fmt.Sprint([]string(*values)) }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	root := flag.String("root", ".", "工作区根")
	pluginDir := flag.String("plugin-dir", "", "插件目录")
	output := flag.String("output", "", "CycloneDX JSON 输出")
	var goBinaries stringList
	var metafiles stringList
	flag.Var(&goBinaries, "go-binary", "实际 Go 可执行文件或 plugin .so；可重复")
	flag.Var(&metafiles, "metafile", "实际 esbuild metafile；可重复")
	flag.Parse()
	if *pluginDir == "" || *output == "" {
		flag.Usage()
		os.Exit(2)
	}
	result, err := pluginsbom.Generate(pluginsbom.Options{Root: *root, PluginDir: *pluginDir, GoBinaries: goBinaries, Metafiles: metafiles})
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*output, result.Raw, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("已生成插件 SBOM: %s components=%d\n", *output, result.Components)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
