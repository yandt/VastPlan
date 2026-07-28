package frontendcompositionv1

import (
	"bytes"

	"github.com/santhosh-tekuri/jsonschema/v6"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func schemas() (*jsonschema.Schema, *jsonschema.Schema, *jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := commonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		if err := compositioncommonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		if err := apiv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		for _, resource := range []struct {
			url string
			raw []byte
		}{{UIContractSchemaURL, uiContractSchemaJSON}, {PlatformProfileSchemaURL, platformSchemaJSON}, {ApplicationCompositionSchemaURL, applicationSchemaJSON}, {PortalPlatformCatalogSchemaURL, portalPlatformCatalogSchemaJSON}} {
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(resource.raw))
			if err != nil {
				compileErr = err
				return
			}
			if err := compiler.AddResource(resource.url, doc); err != nil {
				compileErr = err
				return
			}
		}
		platformSchema, compileErr = compiler.Compile(PlatformProfileSchemaURL)
		if compileErr != nil {
			return
		}
		applicationSchema, compileErr = compiler.Compile(ApplicationCompositionSchemaURL)
		if compileErr != nil {
			return
		}
		portalPlatformCatalogSchema, compileErr = compiler.Compile(PortalPlatformCatalogSchemaURL)
	})
	return platformSchema, applicationSchema, portalPlatformCatalogSchema, compileErr
}
