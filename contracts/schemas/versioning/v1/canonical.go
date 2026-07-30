package versioningv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	maxContentDepth = 64
	maxContentNodes = 10000
)

// CanonicalizeContent normalizes one bounded JSON object before hashing or
// persistence. Provider-specific encoders never define content identity.
func CanonicalizeContent(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > MaxContentBytes {
		return nil, fmt.Errorf("版本内容必须为 1-%d 字节", MaxContentBytes)
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("解析版本内容: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("版本内容只能包含一个 JSON 文档")
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("版本内容根必须是 JSON object")
	}
	nodes := 0
	if err := validateContentShape(root, 1, &nodes); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	if len(canonical) > MaxContentBytes {
		return nil, fmt.Errorf("规范化版本内容超过 %d 字节", MaxContentBytes)
	}
	return canonical, nil
}

func ContentDigest(raw json.RawMessage) (string, error) {
	canonical, err := CanonicalizeContent(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func validateContentShape(value any, depth int, nodes *int) error {
	*nodes = *nodes + 1
	if depth > maxContentDepth || *nodes > maxContentNodes {
		return errors.New("版本内容深度或节点数超过限制")
	}
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			if err := validateContentShape(child, depth+1, nodes); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateContentShape(child, depth+1, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}
