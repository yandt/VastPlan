package arch

import (
	"path/filepath"
	"strings"
	"testing"
)

const (
	nodeAgentDirectory         = "core/kernels/backend/nodeagent"
	nodeAgentImportPath        = modulePath + "/" + nodeAgentDirectory
	nodeAgentModelDirectory    = nodeAgentDirectory + "/model"
	nodeAgentRuntimeDirectory  = nodeAgentDirectory + "/runtimehost"
	nodeAgentRuntimeImportPath = modulePath + "/" + nodeAgentRuntimeDirectory
)

func TestArch_NodeAgentPhysicalBoundaryHasOneWayDependencies(t *testing.T) {
	for _, file := range collectGoFiles(t) {
		if file.generated || strings.HasSuffix(file.relPath, "_test.go") {
			continue
		}
		directory := filepath.ToSlash(filepath.Dir(file.relPath))
		for _, imported := range file.imports {
			switch {
			case directory == nodeAgentModelDirectory && strings.HasPrefix(imported, modulePath+"/core/"):
				t.Errorf("中立 Node Agent model 不得依赖 Core 实现: %s imports %s", file.relPath, imported)
			case directory == nodeAgentRuntimeDirectory && imported == nodeAgentImportPath:
				t.Errorf("Runtime Host 不得反向依赖根 Node Agent: %s imports %s", file.relPath, imported)
			case imported == nodeAgentRuntimeImportPath && directory != nodeAgentDirectory:
				t.Errorf("生产调用方必须经 Node Agent facade 使用 Runtime Host: %s imports %s", file.relPath, imported)
			}
		}
	}
}
