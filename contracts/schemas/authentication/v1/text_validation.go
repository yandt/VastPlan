package authenticationv1

import (
	"errors"
	"strings"
	"unicode"
)

func validateLocalizedText(text LocalizedText) error {
	for _, value := range text {
		if strings.ContainsAny(value, "<>") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
			return errors.New("本地化文案只能是纯文本，不能包含 HTML 或控制字符")
		}
	}
	return nil
}
