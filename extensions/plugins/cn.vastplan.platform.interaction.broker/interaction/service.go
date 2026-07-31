// Package interaction implements the durable state and authorization boundary
// for cross-platform human interactions.
package interaction

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/interactionapi"
)

const (
	PluginID           = "cn.vastplan.platform.interaction.broker"
	PluginVersion      = "0.1.4"
	Capability         = interactionapi.Capability
	StateFileConfigKey = "platform.interaction-broker.stateFile"
)

var (
	ErrForbidden    = interactionapi.ErrForbidden
	ErrNotFound     = interactionapi.ErrNotFound
	ErrConflict     = interactionapi.ErrConflict
	ErrExpired      = interactionapi.ErrExpired
	ErrInvalidState = interactionapi.ErrInvalidState
)

type persistedState struct {
	Records map[string]storedRecord `json:"records"`
}

type storedRecord struct {
	interactionapi.Record
	RequestHash string `json:"requestHash"`
}

type Service struct {
	mu        sync.Mutex
	state     persistedState
	stateFile string
	now       func() time.Time
	watchers  map[string]map[chan struct{}]struct{}
}

func New(stateFile string) (*Service, error) {
	s := &Service{state: persistedState{Records: map[string]storedRecord{}}, now: time.Now, watchers: map[string]map[chan struct{}]struct{}{}}
	if strings.TrimSpace(stateFile) != "" {
		if err := s.configure(stateFile); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Service) configure(stateFile string) error {
	if strings.TrimSpace(stateFile) == "" {
		return errors.New("Interaction Broker stateFile 不能为空")
	}
	if s.stateFile != "" && s.stateFile != stateFile {
		return errors.New("Interaction Broker stateFile 不允许在运行中切换")
	}
	if s.stateFile != "" {
		return nil
	}
	s.stateFile = stateFile
	return s.load()
}

func (s *Service) load() error {
	raw, err := os.ReadFile(s.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 Interaction Broker 状态: %w", err)
	}
	if err := json.Unmarshal(raw, &s.state); err != nil {
		return fmt.Errorf("解析 Interaction Broker 状态: %w", err)
	}
	if s.state.Records == nil {
		s.state.Records = map[string]storedRecord{}
	}
	return nil
}

func (s *Service) save() error {
	if s.stateFile == "" {
		return errors.New("Interaction Broker 尚未配置状态文件")
	}
	raw, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.stateFile), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.stateFile), ".interaction-broker-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.stateFile)
}

func validSubject(subject interactionapi.Subject) bool {
	return strings.TrimSpace(subject.ID) != "" && strings.TrimSpace(subject.TenantID) != ""
}

func requestHash(request uiv1.InteractionRequest) (string, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
