package backendcompositionv1

import (
	"bytes"
	_ "embed"
	"fmt"
	"sync"

	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed vastplan.application-intent.schema.json
var applicationIntentSchemaJSON []byte

//go:embed vastplan.resolution-report.schema.json
var resolutionReportSchemaJSON []byte

var (
	planningCompileOnce sync.Once
	intentSchema        *jsonschema.Schema
	reportSchema        *jsonschema.Schema
	planningCompileErr  error
)

func planningSchemas() (*jsonschema.Schema, *jsonschema.Schema, error) {
	planningCompileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := deploymentv2.AddResources(compiler); err != nil {
			planningCompileErr = err
			return
		}
		if err := pluginv1.AddArtifactSchemaResources(compiler); err != nil {
			planningCompileErr = err
			return
		}
		for _, resource := range []struct {
			url string
			raw []byte
		}{
			{ApplicationCompositionSchemaURL, applicationCompositionSchemaJSON},
			{ApplicationIntentSchemaURL, applicationIntentSchemaJSON},
			{ResolutionReportSchemaURL, resolutionReportSchemaJSON},
		} {
			document, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
			if err != nil {
				planningCompileErr = fmt.Errorf("解析 Backend planning Schema %s: %w", resource.url, err)
				return
			}
			if err := compiler.AddResource(resource.url, document); err != nil {
				planningCompileErr = fmt.Errorf("登记 Backend planning Schema %s: %w", resource.url, err)
				return
			}
		}
		intentSchema, planningCompileErr = compiler.Compile(ApplicationIntentSchemaURL)
		if planningCompileErr != nil {
			planningCompileErr = fmt.Errorf("编译 Backend Application Intent Schema: %w", planningCompileErr)
			return
		}
		reportSchema, planningCompileErr = compiler.Compile(ResolutionReportSchemaURL)
		if planningCompileErr != nil {
			planningCompileErr = fmt.Errorf("编译 Backend Resolution Report Schema: %w", planningCompileErr)
		}
	})
	return intentSchema, reportSchema, planningCompileErr
}
