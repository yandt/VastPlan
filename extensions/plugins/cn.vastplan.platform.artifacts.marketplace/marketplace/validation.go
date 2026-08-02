package marketplace

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginmarketplace"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)

func decodeStrict(raw []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("Marketplace 请求只能包含一个 JSON 文档")
	}
	return nil
}

func normalizeQuery(query platformadminapi.ArtifactCatalogQuery) (platformadminapi.ArtifactCatalogQuery, error) {
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 20
	}
	if query.Page < 1 || query.PageSize < 1 || query.PageSize > 100 {
		return query, errors.New("Marketplace 分页无效")
	}
	for _, value := range []string{query.PluginID, query.PluginPrefix, query.Namespace, query.Publisher, query.Version, query.Channel, query.Target, query.Lifecycle} {
		if len(value) > 255 || strings.ContainsAny(value, "\x00\r\n") {
			return query, errors.New("Marketplace 查询字段无效")
		}
	}
	return query, nil
}

func validateCatalogPage(page pluginmarketplace.CatalogPage, requested platformadminapi.ArtifactCatalogQuery) error {
	if page.Page != requested.Page || page.PageSize != requested.PageSize || page.Total < 0 || len(page.Items) > requested.PageSize {
		return errors.New("远端 Marketplace 分页响应无效")
	}
	for _, item := range page.Items {
		if item.Ref.PluginID == "" || item.Ref.Version == "" || item.Ref.Channel == "" || len(item.SHA256) != 64 || item.RepositoryRevision == 0 {
			return errors.New("远端 Marketplace 制品身份无效")
		}
	}
	return nil
}
