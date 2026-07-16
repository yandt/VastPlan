package arch

import (
	"reflect"
	"sort"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	addressingv1 "cdsoft.com.cn/VastPlan/shared/go/addressing/v1"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	pluginhostv1 "cdsoft.com.cn/VastPlan/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/shared/go/protocol"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// fieldSpec 是 Backend 1.0 已发布字段的最小兼容指纹。
// 新增字段允许；已有字段改名、删除、改编号或改 wire kind 会失败。
type fieldSpec struct {
	number protoreflect.FieldNumber
	kind   protoreflect.Kind
}

func TestCompatibility_BackendV1Matrix(t *testing.T) {
	if !reflect.DeepEqual(protocol.SupportedVersions, []int32{1}) {
		t.Fatalf("Plugin-Host 协议 v1 兼容集漂移: %v", protocol.SupportedVersions)
	}
	if pluginv1.ManifestSchemaURL != "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.plugin.schema.json" ||
		pluginv1.DescriptorSchemaURL != "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.descriptor.schema.json" ||
		pluginv1.ArtifactSchemaURL != "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.artifact.schema.json" {
		t.Fatal("插件 v1 Schema 稳定标识发生破坏性变化")
	}

	assertStringSet(t, "Backend 公开扩展点", extpoint.BackendPluginPoints(), []string{
		"agent", "api.route", "event.sink", "hook", "permission.checker", "runner.capability", "tool.package",
	})
	assertStringSet(t, "内核稳定错误码", errorcode.KernelCodes(), []string{
		"capability.not_found", "hook.aborted", "hostcall.failed", "kernel.service_error",
		"permission.denied", "plugin.handler_error", "plugin.inactive",
		"remote.invalid_response", "remote.invoke_failed", "remote.stream_failed",
		"wire.invalid_request", "wire.target_mismatch",
	})

	assertContractV1(t)
	assertPluginHostV1(t)
	assertAddressingV1(t)
}

func assertContractV1(t *testing.T) {
	t.Helper()
	fd := contractv1.File_contract_v1_contract_proto
	assertPackage(t, fd, "vastplan.contract.v1")
	assertMessages(t, fd, map[string]map[string]fieldSpec{
		"Principal": {
			"user_id": {1, protoreflect.StringKind}, "username": {2, protoreflect.StringKind},
			"is_admin": {3, protoreflect.BoolKind}, "tenant_id": {4, protoreflect.StringKind},
			"system_roles": {5, protoreflect.StringKind}, "project_roles": {6, protoreflect.MessageKind},
			"session_id": {7, protoreflect.StringKind},
		},
		"Caller": {
			"kind": {1, protoreflect.EnumKind}, "id": {2, protoreflect.StringKind},
		},
		"Trace": {
			"trace_id": {1, protoreflect.StringKind}, "span_id": {2, protoreflect.StringKind},
			"parent_span_id": {3, protoreflect.StringKind},
		},
		"CredentialRef": {
			"name": {1, protoreflect.StringKind}, "scope": {2, protoreflect.StringKind},
		},
		"CallContext": {
			"principal": {1, protoreflect.MessageKind}, "caller": {2, protoreflect.MessageKind},
			"scene": {3, protoreflect.StringKind}, "tenant_id": {4, protoreflect.StringKind},
			"project_id": {5, protoreflect.StringKind}, "trace": {6, protoreflect.MessageKind},
			"deadline_unix_ms": {7, protoreflect.Int64Kind}, "credentials": {8, protoreflect.MessageKind},
			"idempotency_key": {9, protoreflect.StringKind}, "metadata": {10, protoreflect.MessageKind},
		},
		"CallTarget": {
			"extension_point": {1, protoreflect.StringKind}, "capability": {2, protoreflect.StringKind},
			"version": {3, protoreflect.StringKind}, "operation": {4, protoreflect.StringKind},
			"payload_schema": {5, protoreflect.StringKind},
		},
		"Error": {
			"code": {1, protoreflect.StringKind}, "message": {2, protoreflect.StringKind},
			"retryable": {3, protoreflect.BoolKind}, "details": {4, protoreflect.MessageKind},
		},
		"Usage": {
			"duration_ms": {1, protoreflect.Int64Kind}, "tokens": {2, protoreflect.Int64Kind},
			"cost": {3, protoreflect.DoubleKind}, "custom": {4, protoreflect.MessageKind},
		},
		"CallResult": {
			"status": {1, protoreflect.EnumKind}, "error": {2, protoreflect.MessageKind},
			"usage": {3, protoreflect.MessageKind}, "warnings": {4, protoreflect.StringKind},
			"metadata": {5, protoreflect.MessageKind},
		},
		"CallEvent": {
			"id": {1, protoreflect.StringKind}, "type": {2, protoreflect.StringKind},
			"source": {3, protoreflect.StringKind}, "subject": {4, protoreflect.StringKind},
			"occurred_at_unix_ms": {5, protoreflect.Int64Kind}, "tenant_id": {6, protoreflect.StringKind},
			"trace": {7, protoreflect.MessageKind}, "principal_ref": {8, protoreflect.StringKind},
			"payload": {9, protoreflect.BytesKind},
		},
	})
	assertEnum(t, fd.Enums().ByName("CallerKind"), map[string]protoreflect.EnumNumber{
		"CALLER_KIND_UNSPECIFIED": 0, "CALLER_KIND_USER": 1, "CALLER_KIND_AGENT": 2,
		"CALLER_KIND_PLUGIN": 3, "CALLER_KIND_SYSTEM": 4, "CALLER_KIND_RUNNER": 5,
	})
	callResult := fd.Messages().ByName("CallResult")
	assertEnum(t, callResult.Enums().ByName("Status"), map[string]protoreflect.EnumNumber{
		"STATUS_UNSPECIFIED": 0, "STATUS_OK": 1, "STATUS_ERROR": 2, "STATUS_PARTIAL": 3,
	})
}

func assertPluginHostV1(t *testing.T) {
	t.Helper()
	fd := pluginhostv1.File_pluginhost_v1_pluginhost_proto
	assertPackage(t, fd, "vastplan.pluginhost.v1")
	assertMessages(t, fd, map[string]map[string]fieldSpec{
		"Hello": {
			"proto_versions": {1, protoreflect.Int32Kind}, "magic": {2, protoreflect.StringKind},
			"plugin_id": {3, protoreflect.StringKind}, "plugin_version": {4, protoreflect.StringKind},
			"engines": {5, protoreflect.MessageKind}, "launch_token": {6, protoreflect.StringKind},
		},
		"HelloAck": {
			"negotiated_proto": {1, protoreflect.Int32Kind}, "session_id": {2, protoreflect.StringKind},
			"host_capabilities": {3, protoreflect.StringKind},
		},
		"Contribution": {
			"extension_point": {1, protoreflect.StringKind}, "id": {2, protoreflect.StringKind},
			"priority": {3, protoreflect.Int32Kind}, "descriptor_json": {4, protoreflect.BytesKind},
		},
		"Declaration": {"contributions": {1, protoreflect.MessageKind}},
		"Registered": {
			"accepted": {1, protoreflect.StringKind}, "rejected": {2, protoreflect.MessageKind},
		},
		"InvokeRequest": {
			"request_id": {1, protoreflect.StringKind}, "target": {2, protoreflect.MessageKind},
			"context": {3, protoreflect.MessageKind}, "payload": {4, protoreflect.BytesKind},
		},
		"InvokeResponse": {
			"request_id": {1, protoreflect.StringKind}, "result": {2, protoreflect.MessageKind},
			"payload": {3, protoreflect.BytesKind},
		},
		"EventEnvelope": {"event": {1, protoreflect.MessageKind}},
		"Lifecycle": {
			"request_id": {1, protoreflect.StringKind}, "op": {2, protoreflect.EnumKind},
		},
		"LifecycleAck": {
			"request_id": {1, protoreflect.StringKind}, "ready": {2, protoreflect.BoolKind},
			"message": {3, protoreflect.StringKind},
		},
		"Ping": {"request_id": {1, protoreflect.StringKind}},
		"Pong": {"request_id": {1, protoreflect.StringKind}},
		"FromPlugin": {
			"declare": {1, protoreflect.MessageKind}, "invoke_result": {2, protoreflect.MessageKind},
			"host_call": {3, protoreflect.MessageKind}, "event": {4, protoreflect.MessageKind},
			"lifecycle_ack": {5, protoreflect.MessageKind}, "pong": {6, protoreflect.MessageKind},
		},
		"FromHost": {
			"registered": {1, protoreflect.MessageKind}, "invoke": {2, protoreflect.MessageKind},
			"host_call_result": {3, protoreflect.MessageKind}, "event": {4, protoreflect.MessageKind},
			"lifecycle": {5, protoreflect.MessageKind}, "ping": {6, protoreflect.MessageKind},
		},
	})
	lifecycle := fd.Messages().ByName("Lifecycle")
	assertEnum(t, lifecycle.Enums().ByName("Op"), map[string]protoreflect.EnumNumber{
		"OP_UNSPECIFIED": 0, "OP_ACTIVATE": 1, "OP_DEACTIVATE": 2, "OP_DRAIN": 3, "OP_SHUTDOWN": 4,
	})
	assertMethod(t, fd, "PluginHost", "Handshake", false, false)
	assertMethod(t, fd, "PluginHost", "Channel", true, true)
}

func assertAddressingV1(t *testing.T) {
	t.Helper()
	fd := addressingv1.File_addressing_v1_addressing_proto
	assertPackage(t, fd, "vastplan.addressing.v1")
	assertMessages(t, fd, map[string]map[string]fieldSpec{
		"InvokeRequest": {
			"request_id": {1, protoreflect.StringKind}, "target": {2, protoreflect.MessageKind},
			"context": {3, protoreflect.MessageKind}, "payload": {4, protoreflect.BytesKind},
		},
		"InvokeResponse": {
			"request_id": {1, protoreflect.StringKind}, "result": {2, protoreflect.MessageKind},
			"payload": {3, protoreflect.BytesKind}, "transport_error": {4, protoreflect.MessageKind},
		},
		"TransportError": {
			"code": {1, protoreflect.StringKind}, "message": {2, protoreflect.StringKind},
			"retryable": {3, protoreflect.BoolKind},
		},
		"EventEnvelope": {
			"context": {1, protoreflect.MessageKind}, "event": {2, protoreflect.MessageKind},
		},
		"StreamOpen": {
			"target": {1, protoreflect.MessageKind}, "context": {2, protoreflect.MessageKind},
			"initial_payload": {3, protoreflect.BytesKind},
		},
		"StreamPayload": {"data": {1, protoreflect.BytesKind}},
		"StreamCancel":  {"reason": {1, protoreflect.StringKind}},
		"StreamResult": {
			"result": {1, protoreflect.MessageKind}, "payload": {2, protoreflect.BytesKind},
			"transport_error": {3, protoreflect.MessageKind},
		},
		"StreamFrame": {
			"request_id": {1, protoreflect.StringKind}, "sequence": {2, protoreflect.Uint64Kind},
			"open": {3, protoreflect.MessageKind}, "payload": {4, protoreflect.MessageKind},
			"end": {5, protoreflect.MessageKind}, "cancel": {6, protoreflect.MessageKind},
			"result": {7, protoreflect.MessageKind},
		},
	})
	assertMethod(t, fd, "CapabilityStream", "Open", true, true)
}

func assertMessages(t *testing.T, fd protoreflect.FileDescriptor, baseline map[string]map[string]fieldSpec) {
	t.Helper()
	for messageName, fields := range baseline {
		message := fd.Messages().ByName(protoreflect.Name(messageName))
		if message == nil {
			t.Errorf("%s: 已发布 message %s 被删除", fd.Path(), messageName)
			continue
		}
		for fieldName, want := range fields {
			field := message.Fields().ByName(protoreflect.Name(fieldName))
			if field == nil {
				t.Errorf("%s.%s: 已发布字段 %s 被删除或改名", fd.Package(), messageName, fieldName)
				continue
			}
			if field.Number() != want.number || field.Kind() != want.kind {
				t.Errorf("%s.%s.%s 兼容性破坏: got=(%d,%s) want=(%d,%s)",
					fd.Package(), messageName, fieldName, field.Number(), field.Kind(), want.number, want.kind)
			}
		}
	}
}

func assertEnum(t *testing.T, enum protoreflect.EnumDescriptor, baseline map[string]protoreflect.EnumNumber) {
	t.Helper()
	if enum == nil {
		t.Fatal("已发布 enum 被删除")
	}
	for name, number := range baseline {
		value := enum.Values().ByName(protoreflect.Name(name))
		if value == nil || value.Number() != number {
			t.Errorf("enum %s.%s 兼容性破坏: got=%v want=%d", enum.FullName(), name, value, number)
		}
	}
}

func assertMethod(t *testing.T, fd protoreflect.FileDescriptor, serviceName, methodName string, clientStream, serverStream bool) {
	t.Helper()
	service := fd.Services().ByName(protoreflect.Name(serviceName))
	if service == nil {
		t.Fatalf("%s: 已发布 service %s 被删除", fd.Path(), serviceName)
	}
	method := service.Methods().ByName(protoreflect.Name(methodName))
	if method == nil {
		t.Fatalf("%s.%s: 已发布 method %s 被删除", fd.Package(), serviceName, methodName)
	}
	if method.IsStreamingClient() != clientStream || method.IsStreamingServer() != serverStream {
		t.Errorf("%s.%s.%s 流式形态发生破坏性变化", fd.Package(), serviceName, methodName)
	}
}

func assertPackage(t *testing.T, fd protoreflect.FileDescriptor, want protoreflect.FullName) {
	t.Helper()
	if fd.Package() != want {
		t.Fatalf("proto package 漂移: got=%s want=%s", fd.Package(), want)
	}
}

func assertStringSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s 兼容矩阵漂移: got=%v want=%v", name, got, want)
	}
}
