package credentials

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

func (s *Service) Put(ctx context.Context, call *contractv1.CallContext, name, value string) (Record, error) {
	if err := validName(name); err != nil {
		return Record{}, err
	}
	t, err := tenant(call)
	if err != nil {
		return Record{}, err
	}
	if value == "" {
		return Record{}, errors.New("凭证 value 不能为空")
	}
	cipher, err := s.transit.Encrypt(ctx, []byte(value))
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records := s.records(t)
	now := time.Now().UTC()
	old, ok := records[name]
	version := old.Version + 1
	if !ok {
		version = 1
	}
	r := Record{Name: name, Version: version, KeyVersion: transitVersion(cipher), CreatedAt: now, UpdatedAt: now, Ciphertext: cipher}
	if ok {
		r.CreatedAt = old.CreatedAt
	}
	records[name] = r
	if err := s.save(); err != nil {
		return Record{}, err
	}
	return r, nil
}
func (s *Service) Describe(call *contractv1.CallContext, name string) (Record, error) {
	if err := validName(name); err != nil {
		return Record{}, err
	}
	t, err := tenant(call)
	if err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records(t)[name]
	if !ok {
		return Record{}, os.ErrNotExist
	}
	return r, nil
}
func (s *Service) List(call *contractv1.CallContext, prefix string) ([]Record, error) {
	t, err := tenant(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Record{}
	for _, r := range s.records(t) {
		if strings.HasPrefix(r.Name, prefix) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s *Service) Rotate(ctx context.Context, call *contractv1.CallContext, name string) (Record, error) {
	r, err := s.Describe(call, name)
	if err != nil {
		return Record{}, err
	}
	if r.Revoked {
		return Record{}, errors.New("已撤销的凭证不能轮换")
	}
	cipher, err := s.transit.Rewrap(ctx, r.Ciphertext)
	if err != nil {
		return Record{}, err
	}
	t, _ := tenant(call)
	s.mu.Lock()
	defer s.mu.Unlock()
	r = s.records(t)[name]
	r.Version++
	r.KeyVersion = transitVersion(cipher)
	r.Ciphertext = cipher
	r.UpdatedAt = time.Now().UTC()
	s.records(t)[name] = r
	if err := s.save(); err != nil {
		return Record{}, err
	}
	return r, nil
}
func (s *Service) Revoke(call *contractv1.CallContext, name string) (Record, error) {
	r, err := s.Describe(call, name)
	if err != nil {
		return Record{}, err
	}
	t, _ := tenant(call)
	s.mu.Lock()
	defer s.mu.Unlock()
	r = s.records(t)[name]
	r.Revoked = true
	r.Version++
	r.UpdatedAt = time.Now().UTC()
	s.records(t)[name] = r
	if err := s.save(); err != nil {
		return Record{}, err
	}
	return r, nil
}
func transitVersion(cipher string) string {
	parts := strings.Split(cipher, ":")
	if len(parts) >= 2 && parts[0] == "vault" {
		return parts[1]
	}
	return "unknown"
}
