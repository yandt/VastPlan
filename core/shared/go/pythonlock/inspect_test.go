package pythonlock

import (
	"strings"
	"testing"
)

const validLock = `lock-version = "1.0"
requires-python = ">=3.11"
created-by = "pip"
extras = []
dependency-groups = []
default-groups = []

[[packages]]
name = "example-dependency"
version = "1.2.3"
requires-python = ">=3.9"
dependencies = [{name = "transitive", version = "2.0.0"}]

[[packages.wheels]]
name = "example_dependency-1.2.3-py3-none-any.whl"
path = "python-wheels/example_dependency-1.2.3-py3-none-any.whl"
size = 42
hashes = {sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}

[[packages]]
name = "transitive"
version = "2.0.0"

[[packages.wheels]]
path = "python-wheels/transitive-2.0.0-py3-none-any.whl"
size = 21
hashes = {sha256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
`

func TestInspectAcceptsBoundedOfflinePylock(t *testing.T) {
	summary, err := Inspect([]byte(validLock))
	if err != nil {
		t.Fatal(err)
	}
	if summary.RequiresPython != ">=3.11" || len(summary.Packages) != 2 || len(summary.Wheels) != 2 || len(summary.SHA256) != 64 {
		t.Fatalf("pylock summary 不完整: %#v", summary)
	}
	if err := ValidateManifestRequirements(map[string]string{"python": ">=3.11", "example__dependency": "1.2.3"}, summary); err != nil {
		t.Fatal(err)
	}
}

func TestInspectRejectsOnlineAndMutableSources(t *testing.T) {
	cases := map[string]string{
		"remote wheel": strings.Replace(validLock, `path = "python-wheels/example_dependency-1.2.3-py3-none-any.whl"`, `url = "https://example.invalid/example.whl"`, 1),
		"sdist": strings.Replace(validLock, `[[packages.wheels]]`, `[packages.sdist]
path = "python-wheels/example.tar.gz"
size = 42
hashes = {sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}

[[packages.wheels]]`, 1),
		"empty vcs source": strings.Replace(validLock, `[[packages.wheels]]`, `[packages.vcs]

[[packages.wheels]]`, 1),
		"path escape": strings.Replace(validLock, `python-wheels/example_dependency-1.2.3-py3-none-any.whl`, `../example.whl`, 1),
		"weak hash":   strings.Replace(validLock, strings.Repeat("a", 64), "abc", 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Inspect([]byte(raw)); err == nil {
				t.Fatal("不安全 pylock 必须拒绝")
			}
		})
	}
}

func TestManifestDirectDependenciesMustExistInCompleteLock(t *testing.T) {
	summary, err := Inspect([]byte(validLock))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateManifestRequirements(map[string]string{"python": ">=3.12", "missing": "1.0.0"}, summary); err == nil {
		t.Fatal("Python 版本和直接依赖未绑定完整锁时必须拒绝")
	}
}
