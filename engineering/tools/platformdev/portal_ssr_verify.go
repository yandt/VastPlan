package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

func verifyPortalSSR(client *http.Client, baseURL, session string) error {
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
	if response.StatusCode != http.StatusOK || response.Header.Get("X-VastPlan-SSR") != "rendered" || !bytes.Contains(body, []byte(`template shadowrootmode="open"`)) {
		return fmt.Errorf("SSR 验收失败 status=%d mode=%q", response.StatusCode, response.Header.Get("X-VastPlan-SSR"))
	}
	return nil
}
