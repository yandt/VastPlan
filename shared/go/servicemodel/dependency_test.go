package servicemodel

import "testing"

func TestTopologicalOrder(t *testing.T) {
	order, err := TopologicalOrder(map[string][]string{"c": {"b"}, "b": {"a"}, "a": {}})
	if err != nil || len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Fatalf("依赖优先顺序错误: %v %v", order, err)
	}
	if _, err := TopologicalOrder(map[string][]string{"a": {"b"}, "b": {"a"}}); err == nil {
		t.Fatal("环依赖必须拒绝")
	}
	if _, err := TopologicalOrder(map[string][]string{"a": {"missing"}}); err == nil {
		t.Fatal("缺失依赖必须拒绝")
	}
}
