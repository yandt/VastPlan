// Package configfile loads operator-authored configuration files into the
// canonical JSON representation consumed by VastPlan schemas and control-plane
// transports. JSON is the internal wire format; YAML is a local file ingress
// convenience only.
package configfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	maxFileBytes    = 4 << 20
	maxIncludedFile = 128
	maxIncludeDepth = 16
)

var jsonNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// Load reads a JSON, YAML, or YML configuration file and returns a canonical
// JSON document. YAML $include nodes may be nested, but every included file
// must remain under the directory containing the root file.
func Load(filename string) ([]byte, error) {
	root, err := resolveRoot(filename)
	if err != nil {
		return nil, err
	}
	loader := loader{root: filepath.Dir(root), seen: map[string]bool{}}
	value, err := loader.load(root, 0)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("编码规范 JSON 配置: %w", err)
	}
	return raw, nil
}

type loader struct {
	root  string
	seen  map[string]bool
	files int
}

func resolveRoot(filename string) (string, error) {
	if strings.TrimSpace(filename) == "" {
		return "", errors.New("配置文件不能为空")
	}
	abs, err := filepath.Abs(filename)
	if err != nil {
		return "", fmt.Errorf("解析配置文件路径: %w", err)
	}
	return resolveRegularFile(abs)
}

func (l *loader) load(filename string, depth int) (any, error) {
	if depth > maxIncludeDepth {
		return nil, fmt.Errorf("配置文件嵌套超过最大深度 %d", maxIncludeDepth)
	}
	if l.files >= maxIncludedFile {
		return nil, fmt.Errorf("配置文件数量超过上限 %d", maxIncludedFile)
	}
	if l.seen[filename] {
		return nil, fmt.Errorf("检测到循环配置引用: %s", filename)
	}
	l.seen[filename] = true
	defer delete(l.seen, filename)
	l.files++

	raw, err := readRegularFile(filename)
	if err != nil {
		return nil, err
	}
	var value any
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".yaml", ".yml":
		value, err = parseYAML(raw)
	case ".json":
		value, err = parseJSON(raw)
	default:
		return nil, fmt.Errorf("不支持的配置文件扩展名 %q；只允许 .json、.yaml 或 .yml", filepath.Ext(filename))
	}
	if err != nil {
		return nil, fmt.Errorf("解析配置文件 %s: %w", filename, err)
	}
	return l.expand(value, filepath.Dir(filename), depth)
}

func (l *loader) expand(value any, directory string, depth int) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 1 {
			if include, ok := typed["$include"]; ok {
				path, ok := include.(string)
				if !ok || strings.TrimSpace(path) == "" {
					return nil, errors.New("$include 必须是非空相对文件路径")
				}
				resolved, err := l.resolveInclude(directory, path)
				if err != nil {
					return nil, err
				}
				return l.load(resolved, depth+1)
			}
		}
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			expanded, err := l.expand(child, directory, depth)
			if err != nil {
				return nil, err
			}
			out[key] = expanded
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			expanded, err := l.expand(child, directory, depth)
			if err != nil {
				return nil, err
			}
			// An include used as a list item splices an included list, allowing
			// large unit/plugin arrays to be split into small files.
			if included, ok := expanded.([]any); ok {
				out = append(out, included...)
				continue
			}
			out = append(out, expanded)
		}
		return out, nil
	default:
		return value, nil
	}
}

func (l *loader) resolveInclude(directory, requested string) (string, error) {
	if filepath.IsAbs(requested) {
		return "", errors.New("$include 不允许绝对路径")
	}
	clean := filepath.Clean(requested)
	if clean == "." {
		return "", fmt.Errorf("$include 不能引用当前目录: %q", requested)
	}
	resolved, err := resolveRegularFile(filepath.Join(directory, clean))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(l.root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("$include 解析后逃出配置根目录: %q", requested)
	}
	return resolved, nil
}

func resolveRegularFile(filename string) (string, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return "", fmt.Errorf("读取配置文件: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("配置文件必须是非符号链接的普通文件")
	}
	resolved, err := filepath.EvalSymlinks(filename)
	if err != nil {
		return "", fmt.Errorf("解析配置文件真实路径: %w", err)
	}
	return resolved, nil
}

func readRegularFile(filename string) ([]byte, error) {
	before, err := os.Lstat(filename)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, errors.New("配置文件必须是非符号链接的普通文件")
	}
	if before.Size() > maxFileBytes {
		return nil, fmt.Errorf("配置文件超过 %d 字节限制", maxFileBytes)
	}
	handle, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件: %w", err)
	}
	defer handle.Close()
	after, err := handle.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, errors.New("配置文件在读取期间发生替换")
	}
	raw, err := io.ReadAll(io.LimitReader(handle, maxFileBytes+1))
	if err != nil || len(raw) > maxFileBytes {
		return nil, errors.New("读取配置文件失败或内容超出限制")
	}
	return raw, nil
}

func parseJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON 配置只能包含一个文档")
	}
	return value, nil
}

func parseYAML(raw []byte) (any, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("YAML 配置只能包含一个文档")
	}
	if len(document.Content) != 1 {
		return nil, errors.New("YAML 配置不能为空")
	}
	return yamlValue(document.Content[0])
}

func yamlValue(node *yaml.Node) (any, error) {
	if node.Alias != nil || node.Kind == yaml.AliasNode || node.Anchor != "" {
		return nil, errors.New("YAML 不允许 anchor 或 alias")
	}
	switch node.Kind {
	case yaml.MappingNode:
		if node.Tag != "!!map" {
			return nil, fmt.Errorf("YAML mapping 不允许标签 %q", node.Tag)
		}
		out := make(map[string]any, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "" {
				return nil, errors.New("YAML mapping key 必须是非空字符串")
			}
			if key.Value == "<<" {
				return nil, errors.New("YAML 不允许 merge key")
			}
			if _, exists := out[key.Value]; exists {
				return nil, fmt.Errorf("YAML mapping key 重复: %q", key.Value)
			}
			parsed, err := yamlValue(value)
			if err != nil {
				return nil, err
			}
			out[key.Value] = parsed
		}
		return out, nil
	case yaml.SequenceNode:
		if node.Tag != "!!seq" {
			return nil, fmt.Errorf("YAML sequence 不允许标签 %q", node.Tag)
		}
		out := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			parsed, err := yamlValue(child)
			if err != nil {
				return nil, err
			}
			out = append(out, parsed)
		}
		return out, nil
	case yaml.ScalarNode:
		return yamlScalar(node)
	default:
		return nil, fmt.Errorf("YAML 不支持 node kind %d", node.Kind)
	}
}

func yamlScalar(node *yaml.Node) (any, error) {
	switch node.Tag {
	case "!!str":
		return node.Value, nil
	case "!!null":
		return nil, nil
	case "!!bool":
		value, err := strconv.ParseBool(node.Value)
		if err != nil {
			return nil, fmt.Errorf("非法 YAML bool %q", node.Value)
		}
		return value, nil
	case "!!int", "!!float":
		if !jsonNumber.MatchString(node.Value) {
			return nil, fmt.Errorf("YAML 数字必须使用 JSON 数字语法: %q", node.Value)
		}
		return json.Number(node.Value), nil
	default:
		return nil, fmt.Errorf("YAML 不允许隐式或自定义类型 %q；请使用引号表示字符串", node.Tag)
	}
}
