package assessmentprovider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

type PackageDownloader interface {
	Download(context.Context, artifactassessment.ScanLease) ([]byte, error)
}

type HTTPSDownloader struct{ client *http.Client }

func NewHTTPSDownloader() *HTTPSDownloader {
	return &HTTPSDownloader{client: &http.Client{
		Timeout:       5 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("安全评估下载禁止重定向") },
	}}
}

func (d *HTTPSDownloader) Download(ctx context.Context, lease artifactassessment.ScanLease) ([]byte, error) {
	if d == nil || d.client == nil || ctx == nil {
		return nil, errors.New("安全评估下载器未初始化")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, lease.URL, nil)
	if err != nil {
		return nil, errors.New("创建安全评估下载请求失败")
	}
	response, err := d.client.Do(request)
	if err != nil {
		return nil, errors.New("下载安全评估制品网络请求失败")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载安全评估制品返回 HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, artifactapi.DefaultMaxArtifactBytes+1))
	if err != nil || int64(len(raw)) > artifactapi.DefaultMaxArtifactBytes {
		return nil, errors.New("安全评估制品读取失败或超过大小上限")
	}
	return raw, nil
}
