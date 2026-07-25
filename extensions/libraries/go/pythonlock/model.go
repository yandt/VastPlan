// Package pythonlock validates the offline-safe subset of the PyPA pylock.toml
// 1.0 specification used by signed VastPlan Python plugin artifacts.
package pythonlock

const (
	Format                    = "pylock-toml"
	SpecVersion               = "1.0"
	PackagePath               = "supply-chain/pylock.toml"
	WheelPathPrefix           = "supply-chain/python-wheels/"
	MaxLockBytes              = 16 << 20
	MaxPackages               = 10_000
	MaxWheelsPerPackage       = 64
	MaxTotalWheels            = 100_000
	MaxWheelBytes       int64 = 256 << 20
)

type Summary struct {
	SHA256         string
	RequiresPython string
	CreatedBy      string
	Packages       []Package
	Wheels         []Wheel
}

type Package struct {
	Name           string
	Version        string
	Marker         string
	RequiresPython string
	Dependencies   []Dependency
}

type Dependency struct {
	Name    string
	Version string
	Marker  string
}

type Wheel struct {
	PackageName    string
	PackageVersion string
	Name           string
	Path           string
	PackagePath    string
	Size           int64
	SHA256         string
}

type document struct {
	LockVersion      string        `toml:"lock-version"`
	Environments     []string      `toml:"environments"`
	RequiresPython   string        `toml:"requires-python"`
	Extras           []string      `toml:"extras"`
	DependencyGroups []string      `toml:"dependency-groups"`
	DefaultGroups    []string      `toml:"default-groups"`
	CreatedBy        string        `toml:"created-by"`
	Packages         []lockPackage `toml:"packages"`
}

type lockPackage struct {
	Name           string         `toml:"name"`
	Version        string         `toml:"version"`
	Marker         string         `toml:"marker"`
	RequiresPython string         `toml:"requires-python"`
	Dependencies   []Dependency   `toml:"dependencies"`
	Wheels         []lockWheel    `toml:"wheels"`
	VCS            map[string]any `toml:"vcs"`
	Directory      map[string]any `toml:"directory"`
	Archive        map[string]any `toml:"archive"`
	SDist          map[string]any `toml:"sdist"`
}

type lockWheel struct {
	Name   string            `toml:"name"`
	URL    string            `toml:"url"`
	Path   string            `toml:"path"`
	Size   int64             `toml:"size"`
	Hashes map[string]string `toml:"hashes"`
}
