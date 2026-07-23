package repositoryruntime

import (
	"errors"
	"fmt"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

func (m *Manager) PrepareAssessmentReport(digest string) (string, error) {
	if m == nil || m.assessmentReports == nil {
		return "", errors.New("Repository 未配置安全评估报告归档")
	}
	if err := m.assessmentReports.Require(digest); err != nil {
		return "", errors.New("安全评估报告缺失或无效")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return "", errors.New("活动制品仓库不可用")
	}
	_, entries := m.active.catalog.Entries()
	for _, entry := range entries {
		admissionRaw, err := m.active.signed.ReadSecurityAdmission(entry.Ref)
		if err == nil && evaluationRawReportReferenced(admissionRaw, digest, false) {
			return fmt.Sprintf("/v1/assessment-reports/%s", digest), nil
		}
		chainRaw, err := m.active.signed.ReadSecurityStatusChain(entry.Ref)
		if err != nil {
			continue
		}
		records, err := artifactassessment.InspectStatusChain(chainRaw)
		if err != nil {
			continue
		}
		for _, raw := range records {
			if evaluationRawReportReferenced(raw, digest, true) {
				return fmt.Sprintf("/v1/assessment-reports/%s", digest), nil
			}
		}
	}
	return "", errors.New("安全评估报告未被当前仓库证据引用")
}

func evaluationRawReportReferenced(raw []byte, digest string, status bool) bool {
	var evaluation artifactassessment.Evaluation
	if status {
		record, _, err := artifactassessment.InspectStatus(raw)
		if err != nil {
			return false
		}
		evaluation = record.Evaluation
	} else {
		record, _, err := artifactassessment.InspectAdmission(raw)
		if err != nil {
			return false
		}
		evaluation = record.Evaluation
	}
	return evaluation.Vulnerabilities.ReportSHA256 == digest || evaluation.Licenses.ReportSHA256 == digest
}

func (m *Manager) ReadAssessmentReport(digest string) ([]byte, error) {
	if m == nil || m.assessmentReports == nil {
		return nil, errors.New("Repository 未配置安全评估报告归档")
	}
	return m.assessmentReports.Read(digest)
}

func (m *Manager) PutAssessmentReport(digest string, raw []byte) error {
	if m == nil || m.assessmentReports == nil {
		return errors.New("Repository 未配置安全评估报告归档")
	}
	return m.assessmentReports.Put(digest, raw)
}

func (m *Manager) requireAdmissionReports(raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	record, _, err := artifactassessment.InspectAdmission(raw)
	if err != nil {
		return err
	}
	return m.requireEvaluationReports(record.Evaluation)
}

func (m *Manager) requireStatusReports(raw []byte) error {
	record, _, err := artifactassessment.InspectStatus(raw)
	if err != nil {
		return err
	}
	return m.requireEvaluationReports(record.Evaluation)
}

func (m *Manager) requireEvaluationReports(evaluation artifactassessment.Evaluation) error {
	digests := make([]string, 0, 2)
	for _, digest := range []string{evaluation.Vulnerabilities.ReportSHA256, evaluation.Licenses.ReportSHA256} {
		if digest == "" {
			continue
		}
		duplicate := false
		for _, existing := range digests {
			if existing == digest {
				duplicate = true
				break
			}
		}
		if !duplicate {
			digests = append(digests, digest)
		}
	}
	if len(digests) == 0 {
		return nil
	}
	if m == nil || m.assessmentReports == nil {
		return errors.New("安全评估记录引用报告，但 Repository 未配置报告归档")
	}
	if err := m.assessmentReports.Require(digests...); err != nil {
		return errors.New("安全评估记录引用的原始报告缺失或无效")
	}
	return nil
}
