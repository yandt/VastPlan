package frontendcompositionv1

import "testing"

func TestFrontendInputsSeparatePlatformAndApplication(t *testing.T) {
	profile, err := ParsePlatformProfile([]byte(`{"version":1,"revision":2,"id":"portal-default","target":{"kernel":"frontend"},"designSystem":{"id":"com.vastplan.foundation.frontend.design-system.arco","version":"1.0.0","uiContract":"^1.0.0"},"composition":{"id":"com.vastplan.foundation.frontend.composition.standard","version":"1.0.0","uiContract":"^1.0.0"},"layout":{"id":"com.vastplan.foundation.frontend.layout.standard","version":"1.0.0","uiContract":"^1.0.0"},"plugins":[{"id":"com.vastplan.foundation.frontend.design-system.arco","version":"1.0.0"},{"id":"com.vastplan.foundation.frontend.composition.standard","version":"1.0.0"},{"id":"com.vastplan.foundation.frontend.layout.standard","version":"1.0.0"}],"security":{"firstPartyOnly":true,"requireIntegrity":true}}`))
	if err != nil || profile.Plugins[0].Channel != "stable" || len(profile.Digest()) != 64 {
		t.Fatalf("profile 无效: %+v %v", profile, err)
	}
	app, err := ParseApplicationComposition([]byte(`{"version":1,"revision":3,"id":"operations","target":{"kernel":"frontend"},"route":"/operations","plugins":[{"id":"com.vastplan.product.frontend.operations","version":"1.0.0"}]}`))
	if err != nil || app.Plugins[0].Channel != "stable" || len(app.Digest()) != 64 {
		t.Fatalf("application 无效: %+v %v", app, err)
	}
}

func TestFrontendInputsRejectBoundaryViolations(t *testing.T) {
	if _, err := ParsePlatformProfile([]byte(`{"version":1,"revision":1,"id":"x","target":{"kernel":"frontend"},"designSystem":{"id":"com.vastplan.foundation.frontend.design-system.arco","version":"1.0.0","uiContract":"^1"},"plugins":[]}`)); err == nil {
		t.Fatal("平台 plugins 缺设计系统必须拒绝")
	}
	if _, err := ParseApplicationComposition([]byte(`{"version":1,"revision":1,"id":"x","target":{"kernel":"frontend"},"route":"/","designSystem":{},"plugins":[]}`)); err == nil {
		t.Fatal("应用输入不得携带 designSystem")
	}
}
