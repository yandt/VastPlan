package pluginv1

import (
	"fmt"
	"regexp"
	"sort"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
)

var apiArtifactDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

type APIContractCatalogSource struct {
	Manifest       Manifest
	ArtifactSHA256 string
}

// BuildAPIContractCatalog 只能接收调用方已经完成签名与制品摘要验证的 Manifest。
// 本函数负责把清单声明投影为确定性目录，并再次绑定贡献摘要。
func BuildAPIContractCatalog(generation uint64, sources []APIContractCatalogSource) (apiv1.ContractCatalog, error) {
	if generation == 0 {
		return apiv1.ContractCatalog{}, fmt.Errorf("API Contract Catalog generation 必须大于 0")
	}
	catalog := apiv1.ContractCatalog{SchemaVersion: apiv1.SchemaVersion, Generation: generation, Contracts: []apiv1.ResolvedContract{}, DataPlaneServices: []apiv1.ResolvedDataPlaneService{}}
	for _, source := range sources {
		if !apiArtifactDigest.MatchString(source.ArtifactSHA256) {
			return apiv1.ContractCatalog{}, fmt.Errorf("插件 %s 的制品摘要无效", source.Manifest.ID)
		}
		contributions, err := ManifestAPIContributions(source.Manifest)
		if err != nil {
			return apiv1.ContractCatalog{}, err
		}
		for _, contract := range contributions.Contracts {
			digest, err := apiv1.ContractDigest(contract)
			if err != nil {
				return apiv1.ContractCatalog{}, err
			}
			catalog.Contracts = append(catalog.Contracts, apiv1.ResolvedContract{
				Reference: apiv1.ContractReference{
					PluginID: source.Manifest.ID, ArtifactSHA256: source.ArtifactSHA256, ContributionID: contract.ID,
					ContractID: contract.ContractID, ContractVersion: contract.ContractVersion, ContractDigest: digest,
				},
				Contract: contract,
			})
		}
		for _, service := range contributions.DataPlaneServices {
			catalog.DataPlaneServices = append(catalog.DataPlaneServices, apiv1.ResolvedDataPlaneService{
				Reference: apiv1.DataPlaneServiceReference{PluginID: source.Manifest.ID, ArtifactSHA256: source.ArtifactSHA256, ContributionID: service.ID},
				Service:   service,
			})
		}
	}
	sort.Slice(catalog.Contracts, func(i, j int) bool {
		left, right := catalog.Contracts[i].Reference, catalog.Contracts[j].Reference
		if left.PluginID != right.PluginID {
			return left.PluginID < right.PluginID
		}
		if left.ContributionID != right.ContributionID {
			return left.ContributionID < right.ContributionID
		}
		return left.ArtifactSHA256 < right.ArtifactSHA256
	})
	sort.Slice(catalog.DataPlaneServices, func(i, j int) bool {
		left, right := catalog.DataPlaneServices[i].Reference, catalog.DataPlaneServices[j].Reference
		if left.PluginID != right.PluginID {
			return left.PluginID < right.PluginID
		}
		if left.ContributionID != right.ContributionID {
			return left.ContributionID < right.ContributionID
		}
		return left.ArtifactSHA256 < right.ArtifactSHA256
	})
	if err := apiv1.ValidateContractCatalog(catalog); err != nil {
		return apiv1.ContractCatalog{}, err
	}
	return catalog, nil
}
