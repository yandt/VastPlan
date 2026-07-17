import unittest

from contract.v1 import contract_pb2
from vastplan_plugin import Contribution, InvocationContext, Plugin
from vastplan_plugin.plugin import MAX_PAYLOAD_BYTES


class SDKTest(unittest.TestCase):
    def test_contribution_wire_and_local_route(self):
        plugin = Plugin("com.example.python", "1.0.0", {"backend": "^0.1"})
        handler = lambda ctx, host, call_ctx, payload: (contract_pb2.CallResult(status=1), payload)
        contribution = Contribution("tool.package", "example.python", b"{}", handlers={"run": handler})
        plugin.contribute(contribution)
        self.assertEqual(contribution.wire().id, "example.python")
        self.assertIs(plugin._routes[("tool.package", "example.python", "run")], handler)

    def test_invocation_context_cancel(self):
        context = InvocationContext()
        self.assertFalse(context.cancelled)
        context._cancelled.set()
        self.assertTrue(context.cancelled)
        with self.assertRaises(TimeoutError):
            context.raise_if_cancelled()

    def test_feature_gate(self):
        plugin = Plugin("com.example.python", "1.0.0", {"backend": "^0.1"})
        with self.assertRaises(RuntimeError):
            plugin.publish_event(contract_pb2.CallEvent(id="event-1", type="example.event"))

    def test_host_call_payload_limit(self):
        plugin = Plugin("com.example.python", "1.0.0", {"backend": "^0.1"})
        with self.assertRaises(ValueError):
            plugin.call(contract_pb2.CallTarget(capability="x"), contract_pb2.CallContext(),
                        b"x" * (MAX_PAYLOAD_BYTES + 1))


if __name__ == "__main__":
    unittest.main()
