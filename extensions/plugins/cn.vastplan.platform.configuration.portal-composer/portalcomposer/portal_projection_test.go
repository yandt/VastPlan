package portalcomposer

import (
	"bytes"
	"encoding/json"
	"testing"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func TestFrozenConfigurationWithoutAccountCenterKeepsItsOriginalDigest(t *testing.T) {
	profile := testProfile()
	profile.AccountCenter = nil
	profile.Plugins = profile.Plugins[:len(profile.Plugins)-1]
	configuration := portalapi.PortalConfiguration{
		Platform: profile, Application: testComposition("/admin"), Services: testPlatformCatalog().Bindings[0].Services,
	}
	want, err := portalConfigurationDigest(configuration)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"accountCenter"`)) {
		t.Fatal("旧冻结配置不得因新增可选解码字段改变规范 JSON")
	}
	var restored portalapi.PortalConfiguration
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	got, err := portalConfigurationDigest(restored)
	if err != nil || got != want {
		t.Fatalf("旧冻结配置跨解码后摘要漂移: want=%s got=%s err=%v", want, got, err)
	}
}
