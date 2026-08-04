package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	"cdsoft.com.cn/VastPlan/engineering/internal/datamodelgen"
)

func main() {
	modelPath := flag.String("model", "", "data.model.v1 JSON path")
	language := flag.String("language", "", "go, typescript or python")
	outputDirectory := flag.String("out", "", "generated output directory")
	packageName := flag.String("package", "generated", "Go package or logical package name")
	flag.Parse()
	if *modelPath == "" || *language == "" || *outputDirectory == "" {
		fmt.Fprintln(os.Stderr, "用法: datamodelgen -model <file> -language <go|typescript|python> -out <directory> [-package name]")
		os.Exit(2)
	}
	raw, err := os.ReadFile(*modelPath)
	if err != nil {
		fatal(err)
	}
	model, err := datamodelv1.Parse(raw)
	if err != nil {
		fatal(err)
	}
	output, err := datamodelgen.Generate(model, datamodelgen.Language(*language), *packageName)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*outputDirectory, 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*outputDirectory, output.Filename), output.Content, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
