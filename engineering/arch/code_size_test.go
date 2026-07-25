package arch

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	maximumProductionFileLines = 600
	maximumGoFunctionLines     = 160
)

type sizeException struct {
	Maximum int
	Reason  string
}

// Existing oversized files are a burn-down list, not a blanket exemption.
// Maximum freezes today's size: new responsibilities must be split out, and
// once a file falls under the default limit this entry must be deleted.
var productionFileSizeExceptions = map[string]sizeException{}

// Function exceptions use exact receiver/name identities and freeze the
// current span. Keep this list intentionally small; workflow-sized functions
// should normally be decomposed before landing.
var goFunctionSizeExceptions = map[string]sizeException{
	"core/kernels/backend/commands/controlplane/command.go#Run":                                                             {187, "控制面命令仍待拆分参数投影、NATS 装配与运行阶段"},
	"core/kernels/backend/main.go#runReconcile":                                                                             {181, "Backend reconcile 入口仍待拆分依赖装配和运行循环"},
	"core/kernels/backend/reconcile_support.go#parseReconcileOptions":                                                       {181, "兼容参数解析仍待迁入分域配置结构"},
	"extensions/plugins/cn.vastplan.platform.artifacts.repository/backend/main.go#main":                                     {263, "仓库插件入口仍待把协议 Handler 注册抽为装配模块"},
	"extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog/bundle.go#CreateOfflineBundle":                    {202, "离线 Bundle 创建仍待拆分取证、复制与签名验证阶段"},
	"extensions/plugins/cn.vastplan.platform.configuration.portal-composer/portalcomposer/capability.go#(*Service).Handle":  {232, "Portal Composer 能力分发仍待按治理领域拆分"},
	"extensions/plugins/cn.vastplan.platform.integration.api-exposure/apiexposure/capability.go#(*Service).handlePrincipal": {186, "API Exposure 协议分发仍待按控制面和数据面拆分"},
}

func TestArch_ProductionFilesDoNotAccumulateResponsibilities(t *testing.T) {
	root := repoRoot(t)
	seenExceptions := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if ignoredSizeGateDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !productionSourceFile(relative) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := sourceLineCount(raw)
		exception, excepted := productionFileSizeExceptions[relative]
		if excepted {
			seenExceptions[relative] = true
			if strings.TrimSpace(exception.Reason) == "" || exception.Maximum <= maximumProductionFileLines {
				t.Errorf("代码规模例外无有效理由或上限: %s", relative)
			} else if lines > exception.Maximum {
				t.Errorf("既有大文件继续增长: %s 为 %d 行，冻结上限 %d；请先拆分职责（%s）", relative, lines, exception.Maximum, exception.Reason)
			} else if lines <= maximumProductionFileLines {
				t.Errorf("大文件已降至 %d 行，请删除过期例外: %s", lines, relative)
			}
			return nil
		}
		if lines > maximumProductionFileLines {
			t.Errorf("新增代码集中: %s 为 %d 行，默认上限 %d；请按领域/工作流/端口拆分", relative, lines, maximumProductionFileLines)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描生产源码规模: %v", err)
	}
	for path := range productionFileSizeExceptions {
		if !seenExceptions[path] {
			t.Errorf("代码规模例外指向不存在或已排除文件，请删除: %s", path)
		}
	}
}

func TestArch_GoFunctionsStayWorkflowSized(t *testing.T) {
	root := repoRoot(t)
	seenExceptions := map[string]bool{}
	for _, file := range collectGoFiles(t) {
		if file.generated || strings.HasSuffix(file.relPath, "_test.go") {
			continue
		}
		filename := filepath.Join(root, filepath.FromSlash(file.relPath))
		fileset := token.NewFileSet()
		parsed, err := parser.ParseFile(fileset, filename, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("解析 Go 函数 %s: %v", file.relPath, err)
			continue
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			start := fileset.Position(function.Pos()).Line
			end := fileset.Position(function.End()).Line
			lines := end - start + 1
			identity := goFunctionIdentity(file.relPath, function)
			exception, excepted := goFunctionSizeExceptions[identity]
			if excepted {
				seenExceptions[identity] = true
				if strings.TrimSpace(exception.Reason) == "" || exception.Maximum <= maximumGoFunctionLines {
					t.Errorf("Go 函数规模例外无有效理由或上限: %s", identity)
				} else if lines > exception.Maximum {
					t.Errorf("既有长函数继续增长: %s 为 %d 行，冻结上限 %d（%s）", identity, lines, exception.Maximum, exception.Reason)
				} else if lines <= maximumGoFunctionLines {
					t.Errorf("Go 函数已降至 %d 行，请删除过期例外: %s", lines, identity)
				}
				continue
			}
			if lines > maximumGoFunctionLines {
				t.Errorf("Go 函数职责过度集中: %s 为 %d 行，默认上限 %d", identity, lines, maximumGoFunctionLines)
			}
		}
	}
	for identity := range goFunctionSizeExceptions {
		if !seenExceptions[identity] {
			t.Errorf("Go 函数规模例外已失效，请删除: %s", identity)
		}
	}
}

func ignoredSizeGateDirectory(name string) bool {
	switch name {
	case ".git", ".vastplan", "bin", "node_modules", "graphify-out", "dist", "coverage", "__pycache__":
		return true
	default:
		return false
	}
}

func productionSourceFile(path string) bool {
	if strings.HasSuffix(path, ".pb.go") || strings.HasSuffix(path, "_test.go") || strings.Contains(path, ".test.") || strings.Contains(path, ".spec.") {
		return false
	}
	for _, suffix := range []string{".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".py", ".sh"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func sourceLineCount(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	lines := bytes.Count(raw, []byte{'\n'})
	if raw[len(raw)-1] != '\n' {
		lines++
	}
	return lines
}

func goFunctionIdentity(path string, function *ast.FuncDecl) string {
	name := function.Name.Name
	if function.Recv != nil && len(function.Recv.List) != 0 {
		var receiver bytes.Buffer
		_ = printer.Fprint(&receiver, token.NewFileSet(), function.Recv.List[0].Type)
		name = fmt.Sprintf("(%s).%s", receiver.String(), name)
	}
	return path + "#" + name
}
