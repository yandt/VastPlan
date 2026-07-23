package repositoryruntime

import (
	"errors"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

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
