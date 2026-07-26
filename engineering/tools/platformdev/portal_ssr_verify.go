package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func verifyPortalColdStart(client *http.Client, baseURL, session string) error {
	request, err := http.NewRequest(http.MethodGet, baseURL+"/operations", nil)
	if err != nil {
		return err
	}
	request.AddCookie(&http.Cookie{Name: "vastplan_session", Value: session})
	request.Header.Set("Accept-Language", "zh-CN")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	mode := response.Header.Get("X-VastPlan-SSR")
	validBody := mode == "rendered" && bytes.Contains(body, []byte(`template shadowrootmode="open"`))
	validBody = validBody || mode == "bypass" && bytes.Contains(body, []byte(`<div id="vastplan-portal" aria-live="polite"></div>`))
	if response.StatusCode != http.StatusOK || !validBody {
		return fmt.Errorf("Portal 冷启动验收失败 status=%d ssr-mode=%q", response.StatusCode, mode)
	}
	return nil
}
