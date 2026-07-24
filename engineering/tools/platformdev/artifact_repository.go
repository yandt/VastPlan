package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
)

func (r *runtime) prepareTestingRepositoryProtocol() (artifactrepositoryv1.Profile, error) {
	tokenRaw := make([]byte, 32)
	if _, err := rand.Read(tokenRaw); err != nil {
		return artifactrepositoryv1.Profile{}, fmt.Errorf("生成 local-test 仓库令牌: %w", err)
	}
	if err := os.WriteFile(r.testingRepositoryTokenFile(), []byte(base64.RawURLEncoding.EncodeToString(tokenRaw)+"\n"), 0o600); err != nil {
		return artifactrepositoryv1.Profile{}, err
	}
	profile, err := r.testingRepositoryProfile()
	if err != nil {
		return artifactrepositoryv1.Profile{}, err
	}
	raw, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return artifactrepositoryv1.Profile{}, err
	}
	if err := os.WriteFile(r.testingRepositoryProfileFile(), append(raw, '\n'), 0o600); err != nil {
		return artifactrepositoryv1.Profile{}, err
	}
	r.repositoryProfile = profile
	if profile.Protocol == artifactrepositoryv1.ProtocolLocalTest {
		if err := removeStaleDevelopmentSocket(r.testingRepositorySocket()); err != nil {
			return artifactrepositoryv1.Profile{}, err
		}
	}
	return profile, nil
}

func (r *runtime) testingRepositoryRoot() string {
	return filepath.Join(r.options.stateRoot, "repositories", "testing")
}

func (r *runtime) testingRepositoryVolumes() string {
	return filepath.Join(r.testingRepositoryRoot(), "volumes")
}

func (r *runtime) testingRepositoryData() string {
	return filepath.Join(r.testingRepositoryVolumes(), "repository.primary")
}

func (r *runtime) testingAssessmentReports() string {
	return filepath.Join(r.testingRepositoryRoot(), "assessment-reports")
}

func (r *runtime) testingRepositorySecrets() string {
	return filepath.Join(r.testingRepositoryRoot(), "secrets")
}

func (r *runtime) testingRepositorySigningKey() string {
	return filepath.Join(r.testingRepositorySecrets(), "artifact-signing.pem")
}

func (r *runtime) testingRepositoryTrust() string {
	return filepath.Join(r.testingRepositoryRoot(), "artifact-trust.json")
}

func (r *runtime) testingRepositorySocket() string {
	return filepath.Join(r.testingRepositoryRoot(), "repository.sock")
}

func (r *runtime) testingRepositoryProfileFile() string {
	return filepath.Join(r.runDir, "repository-profile.json")
}

func (r *runtime) testingRepositoryTokenFile() string {
	return filepath.Join(r.runDir, "secrets", "artifact-local-test.token")
}

func (r *runtime) testingRepositoryProfile() (artifactrepositoryv1.Profile, error) {
	profile := artifactrepositoryv1.Profile{Version: artifactrepositoryv1.ProfileVersion, ID: "local-testing", Channels: []string{"testing"}}
	if r.options.artifactProtocol == "remote-compat" {
		profile.Protocol = artifactrepositoryv1.ProtocolRemote
		profile.Endpoint = "https://" + r.options.artifactListen
	} else {
		profile.Protocol = artifactrepositoryv1.ProtocolLocalTest
		profile.Endpoint = "unix://" + filepath.ToSlash(r.testingRepositorySocket())
		profile.DevelopmentOnly = true
	}
	return artifactrepositoryv1.ValidateProfile(profile)
}

func removeStaleDevelopmentSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("检查 local-test socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("local-test socket 路径被非 socket 文件占用: %s", path)
	}
	connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return fmt.Errorf("local-test socket 仍有活动服务，拒绝覆盖: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("清理失效 local-test socket: %w", err)
	}
	return nil
}

func (r *runtime) managedArtifactSourceArgs() []string {
	if r.repositoryProfile.Protocol == artifactrepositoryv1.ProtocolLocalTest {
		return []string{
			"-bootstrap-repository", filepath.Join(r.runDir, "repository"),
			"-bootstrap-inventory", filepath.Join(r.runDir, "seed-inventory.json"),
			"-repository-profile", r.testingRepositoryProfileFile(),
			"-repository-token-file", r.testingRepositoryTokenFile(),
			"-repository-trust", filepath.Join(r.runDir, "secrets", "artifact-trust.json"),
		}
	}
	return []string{
		"-bootstrap-repository", filepath.Join(r.runDir, "repository"),
		"-bootstrap-inventory", filepath.Join(r.runDir, "seed-inventory.json"),
		"-repository-url", "https://" + r.options.artifactListen,
		"-repository-trust", filepath.Join(r.runDir, "secrets", "artifact-trust.json"),
		"-repository-ca", filepath.Join(r.runDir, "secrets", "tls-cert.pem"),
	}
}

func (r *runtime) controllerArtifactSourceArgs() []string {
	if r.repositoryProfile.Protocol == artifactrepositoryv1.ProtocolLocalTest {
		return []string{
			"-repository-profile", r.testingRepositoryProfileFile(),
			"-repository-token-file", r.testingRepositoryTokenFile(),
			"-repository-trust", filepath.Join(r.runDir, "secrets", "artifact-trust.json"),
		}
	}
	return []string{
		"-repository-url", "https://" + r.options.artifactListen,
		"-repository-trust", filepath.Join(r.runDir, "secrets", "artifact-trust.json"),
		"-repository-ca", filepath.Join(r.runDir, "secrets", "tls-cert.pem"),
	}
}

func (r *runtime) testingRepositoryReady() bool {
	if r.repositoryProfile.Protocol == artifactrepositoryv1.ProtocolLocalTest {
		connection, err := net.DialTimeout("unix", r.testingRepositorySocket(), 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = connection.Close()
		return true
	}
	connection, err := net.DialTimeout("tcp", r.options.artifactListen, 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}
