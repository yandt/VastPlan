package contentstaging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
)

const maxProtectionRecordBytes = 32 << 20

func (p *FileProvider) LoadProtections(ctx context.Context) ([]protectionRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tenants := filepath.Join(p.root, "tenants")
	entries, err := os.ReadDir(tenants)
	if errors.Is(err, os.ErrNotExist) {
		return []protectionRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := validatePrivateDirectory(tenants); err != nil {
		return nil, err
	}
	records := []protectionRecord{}
	for _, tenant := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !tenant.IsDir() || len(tenant.Name()) != 64 || !isLowerHex(tenant.Name()) {
			return nil, fmt.Errorf("Content Staging tenants 包含无效条目 %q", tenant.Name())
		}
		directory := filepath.Join(tenants, tenant.Name(), "protections")
		items, err := os.ReadDir(directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := validateExistingDirectoryChain(p.root, []string{"tenants", tenant.Name(), "protections"}); err != nil {
			return nil, err
		}
		for _, item := range items {
			if strings.HasPrefix(item.Name(), ".protection-") {
				continue
			}
			if item.IsDir() || !strings.HasPrefix(item.Name(), "vcr_") || !strings.HasSuffix(item.Name(), ".json") {
				return nil, fmt.Errorf("Content Reference protections 包含无效条目 %q", item.Name())
			}
			var record protectionRecord
			if err := readPrivateJSON(filepath.Join(directory, item.Name()), maxProtectionRecordBytes, &record); err != nil {
				return nil, err
			}
			if item.Name() != record.Protection.ID+".json" || pathDigest(record.Owner.TenantID) != tenant.Name() {
				return nil, errors.New("Content Reference 文件身份不一致")
			}
			records = append(records, record)
		}
	}
	return records, nil
}

func (p *FileProvider) SaveProtection(ctx context.Context, record protectionRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateStoredProtection(record); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, err := p.tenantChild(record.Owner, "protections", true)
	if err != nil {
		return err
	}
	return writeAtomicJSON(directory, ".protection-*", record.Protection.ID+".json", record)
}

func (p *FileProvider) DeleteProtection(ctx context.Context, scope Scope, protectionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := contentv1.ValidateStatusRequest(contentv1.StatusRequest{ProtectionID: protectionID}); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	directory, err := p.tenantChild(scope, "protections", false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return removeRegularFile(filepath.Join(directory, protectionID+".json"))
}
