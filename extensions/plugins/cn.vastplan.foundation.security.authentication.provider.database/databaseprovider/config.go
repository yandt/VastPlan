package databaseprovider

import (
	"errors"
	"regexp"
	"strings"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

type Profile struct {
	Connection         databasev1.ConnectionRef `json:"connection"`
	LookupSQL          string                   `json:"lookupSql"`
	SubjectColumn      string                   `json:"subjectColumn"`
	PasswordHashColumn string                   `json:"passwordHashColumn"`
	DisabledColumn     string                   `json:"disabledColumn"`
	Issuer             string                   `json:"issuer"`
}
type Configuration struct {
	Profiles map[string]Profile `json:"profiles"`
}

var selectPattern = regexp.MustCompile(`(?is)^\s*select\s+`)

func (c Configuration) Validate() error {
	if len(c.Profiles) == 0 || len(c.Profiles) > 64 {
		return errors.New("Database Authentication profiles 数量必须为 1-64")
	}
	for id, profile := range c.Profiles {
		if id == "" || profile.Connection.ResourceID == "" || profile.Connection.Revision == 0 || !selectPattern.MatchString(profile.LookupSQL) || len(profile.LookupSQL) > 4096 || strings.Contains(profile.LookupSQL, ";") || strings.Contains(profile.LookupSQL, "--") || strings.Contains(profile.LookupSQL, "/*") {
			return errors.New("Database Authentication Profile 连接或只读查询无效")
		}
		placeholders := strings.Count(profile.LookupSQL, "?") + strings.Count(profile.LookupSQL, "$1")
		if placeholders != 1 || profile.SubjectColumn == "" || profile.PasswordHashColumn == "" || profile.DisabledColumn == "" || profile.Issuer == "" {
			return errors.New("Database Authentication Profile 必须声明唯一参数和结果列")
		}
	}
	return nil
}
