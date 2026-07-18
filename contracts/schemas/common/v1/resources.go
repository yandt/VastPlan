package commonv1

// ResourceList 使用规范化整数，避免不同组件对 "500m"、"2Gi" 等文本单位产生歧义。
type ResourceList struct {
	CPUMillis   int64 `json:"cpu_millis,omitempty"`
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
	GPU         int64 `json:"gpu,omitempty"`
}

// ResourceRequirements 是部署规格中按副本声明的资源请求。
type ResourceRequirements struct {
	Requests ResourceList `json:"requests,omitempty"`
}
