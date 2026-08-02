package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
)

const maxCatalogResponseBytes = int64(8 << 20)

type catalogClient interface {
	List(context.Context, SourceConfig, platformadminapi.ArtifactCatalogQuery, string) (platformadminapi.ArtifactCatalogPage, error)
}

type httpCatalogClient struct{ client *http.Client }

func newHTTPCatalogClient() catalogClient {
	return &httpCatalogClient{client: &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
}

func (c *httpCatalogClient) List(ctx context.Context, source SourceConfig, query platformadminapi.ArtifactCatalogQuery, token string) (platformadminapi.ArtifactCatalogPage, error) {
	values := url.Values{}
	put := func(key, value string) {
		if value != "" {
			values.Set(key, value)
		}
	}
	put("pluginId", query.PluginID)
	put("pluginPrefix", query.PluginPrefix)
	put("namespace", query.Namespace)
	put("publisher", query.Publisher)
	put("version", query.Version)
	put("channel", query.Channel)
	put("target", query.Target)
	put("lifecycle", query.Lifecycle)
	values.Set("page", strconv.Itoa(query.Page))
	values.Set("pageSize", strconv.Itoa(query.PageSize))
	requestCtx, cancel := context.WithTimeout(ctx, source.Timeout())
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(source.URL, "/")+"/v1/catalog/artifacts?"+values.Encode(), nil)
	if err != nil {
		return platformadminapi.ArtifactCatalogPage{}, err
	}
	request.Header.Set("Accept", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return platformadminapi.ArtifactCatalogPage{}, fmt.Errorf("查询 Marketplace %s: %w", source.ID, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return platformadminapi.ArtifactCatalogPage{}, fmt.Errorf("Marketplace %s 返回 HTTP %d", source.ID, response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxCatalogResponseBytes+1))
	var page platformadminapi.ArtifactCatalogPage
	if err := decoder.Decode(&page); err != nil {
		return page, fmt.Errorf("解码 Marketplace %s: %w", source.ID, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return page, errors.New("Marketplace 响应只能包含一个 JSON 文档")
	}
	return page, nil
}
