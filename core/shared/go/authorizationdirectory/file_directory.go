// Package authorizationdirectory loads trusted-host projections produced by
// authorization.directory.v1 providers. It never interprets an IdP claim as a
// permission: external groups only become inputs to published SubjectBindings.
package authorizationdirectory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

const maxProjectionBytes = 4 << 20

type Projection struct {
	Version  int                                        `json:"version"`
	Revision uint64                                     `json:"revision"`
	Subjects map[string][]authorizationv1.ExternalGroup `json:"subjects"`
}

type FileDirectory struct {
	Path string
}

func (d FileDirectory) Groups(subjectID string) ([]authorizationv1.ExternalGroup, uint64, error) {
	projection, err := LoadFile(d.Path)
	if err != nil {
		return nil, 0, err
	}
	groups := append([]authorizationv1.ExternalGroup(nil), projection.Subjects[subjectID]...)
	return groups, projection.Revision, nil
}

func LoadFile(path string) (Projection, error) {
	if err := securePath(path); err != nil {
		return Projection{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Projection{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxProjectionBytes+1))
	if err != nil {
		return Projection{}, err
	}
	if len(raw) > maxProjectionBytes {
		return Projection{}, errors.New("Authorization Directory 投影超过 4 MiB")
	}
	return Parse(raw)
}

func Parse(raw []byte) (Projection, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var projection Projection
	if err := decoder.Decode(&projection); err != nil {
		return Projection{}, fmt.Errorf("解析 Authorization Directory 投影: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Projection{}, err
	}
	if projection.Version != 1 || projection.Revision == 0 || projection.Subjects == nil {
		return Projection{}, errors.New("Authorization Directory 投影版本、revision 或 subjects 无效")
	}
	for subjectID, groups := range projection.Subjects {
		if strings.TrimSpace(subjectID) == "" || len(subjectID) > 512 || len(groups) > 512 {
			return Projection{}, errors.New("Authorization Directory 主体或组数量无效")
		}
		seen := map[string]struct{}{}
		for _, group := range groups {
			if strings.TrimSpace(group.ID) == "" || len(group.ID) > 512 || strings.TrimSpace(group.Issuer) == "" || len(group.Issuer) > 512 || len(group.DisplayName) > 512 {
				return Projection{}, errors.New("Authorization Directory 外部组无效")
			}
			key := group.Issuer + "\x00" + group.ID
			if _, exists := seen[key]; exists {
				return Projection{}, errors.New("Authorization Directory 外部组重复")
			}
			seen[key] = struct{}{}
		}
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].Issuer != groups[j].Issuer {
				return groups[i].Issuer < groups[j].Issuer
			}
			return groups[i].ID < groups[j].ID
		})
		projection.Subjects[subjectID] = groups
	}
	return projection, nil
}

func securePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Authorization Directory 文件必须是规范绝对路径")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("Authorization Directory 文件权限不安全")
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("Authorization Directory 投影只能包含一个 JSON document")
		}
		return err
	}
	return nil
}
