package nodeagent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

const securityWatermarkSchemaVersion = "v1"

var securityWatermarkLocks sync.Map

type securityWatermark struct {
	SchemaVersion   string `json:"schemaVersion"`
	ArtifactSHA256  string `json:"artifactSha256"`
	AdmissionSHA256 string `json:"admissionSha256"`
	Sequence        uint64 `json:"sequence"`
	RecordSHA256    string `json:"recordSha256"`
}

func enforceSecurityWatermark(installRoot string, verified VerifiedArtifact) error {
	next, present, err := watermarkFromVerified(verified)
	if err != nil || !present {
		return err
	}
	directory := filepath.Join(filepath.Clean(installRoot), ".security-watermarks")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("创建安全复扫高水位目录: %w", err)
	}
	filename := filepath.Join(directory, next.ArtifactSHA256+".json")
	lockValue, _ := securityWatermarkLocks.LoadOrStore(filename, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	priorRaw, err := os.ReadFile(filename)
	if err == nil {
		var prior securityWatermark
		decoder := json.NewDecoder(bytes.NewReader(priorRaw))
		decoder.DisallowUnknownFields()
		if decodeErr := decoder.Decode(&prior); decodeErr != nil || prior.SchemaVersion != securityWatermarkSchemaVersion || prior.ArtifactSHA256 != next.ArtifactSHA256 || prior.AdmissionSHA256 != next.AdmissionSHA256 {
			return errors.New("本机安全复扫高水位状态无效")
		}
		if next.Sequence < prior.Sequence || next.Sequence == prior.Sequence && next.RecordSHA256 != prior.RecordSHA256 {
			return errors.New("制品安全复扫状态发生回滚或同序号替换")
		}
		if next == prior {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取安全复扫高水位: %w", err)
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".watermark-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("提交安全复扫高水位: %w", err)
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("打开安全复扫高水位目录: %w", err)
	}
	syncErr, closeErr := directoryFile.Sync(), directoryFile.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("持久化安全复扫高水位目录: %w", err)
	}
	return nil
}

func watermarkFromVerified(verified VerifiedArtifact) (securityWatermark, bool, error) {
	if len(verified.securityAdmission) == 0 {
		return securityWatermark{}, false, nil
	}
	_, admissionDigest, err := artifactassessment.InspectAdmission(verified.securityAdmission)
	if err != nil {
		return securityWatermark{}, false, err
	}
	value := securityWatermark{SchemaVersion: securityWatermarkSchemaVersion, ArtifactSHA256: verified.artifact.SHA256, AdmissionSHA256: admissionDigest, RecordSHA256: admissionDigest}
	records, err := artifactassessment.InspectStatusChain(verified.securityStatusChain)
	if err != nil {
		return securityWatermark{}, false, err
	}
	if len(records) > 0 {
		latest, digest, err := artifactassessment.InspectStatus(records[len(records)-1])
		if err != nil {
			return securityWatermark{}, false, err
		}
		value.Sequence, value.RecordSHA256 = latest.Sequence, digest
	}
	return value, true, nil
}
