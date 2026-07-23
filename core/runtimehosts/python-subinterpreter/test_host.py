import importlib.util
import json
import queue
import subprocess
import sys
import threading
import time
import unittest
from pathlib import Path


HOST_PATH = Path(__file__).with_name("host.py")
SPEC = importlib.util.spec_from_file_location("vastplan_python_subinterpreter_host", HOST_PATH)
HOST = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HOST)


class RuntimeHostTest(unittest.TestCase):
    def test_probe_is_machine_readable_and_truthful(self):
        completed = subprocess.run(
            [sys.executable, str(HOST_PATH), "--probe"], check=True, capture_output=True, text=True
        )
        capability = json.loads(completed.stdout)
        self.assertEqual(capability["runtime"], "python-subinterpreter")
        self.assertEqual(capability["supported"], sys.implementation.name == "cpython" and sys.version_info >= (3, 14))

    def test_missing_entry_is_rejected(self):
        with self.assertRaises(SystemExit):
            HOST.parse_arguments(())

    def test_plugin_arguments_are_preserved_after_separator(self):
        arguments = HOST.parse_arguments(("--entry", "plugin.py", "--", "--tenant", "acme"))
        self.assertEqual(arguments.plugin_args, ("--tenant", "acme"))

    def test_pool_mode_does_not_require_a_plugin_entry(self):
        arguments = HOST.parse_arguments(("--pool",))
        self.assertTrue(arguments.pool)
        self.assertIsNone(arguments.entry)

    def test_host_call_is_executed_on_invocation_thread(self):
        from contract.v1 import contract_pb2

        requests = queue.Queue()
        responses = queue.Queue()
        bridge = HOST.SubinterpreterBridge(requests, responses)
        bridge.start()

        class Invocation:
            deadline_unix_ms = int(time.time() * 1000) + 5_000
            delegation_token = "opaque-host-token"

            @staticmethod
            def raise_if_cancelled():
                return None

        class OuterHost:
            call_thread = None

            def call(self, target, context, payload, timeout, cancelled=None):
                self.call_thread = threading.current_thread()
                self.target = target
                self.context = context
                self.payload = payload
                self.timeout = timeout
                self.cancelled = cancelled
                return contract_pb2.CallResult(status=contract_pb2.CallResult.STATUS_OK), b'{"resolved":true}'

        outer_host = OuterHost()
        outcome = {}

        def invoke():
            outcome["value"] = bridge.invoke(
                Invocation(), outer_host, contract_pb2.CallContext(tenant_id="tenant-a"), b"request",
                {"extension_point": "tool.package", "capability": "example", "operation": "resolve"},
            )

        invocation_thread = threading.Thread(target=invoke)
        invocation_thread.start()
        parent = requests.get(timeout=1.0)
        self.assertNotIn("delegation_token", parent)
        responses.put({
            "type": "host_call",
            "request_id": parent["request_id"],
            "host_call_id": "hc-1",
            "target": {"extension_point": "configuration.scoped-resolver", "capability": "configuration.scoped", "operation": "resolve"},
            "context": {"tenant_id": "tenant-a"},
            "payload": b"{}",
            "timeout_ms": 1_000,
        })
        host_call_result = requests.get(timeout=1.0)
        self.assertEqual(host_call_result["type"], "host_call_result")
        self.assertEqual(host_call_result["result"]["status"], "ok")
        self.assertIs(outer_host.call_thread, invocation_thread)
        responses.put({
            "type": "result",
            "request_id": parent["request_id"],
            "status": "ok",
            "metadata": {},
            "payload": b"done",
        })
        invocation_thread.join(timeout=1.0)
        bridge.close()

        self.assertFalse(invocation_thread.is_alive())
        self.assertEqual(outcome["value"][1], b"done")


if __name__ == "__main__":
    unittest.main()
