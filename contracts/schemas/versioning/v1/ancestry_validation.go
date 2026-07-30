package versioningv1

import "errors"

func ValidateIsAncestorResult(result IsAncestorResult) error {
	if result.IsAncestor {
		if result.Distance < 0 || result.Distance > MaxAncestryNodes {
			return errors.New("祖先距离无效")
		}
		return nil
	}
	if result.Distance != -1 {
		return errors.New("非祖先结果的 distance 必须为 -1")
	}
	return nil
}

func ValidateCommonAncestorResult(result FindCommonAncestorResult) error {
	if !result.Found {
		if result.Ancestor != nil || result.LeftDistance != -1 || result.RightDistance != -1 {
			return errors.New("未找到共同祖先时结果必须为空")
		}
		return nil
	}
	if result.Ancestor == nil || ValidateVersionRef(*result.Ancestor) != nil || result.LeftDistance < 0 || result.RightDistance < 0 || result.LeftDistance > MaxAncestryNodes || result.RightDistance > MaxAncestryNodes {
		return errors.New("共同祖先结果无效")
	}
	return nil
}
