package pluginv1

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const MaxPlanningArtifacts = 4096

// ArtifactPlanningRequest 请求仓库返回精确制品的已验签规划投影。
// 它不接受版本范围，避免描述查询与 Artifact Lock 发生二次选择。
type ArtifactPlanningRequest struct {
	Refs []ArtifactRef `json:"refs"`
}

type ArtifactPlanningDescriptor struct {
	Ref       ArtifactRef     `json:"ref"`
	SHA256    string          `json:"sha256"`
	Publisher string          `json:"publisher"`
	Manifest  json.RawMessage `json:"manifest"`
}

type ArtifactPlanningResponse struct {
	RepositoryRevision uint64                       `json:"repositoryRevision"`
	Items              []ArtifactPlanningDescriptor `json:"items"`
}

func ParseArtifactPlanningRequest(raw []byte) (ArtifactPlanningRequest, error) {
	var request ArtifactPlanningRequest
	if err := decodePlanningJSON(raw, &request); err != nil {
		return ArtifactPlanningRequest{}, fmt.Errorf("解析制品规划描述请求: %w", err)
	}
	if len(request.Refs) == 0 || len(request.Refs) > MaxPlanningArtifacts {
		return ArtifactPlanningRequest{}, fmt.Errorf("制品规划描述请求 refs 必须为 1..%d 项", MaxPlanningArtifacts)
	}
	seen := map[string]struct{}{}
	for _, ref := range request.Refs {
		if ref.PluginID == "" || ref.Version == "" || ref.Channel == "" {
			return ArtifactPlanningRequest{}, errors.New("制品规划描述请求包含不完整精确引用")
		}
		key := planningRefKey(ref)
		if _, duplicate := seen[key]; duplicate {
			return ArtifactPlanningRequest{}, fmt.Errorf("制品规划描述请求包含重复引用 %s", key)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(request.Refs, func(i, j int) bool { return planningRefKey(request.Refs[i]) < planningRefKey(request.Refs[j]) })
	return request, nil
}

func ValidateArtifactPlanningResponse(response ArtifactPlanningResponse) (ArtifactPlanningResponse, error) {
	if response.RepositoryRevision == 0 || len(response.Items) == 0 || len(response.Items) > MaxPlanningArtifacts {
		return ArtifactPlanningResponse{}, errors.New("制品规划描述响应身份无效")
	}
	seen := map[string]struct{}{}
	for index := range response.Items {
		item := &response.Items[index]
		_, digestErr := hex.DecodeString(item.SHA256)
		if item.Ref.PluginID == "" || item.Ref.Version == "" || item.Ref.Channel == "" || len(item.SHA256) != 64 || strings.ToLower(item.SHA256) != item.SHA256 || digestErr != nil || item.Publisher == "" || len(item.Manifest) == 0 {
			return ArtifactPlanningResponse{}, errors.New("制品规划描述响应包含不完整制品身份")
		}
		manifest, err := ParseManifest(item.Manifest)
		if err != nil {
			return ArtifactPlanningResponse{}, fmt.Errorf("制品规划描述 %s Manifest 无效: %w", item.Ref.PluginID, err)
		}
		if manifest.ID != item.Ref.PluginID || manifest.Version != item.Ref.Version || manifest.Publisher != item.Publisher {
			return ArtifactPlanningResponse{}, fmt.Errorf("制品规划描述 %s 身份与 Manifest 不一致", item.Ref.PluginID)
		}
		key := planningRefKey(item.Ref)
		if _, duplicate := seen[key]; duplicate {
			return ArtifactPlanningResponse{}, fmt.Errorf("制品规划描述响应包含重复引用 %s", key)
		}
		seen[key] = struct{}{}
	}
	sort.Slice(response.Items, func(i, j int) bool {
		return planningRefKey(response.Items[i].Ref) < planningRefKey(response.Items[j].Ref)
	})
	return response, nil
}

func decodePlanningJSON(raw []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("JSON 只能包含一个文档")
	}
	return nil
}

func planningRefKey(ref ArtifactRef) string {
	return ref.PluginID + "@" + ref.Version + "/" + ref.Channel
}
