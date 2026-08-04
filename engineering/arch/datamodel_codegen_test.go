package arch

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	datamigrationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamigration/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/engineering/internal/datamodelgen"
)

func TestSignedDataModelsAndGeneratedRepositoriesHaveNoDrift(t *testing.T) {
	forEachPluginManifest(t, "extensions/plugins", func(manifestPath string, manifest pluginv1.Manifest) {
		references, err := pluginv1.ManifestDataModels(manifest)
		if err != nil {
			t.Errorf("%s: %v", manifestPath, err)
			return
		}
		pluginRoot := filepath.Dir(manifestPath)
		for _, reference := range references {
			t.Run(manifest.ID+"/"+reference.ID, func(t *testing.T) {
				modelPath := safePluginPath(t, pluginRoot, reference.Path)
				raw, err := os.ReadFile(modelPath)
				if err != nil {
					t.Fatal(err)
				}
				digest := fmt.Sprintf("%x", sha256.Sum256(raw))
				if digest != reference.SHA256 {
					t.Fatalf("DataModel 签名漂移: manifest=%s actual=%s", reference.SHA256, digest)
				}
				model, err := datamodelv1.Parse(raw)
				if err != nil {
					t.Fatal(err)
				}
				if model.ID != reference.ID || reference.ContractVersion != datamodelv1.ContractVersion {
					t.Fatalf("DataModel 身份不一致: ref=%+v model=%s", reference, model.ID)
				}
				assertGeneratedRepository(t, pluginRoot, model, datamodelgen.Go, "go", "generated")
				assertGeneratedRepository(t, pluginRoot, model, datamodelgen.TypeScript, "typescript", "generated")
				assertGeneratedRepository(t, pluginRoot, model, datamodelgen.Python, "python", "generated")
			})
		}
	})
}

func TestSignedDataMigrationsHaveNoDrift(t *testing.T) {
	forEachPluginManifest(t, "extensions/plugins", func(manifestPath string, manifest pluginv1.Manifest) {
		references, err := pluginv1.ManifestDataMigrations(manifest)
		if err != nil {
			t.Errorf("%s: %v", manifestPath, err)
			return
		}
		pluginRoot := filepath.Dir(manifestPath)
		for _, reference := range references {
			t.Run(manifest.ID+"/"+reference.ID, func(t *testing.T) {
				raw, err := os.ReadFile(safePluginPath(t, pluginRoot, reference.Path))
				if err != nil {
					t.Fatal(err)
				}
				digest := fmt.Sprintf("%x", sha256.Sum256(raw))
				if digest != reference.SHA256 {
					t.Fatalf("DataMigration 签名漂移: manifest=%s actual=%s", reference.SHA256, digest)
				}
				migration, err := datamigrationv1.Parse(raw)
				if err != nil {
					t.Fatal(err)
				}
				if migration.ID != reference.ID || migration.ModelID != reference.ModelID || migration.From.SchemaVersion != reference.FromVersion || migration.To.SchemaVersion != reference.ToVersion || reference.ContractVersion != datamigrationv1.ContractVersion {
					t.Fatalf("DataMigration 身份不一致: ref=%+v migration=%+v", reference, migration)
				}
			})
		}
	})
}

func safePluginPath(t *testing.T, root, relative string) string {
	t.Helper()
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("声明文件路径越出插件目录: %s", relative)
	}
	current := root
	for _, segment := range strings.Split(filepath.FromSlash(relative), string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			t.Fatalf("检查声明文件路径: %v", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("声明文件路径不得经过符号链接: %s", current)
		}
	}
	return path
}

func assertGeneratedRepository(t *testing.T, pluginRoot string, model datamodelv1.Model, language datamodelgen.Language, directory, packageName string) {
	t.Helper()
	root := filepath.Join(pluginRoot, "generated", directory)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return
	} else if err != nil {
		t.Fatal(err)
	}
	output, err := datamodelgen.Generate(model, language, packageName)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(root, output.Filename))
	if err != nil {
		t.Fatalf("读取已提交生成物: %v", err)
	}
	if !bytes.Equal(actual, output.Content) {
		t.Fatalf("生成物已陈旧，请重新运行 datamodelgen: %s", filepath.Join(root, output.Filename))
	}
}
