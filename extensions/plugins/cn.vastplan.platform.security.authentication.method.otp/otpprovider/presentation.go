package otpprovider

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"math/big"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
)

func randomKey() ([]byte, error) {
	value := make([]byte, 32)
	_, err := rand.Read(value)
	return value, err
}
func (p *Provider) codeMAC(transactionID, code string) []byte {
	mac := hmac.New(sha256.New, p.hmacKey)
	mac.Write([]byte(transactionID))
	mac.Write([]byte{0})
	mac.Write([]byte(code))
	return mac.Sum(nil)
}
func randomCode(length int) (string, error) {
	output := make([]byte, length)
	for index := range output {
		value, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		output[index] = byte('0' + value.Int64())
	}
	return string(output), nil
}
func randomID() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func exactResponse(values []authenticationv1.FieldResponse, id string) (string, bool) {
	if len(values) != 1 || values[0].FieldID != id {
		return "", false
	}
	return values[0].Value, true
}
func terminal(state authenticationv1.MethodState, reason string) authenticationv1.MethodResult {
	return authenticationv1.MethodResult{State: state, ReasonCode: reason}
}
func text(zh, en string) authenticationv1.LocalizedText {
	return authenticationv1.LocalizedText{"zh-CN": zh, "en-US": en}
}
func identifierStep(id string, channel authenticationv1.DeliveryChannel, expires time.Time) *authenticationv1.AuthenticationStep {
	label, help, autocomplete := "邮箱或手机号", "输入企业登记的标识", "username"
	if channel == authenticationv1.DeliveryEmail {
		label, help, autocomplete = "邮箱", "输入企业邮箱", "email"
	} else if channel == authenticationv1.DeliverySMS {
		label, help, autocomplete = "手机号", "输入包含国家码的手机号", "tel"
	}
	english := map[authenticationv1.DeliveryChannel]string{authenticationv1.DeliveryEmail: "Email", authenticationv1.DeliverySMS: "Phone number"}[channel]
	return &authenticationv1.AuthenticationStep{StepID: id, Kind: authenticationv1.StepIdentifier, Title: text("验证企业身份", "Verify enterprise identity"), Description: text("我们将在账号可用时发送一次性验证码", "We will send a one-time code when the account is eligible"), SubmitLabel: text("继续", "Continue"), Fields: []authenticationv1.AuthenticationField{{ID: "identifier", Kind: authenticationv1.FieldIdentifier, Label: text(label, english), Help: text(help, "Use the identifier registered by your organization"), Autocomplete: autocomplete, Required: true, MinLength: 1, MaxLength: 320, Choices: []authenticationv1.FieldChoice{}}}, ExpiresAt: expires}
}
func codeStep(id string, expires, resendAt time.Time, length int) *authenticationv1.AuthenticationStep {
	return &authenticationv1.AuthenticationStep{StepID: id, Kind: authenticationv1.StepOneTimeCode, Title: text("输入验证码", "Enter verification code"), Description: text("如果账号可用，验证码已发送", "If the account is eligible, a code has been sent"), SubmitLabel: text("验证", "Verify"), Fields: []authenticationv1.AuthenticationField{{ID: "code", Kind: authenticationv1.FieldOneTimeCode, Label: text("验证码", "Verification code"), Help: text("验证码仅可使用一次", "The code can only be used once"), Autocomplete: "one-time-code", Required: true, MinLength: length, MaxLength: length, Choices: []authenticationv1.FieldChoice{}}}, ExpiresAt: expires, ResendAfter: &resendAt}
}
