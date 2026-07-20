package pluginconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ManagedCredentialRef is the non-sensitive runtime projection of a credential
// entered through a plugin's own configuration form.
type ManagedCredentialRef struct {
	Handle  string `json:"handle"`
	Scope   string `json:"scope"`
	Owner   string `json:"owner"`
	Purpose string `json:"purpose"`
	Version int64  `json:"version"`
	Name    string `json:"name,omitempty"`
}

type CredentialSpec struct {
	ID       string
	Purpose  string
	Required bool
}

type WriteRequest struct {
	Values     json.RawMessage
	Secrets    map[string][]byte
	IfRevision *uint64
}

type Snapshot struct {
	ID          string                          `json:"id"`
	PluginID    string                          `json:"pluginId"`
	Revision    uint64                          `json:"revision"`
	Values      json.RawMessage                 `json:"values"`
	Credentials map[string]ManagedCredentialRef `json:"credentials,omitempty"`
	State       string                          `json:"state"`
}

type StagedCredential struct {
	ID  string
	Ref ManagedCredentialRef
}

// CredentialCustodian owns all plaintext handling. Stage must copy the bytes it
// needs before returning; Manager zeroes request buffers immediately afterward.
type CredentialCustodian interface {
	Stage(context.Context, string, string, string, []byte) (StagedCredential, error)
	Activate(context.Context, StagedCredential) error
	Abort(context.Context, StagedCredential) error
}

// CandidateStore persists a durable saga. Runtime readers only consume Active;
// Preparing/Failed candidates are never projected to a running plugin.
type CandidateStore interface {
	Active(context.Context, string) (Snapshot, bool, error)
	Prepare(context.Context, Snapshot, *uint64) (Snapshot, error)
	Activate(context.Context, string) (Snapshot, error)
	Fail(context.Context, string, string) error
}

type Manager struct {
	Credentials CredentialCustodian
	Store       CandidateStore
}

// Apply stages credentials, persists a non-runnable candidate, activates the
// credential versions and only then makes the candidate visible to runtime.
// This is a durable saga, not a false cross-service ACID promise. A committed
// credential left by a final store failure is unreachable and can be collected
// by the custodian using the candidate ID.
func (m Manager) Apply(ctx context.Context, pluginID string, specs []CredentialSpec, request WriteRequest) (Snapshot, error) {
	defer zeroSecrets(request.Secrets)
	if m.Credentials == nil || m.Store == nil || pluginID == "" || !json.Valid(request.Values) {
		return Snapshot{}, errors.New("插件配置管理器参数无效")
	}
	var values map[string]any
	if err := json.Unmarshal(request.Values, &values); err != nil || values == nil {
		return Snapshot{}, errors.New("插件配置 values 必须是 JSON 对象")
	}
	active, exists, err := m.Store.Active(ctx, pluginID)
	if err != nil {
		return Snapshot{}, err
	}
	refs := map[string]ManagedCredentialRef{}
	if exists {
		for id, ref := range active.Credentials {
			refs[id] = ref
		}
	}
	allowed := make(map[string]CredentialSpec, len(specs))
	for _, spec := range specs {
		if spec.ID == "" || spec.Purpose == "" {
			return Snapshot{}, errors.New("托管凭证声明缺少 id 或 purpose")
		}
		if _, duplicate := allowed[spec.ID]; duplicate {
			return Snapshot{}, fmt.Errorf("托管凭证声明重复: %q", spec.ID)
		}
		allowed[spec.ID] = spec
	}
	for id := range request.Secrets {
		if _, ok := allowed[id]; !ok {
			return Snapshot{}, fmt.Errorf("未声明的托管凭证输入: %q", id)
		}
	}
	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	staged := make([]StagedCredential, 0, len(request.Secrets))
	abort := func() {
		for _, credential := range staged {
			_ = m.Credentials.Abort(context.Background(), credential)
		}
	}
	for _, id := range ids {
		spec := allowed[id]
		secret, supplied := request.Secrets[id]
		if !supplied {
			if spec.Required {
				if _, retained := refs[id]; !retained {
					abort()
					return Snapshot{}, fmt.Errorf("缺少必填托管凭证 %q", id)
				}
			}
			continue
		}
		if len(secret) == 0 {
			abort()
			return Snapshot{}, fmt.Errorf("托管凭证 %q 不能为空", id)
		}
		credential, stageErr := m.Credentials.Stage(ctx, pluginID, id, spec.Purpose, secret)
		if stageErr != nil {
			abort()
			return Snapshot{}, stageErr
		}
		if credential.Ref.Owner != pluginID || credential.Ref.Purpose != spec.Purpose || credential.Ref.Handle == "" {
			staged = append(staged, credential)
			abort()
			return Snapshot{}, fmt.Errorf("凭证托管器返回了越权引用 %q", id)
		}
		staged = append(staged, credential)
		refs[id] = credential.Ref
	}
	candidate, err := m.Store.Prepare(ctx, Snapshot{PluginID: pluginID, Values: append(json.RawMessage(nil), request.Values...), Credentials: refs, State: "Preparing"}, request.IfRevision)
	if err != nil {
		abort()
		return Snapshot{}, err
	}
	for _, credential := range staged {
		if err := m.Credentials.Activate(ctx, credential); err != nil {
			_ = m.Store.Fail(context.Background(), candidate.ID, err.Error())
			abort()
			return Snapshot{}, err
		}
	}
	activated, err := m.Store.Activate(ctx, candidate.ID)
	if err != nil {
		_ = m.Store.Fail(context.Background(), candidate.ID, err.Error())
		return Snapshot{}, err
	}
	return activated, nil
}

func zeroSecrets(secrets map[string][]byte) {
	for _, secret := range secrets {
		for index := range secret {
			secret[index] = 0
		}
	}
}
