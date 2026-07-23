import queue
import threading
import unittest

from vastplan_subinterpreter import Contribution, InvocationContext, Plugin, call_ok
from vastplan_subinterpreter import plugin as bridge_module


class SubinterpreterSDKTest(unittest.TestCase):
    def test_contribution_is_pure_python_wire_data(self):
        contribution = Contribution(
            "tool.package", "example.subinterpreter", b"{}", handlers={"echo": lambda *_: call_ok(b"ok")}
        )
        self.assertEqual(contribution.wire()["extension_point"], "tool.package")
        self.assertEqual(contribution.wire()["operations"], ("echo",))

    def test_invocation_context_deadline(self):
        context = InvocationContext(1, "opaque")
        self.assertEqual(context.delegation_token, "opaque")
        self.assertTrue(context.cancelled)
        with self.assertRaises(TimeoutError):
            context.raise_if_cancelled()

    def test_unmanaged_serve_is_rejected(self):
        plugin = Plugin("cn.example.plugin", "1.0.0", {"backend": "^1.0"})
        with self.assertRaises(RuntimeError):
            plugin.serve()

    def test_host_call_round_trips_through_parent_invocation(self):
        old_requests, old_responses = bridge_module._requests, bridge_module._responses
        bridge_module._requests = queue.Queue()
        bridge_module._responses = queue.Queue()
        try:
            plugin = Plugin("cn.example.plugin", "1.0.0", {"backend": "^1.0"})

            def handler(_invocation, host, context, _payload):
                result, payload = host.call(
                    {"extension_point": "configuration.scoped-resolver", "capability": "configuration.scoped", "operation": "resolve"},
                    context,
                    b"{}",
                    timeout=1.0,
                )
                self.assertEqual(result["status"], "ok")
                return call_ok(payload)

            plugin.contribute(Contribution("tool.package", "example.subinterpreter", b"{}", handlers={"resolve": handler}))
            worker = threading.Thread(target=plugin._invoke, args=({
                "type": "invoke",
                "request_id": "parent-1",
                "extension_point": "tool.package",
                "capability": "example.subinterpreter",
                "operation": "resolve",
                "context": {"tenant_id": "tenant-a"},
                "payload": b"",
            },))
            worker.start()

            host_call = bridge_module._responses.get(timeout=1.0)
            self.assertEqual(host_call["type"], "host_call")
            self.assertEqual(host_call["request_id"], "parent-1")
            self.assertNotIn("delegation_token", host_call)
            bridge_module._requests.put({
                "type": "host_call_result",
                "host_call_id": host_call["host_call_id"],
                "result": {"status": "ok", "metadata": {}},
                "payload": b'{"source":"active"}',
            })
            completed = bridge_module._responses.get(timeout=1.0)
            worker.join(timeout=1.0)

            self.assertFalse(worker.is_alive())
            self.assertEqual(completed["type"], "result")
            self.assertEqual(completed["status"], "ok")
            self.assertEqual(completed["payload"], b'{"source":"active"}')
        finally:
            bridge_module._requests, bridge_module._responses = old_requests, old_responses


if __name__ == "__main__":
    unittest.main()
