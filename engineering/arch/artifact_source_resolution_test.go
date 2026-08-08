package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

type exactResolutionConsumer struct {
	path     string
	receiver string
	function string
}

// Ordered artifact fallback is a security protocol, not caller-local loop
// mechanics. Every trust domain keeps its own verification callback while the
// shared resolver alone decides which error may advance to another source.
func TestArch_ArtifactConsumersUseSharedExactSourceResolution(t *testing.T) {
	consumers := []exactResolutionConsumer{
		{"core/kernels/backend/reconcile_support.go", "artifactResolution", "Read"},
		{"core/kernels/backend/nodeagent/reconcile_support.go", "Reconciler", "resolveArtifact"},
		{"core/kernels/backend/portaltrust/catalog.go", "TrustedCatalog", "verifiedManifest"},
		{"core/kernels/backend/commands/controlplane/artifacts.go", "fallbackArtifactReader", "Read"},
	}
	for _, consumer := range consumers {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot(t), filepath.FromSlash(consumer.path)), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("解析 %s: %v", consumer.path, err)
		}
		function := findMethod(file, consumer.receiver, consumer.function)
		if function == nil {
			t.Errorf("未找到受治理制品消费者 %s.%s", consumer.receiver, consumer.function)
			continue
		}
		usesSharedResolver := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "ResolveExact" {
				usesSharedResolver = true
			}
			return true
		})
		if !usesSharedResolver {
			t.Errorf("%s.%s 必须通过 artifacttrust.ResolveExact 统一遍历制品源", consumer.receiver, consumer.function)
		}
	}
}

func findMethod(file *ast.File, receiver, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != name || function.Recv == nil || len(function.Recv.List) != 1 {
			continue
		}
		typeName := function.Recv.List[0].Type
		if pointer, ok := typeName.(*ast.StarExpr); ok {
			typeName = pointer.X
		}
		identifier, ok := typeName.(*ast.Ident)
		if ok && identifier.Name == receiver {
			return function
		}
	}
	return nil
}
