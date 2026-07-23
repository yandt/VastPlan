package artifacttrust

import (
	"errors"
	"fmt"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pythonlock"
)

func validatePackagedPythonLock(packageBytes []byte, manifest pluginv1.Manifest, facts map[string]packageFileFact) error {
	execution := pluginv1.BackendExecutionContract(manifest)
	pythonDriver := execution.Driver == "python" || execution.Driver == "python-subinterpreter"
	var declaration *pluginv1.SupplyChainDocument
	if manifest.SupplyChain != nil {
		declaration = manifest.SupplyChain.PythonLock
	}
	if declaration == nil {
		if pythonDriver {
			return errors.New("Python 插件必须携带签名清单绑定的 pylock.toml")
		}
		return nil
	}
	if !pythonDriver {
		return errors.New("非 Python 插件不得声明 pythonLock")
	}
	fact, exists := facts[declaration.Path]
	if !exists || fact.size <= 0 || fact.size > pythonlock.MaxLockBytes || fact.sha256 != declaration.SHA256 {
		return errors.New("插件包 pylock.toml 与签名清单声明失配")
	}
	raw, err := ReadPackageFile(packageBytes, declaration.Path, pythonlock.MaxLockBytes)
	if err != nil {
		return fmt.Errorf("读取插件包 pylock.toml: %w", err)
	}
	summary, err := pythonlock.Inspect(raw)
	if err != nil {
		return err
	}
	if declaration.Format != pythonlock.Format || declaration.SpecVersion != pythonlock.SpecVersion || declaration.Path != pythonlock.PackagePath || declaration.SHA256 != summary.SHA256 {
		return errors.New("插件包 pylock.toml 格式或摘要与签名清单失配")
	}
	if err := pythonlock.ValidateManifestRequirements(execution.Requirements, summary); err != nil {
		return err
	}
	referenced := make(map[string]struct{}, len(summary.Wheels))
	for _, wheel := range summary.Wheels {
		fact, exists := facts[wheel.PackagePath]
		if !exists || fact.size != wheel.Size || fact.sha256 != wheel.SHA256 {
			return fmt.Errorf("Python wheel 与 pylock.toml 失配: %s", wheel.PackagePath)
		}
		referenced[wheel.PackagePath] = struct{}{}
	}
	for filename := range facts {
		if !strings.HasPrefix(filename, pythonlock.WheelPathPrefix) {
			continue
		}
		if _, exists := referenced[filename]; !exists {
			return fmt.Errorf("插件包包含 pylock.toml 未引用的 wheel: %s", filename)
		}
	}
	return nil
}
