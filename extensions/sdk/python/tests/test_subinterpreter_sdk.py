import unittest

from vastplan_subinterpreter import Contribution, InvocationContext, Plugin, call_ok


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


if __name__ == "__main__":
    unittest.main()
