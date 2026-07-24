package localtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

type Client struct {
	profile artifactrepositoryv1.Profile
	token   string
	http    *http.Client
}

func NewClient(profile artifactrepositoryv1.Profile, token string) (*Client, error) {
	profile, err := validateBinding(profile, token)
	if err != nil {
		return nil, err
	}
	path, err := socketPath(profile)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", path)
		},
		DisableCompression:    true,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	return &Client{
		profile: profile,
		token:   token,
		http: &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("local-test 协议禁止重定向")
		}},
	}, nil
}

func Factory(token string) artifactrepository.Factory {
	return func(profile artifactrepositoryv1.Profile) (artifactrepository.Adapter, error) {
		return NewClient(profile, token)
	}
}

func (c *Client) Profile() artifactrepositoryv1.Profile { return c.profile }

func (c *Client) ReadExact(ctx context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	if err := artifactrepositoryv1.ValidateRef(c.profile, ref); err != nil {
		return artifacttrust.Envelope{}, err
	}
	response, err := c.do(ctx, http.MethodGet, artifactPath(ref), "", nil)
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return artifacttrust.Envelope{}, fmt.Errorf("%w: %s@%s/%s", artifacttrust.ErrNotFound, ref.PluginID, ref.Version, ref.Channel)
	}
	if response.StatusCode != http.StatusOK {
		return artifacttrust.Envelope{}, responseError(response)
	}
	envelope, err := readEnvelope(io.LimitReader(response.Body, maxRequestBytes()+1), response.Header.Get("Content-Type"))
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	if err := validateEnvelopeForProfile(c.profile, envelope); err != nil || !sameRef(exactRef(envelope), ref) {
		return artifacttrust.Envelope{}, errors.New("local-test 响应制品与精确引用不匹配")
	}
	return envelope, nil
}

func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

func (c *Client) Publish(ctx context.Context, envelope artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error) {
	if err := validateEnvelopeForProfile(c.profile, envelope); err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	body, contentType := streamEnvelope(envelope)
	response, err := c.do(ctx, http.MethodPost, "/v1/artifacts", contentType, body)
	if err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return artifactrepositoryv1.Receipt{}, responseError(response)
	}
	var receipt artifactrepositoryv1.Receipt
	if err := decodeJSON(response.Body, &receipt); err != nil {
		return artifactrepositoryv1.Receipt{}, err
	}
	if err := artifactrepositoryv1.ValidateReceipt(c.profile, receipt); err != nil || !sameRef(receipt.Ref, exactRef(envelope)) || receipt.SHA256 != envelope.Artifact.SHA256 {
		return artifactrepositoryv1.Receipt{}, errors.New("local-test 发布回执与请求不匹配")
	}
	return receipt, nil
}

func streamEnvelope(envelope artifacttrust.Envelope) (io.ReadCloser, string) {
	reader, pipe := io.Pipe()
	writer := multipart.NewWriter(pipe)
	contentType := writer.FormDataContentType()
	go func() {
		err := writeEnvelope(writer, envelope)
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
		_ = pipe.CloseWithError(err)
	}()
	return reader, contentType
}

func (c *Client) CatalogSnapshot(ctx context.Context) (artifactrepositoryv1.CatalogSnapshot, error) {
	response, err := c.do(ctx, http.MethodGet, "/v1/catalog", "", nil)
	if err != nil {
		return artifactrepositoryv1.CatalogSnapshot{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return artifactrepositoryv1.CatalogSnapshot{}, responseError(response)
	}
	var snapshot artifactrepositoryv1.CatalogSnapshot
	if err := decodeJSON(response.Body, &snapshot); err != nil {
		return artifactrepositoryv1.CatalogSnapshot{}, err
	}
	if err := artifactrepositoryv1.ValidateCatalogSnapshot(c.profile, snapshot); err != nil {
		return artifactrepositoryv1.CatalogSnapshot{}, err
	}
	return snapshot, nil
}

func (c *Client) ExpireWorkspace(ctx context.Context) (artifactrepositoryv1.ExpireWorkspaceResult, error) {
	if c.profile.Workspace == nil {
		return artifactrepositoryv1.ExpireWorkspaceResult{}, errors.New("local-test Profile 未启用 workspace")
	}
	response, err := c.do(ctx, http.MethodPost, "/v1/workspace/expire", "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		return artifactrepositoryv1.ExpireWorkspaceResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return artifactrepositoryv1.ExpireWorkspaceResult{}, responseError(response)
	}
	var result artifactrepositoryv1.ExpireWorkspaceResult
	if err := decodeJSON(response.Body, &result); err != nil {
		return artifactrepositoryv1.ExpireWorkspaceResult{}, err
	}
	if err := artifactrepositoryv1.ValidateExpireWorkspaceResult(c.profile, result); err != nil {
		return artifactrepositoryv1.ExpireWorkspaceResult{}, err
	}
	return result, nil
}

func (c *Client) do(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, "http://local-test"+path, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set(ProtocolHeader, artifactrepositoryv1.ProtocolLocalTest)
	request.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return c.http.Do(request)
}

func decodeJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maxMetadataBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("local-test JSON 响应只能包含一个文档")
	}
	return nil
}

func responseError(response *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	return fmt.Errorf("local-test 仓库返回 HTTP %d: %s", response.StatusCode, string(bytes.TrimSpace(raw)))
}
