// Package dataplane provides bounded clients for governed private data planes.
package dataplane

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
)

const privateTicketOperation = "issuePrivateDataPlaneTicket"

var ticketPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type Caller interface {
	Call(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)
}

type TicketRequest struct {
	DataPlaneExposureID string `json:"dataPlaneExposureId"`
	Method              string `json:"method"`
	Resource            string `json:"resource"`
	ContentSHA256       string `json:"contentSha256,omitempty"`
}

type Grant struct {
	Endpoint  string    `json:"endpoint"`
	LeaseID   string    `json:"leaseId"`
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func IssuePrivateGrant(ctx context.Context, caller Caller, call *contractv1.CallContext, request TicketRequest) (Grant, error) {
	if caller == nil || call == nil {
		return Grant{}, errors.New("Private Data Plane 缺少可信调用入口")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return Grant{}, err
	}
	operation := privateTicketOperation
	result, response, err := caller.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: "platform.api-exposure", Operation: &operation}, call, raw)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return Grant{}, errors.New("Private Data Plane 授权失败")
	}
	var grant Grant
	if json.Unmarshal(response, &grant) != nil || validateGrant(grant) != nil {
		return Grant{}, errors.New("Private Data Plane 返回无效授权")
	}
	return grant, nil
}

type Uploader struct{ client *http.Client }

func NewUploader(configuration *tls.Config) (*Uploader, error) {
	if configuration == nil || len(configuration.Certificates) == 0 || configuration.RootCAs == nil {
		return nil, errors.New("Private Data Plane 客户端必须配置 mTLS 身份和信任根")
	}
	secure := configuration.Clone()
	secure.MinVersion = tls.VersionTLS13
	return &Uploader{client: &http.Client{
		Transport:     &http.Transport{TLSClientConfig: secure, ForceAttemptHTTP2: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (u *Uploader) Upload(ctx context.Context, grant Grant, resource string, size int64, body io.Reader) (*http.Response, error) {
	if u == nil || u.client == nil || body == nil || size < 0 || validateGrant(grant) != nil {
		return nil, errors.New("Private Data Plane 上传参数无效")
	}
	endpoint, _ := url.Parse(grant.Endpoint)
	resourceURL, err := url.Parse(resource)
	if err != nil || resourceURL.IsAbs() || !strings.HasPrefix(resourceURL.Path, "/") || strings.HasPrefix(resourceURL.Path, "//") || resourceURL.RawQuery != "" || resourceURL.Fragment != "" {
		return nil, errors.New("Private Data Plane resource 无效")
	}
	endpoint.Path = resourceURL.Path
	query := endpoint.Query()
	query.Set("vp_ticket", grant.Ticket)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	request.ContentLength = size
	request.Header.Set("Content-Type", "application/octet-stream")
	response, err := u.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Private Data Plane 上传失败: %w", err)
	}
	return response, nil
}

func validateGrant(grant Grant) error {
	endpoint, err := url.Parse(grant.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || !ticketPattern.MatchString(grant.Ticket) || !grant.ExpiresAt.After(time.Now()) || grant.ExpiresAt.After(time.Now().Add(35*time.Second)) {
		return errors.New("Private Data Plane grant 无效")
	}
	return nil
}
