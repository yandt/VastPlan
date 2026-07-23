import json
import unittest

from contract.v1 import contract_pb2
from vastplan_plugin import SharedStateClient, is_shared_state_conflict


class FakePlugin:
    def __init__(self):
        self.calls = []
        self.result = contract_pb2.CallResult(status=contract_pb2.CallResult.STATUS_OK)

    def call(self, target, context, payload, timeout=30.0):
        self.calls.append((target, context, payload))
        return self.result, json.dumps({"protocol": "state.shared.v1", "key": "active", "value": "e30", "revision": 1, "updatedAt": "2026-07-23T00:00:00Z"}).encode()


class SharedStateSDKTest(unittest.TestCase):
    def test_client_request_omits_trusted_identity(self):
        plugin = FakePlugin()
        client = SharedStateClient(plugin, "tenant", "settings")
        entry = client.create({"tenant_id": "trusted"}, "active", b"{}")
        self.assertEqual(entry.value, b"{}")
        self.assertEqual(plugin.calls[0][0]["capability"], "kernel.state.shared.create")
        request = json.loads(plugin.calls[0][2])
        self.assertEqual(set(request), {"scope", "namespace", "key", "value"})

    def test_conflict_code_is_stable(self):
        plugin = FakePlugin()
        plugin.result = contract_pb2.CallResult(status=contract_pb2.CallResult.STATUS_ERROR, error=contract_pb2.Error(code="state.conflict", message="stale", retryable=True))
        client = SharedStateClient(plugin, "service", "ledger")
        with self.assertRaises(Exception) as caught:
            client.update({}, "active", b"{}", 1)
        self.assertTrue(is_shared_state_conflict(caught.exception))


if __name__ == "__main__":
    unittest.main()
