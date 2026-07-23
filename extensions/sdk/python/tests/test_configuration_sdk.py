import json
from pathlib import Path
import unittest

from contract.v1 import contract_pb2
from vastplan_plugin import ManagedCredentialRef, ScopedConfigurationClient, managed_credential_refs
from vastplan_plugin.scoped_configuration import digest_scoped_values, parse_scoped_resolution


VECTOR = json.loads((Path(__file__).resolve().parents[4] / "contracts" / "testdata" / "sdk-interop-v1.json").read_text(encoding="utf-8"))


class FakePlugin:
    def __init__(self, resolution):
        self.resolution = resolution
        self.calls = []

    def call(self, target, context, payload, timeout=30.0):
        self.calls.append((target, context, payload, timeout))
        if target["operation"] == "resolve":
            body = self.resolution
        else:
            body = {
                "protocol": "configuration.scoped.v1",
                "configurationId": self.resolution["configurationId"],
                "changed": False,
                "revision": self.resolution["revision"],
                "digest": self.resolution["digest"],
                "observedAt": "2026-07-23T00:00:01Z",
            }
        return contract_pb2.CallResult(status=contract_pb2.CallResult.STATUS_OK), json.dumps(body, separators=(",", ":")).encode()


class ConfigurationSDKTest(unittest.TestCase):
    def setUp(self):
        self.resolution = {
            "protocol": "configuration.scoped.v1",
            "configurationId": "cfg_" + "a" * 24,
            "scope": "tenant",
            "revision": 0,
            "digest": VECTOR["scopedValuesDigest"],
            "schemaDigest": "b" * 64,
            "artifactSha256": "c" * 64,
            "values": VECTOR["scopedValues"],
            "source": "seed",
            "observedAt": "2026-07-23T00:00:00Z",
        }

    def test_managed_credential_ref_uses_shared_closed_vector(self):
        ref = ManagedCredentialRef.from_mapping(VECTOR["managedCredentialRef"], ("tenant",))
        self.assertEqual(dict(ref.as_dict()), VECTOR["managedCredentialRef"])
        self.assertEqual(managed_credential_refs({"token": VECTOR["managedCredentialRef"]})["token"], ref)
        with self.assertRaises(ValueError):
            ManagedCredentialRef.from_mapping({**VECTOR["managedCredentialRef"], "plaintext": "secret"})

    def test_scoped_digest_and_parser_match_go_node_vector(self):
        self.assertEqual(digest_scoped_values(VECTOR["scopedValues"]), VECTOR["scopedValuesDigest"])
        parsed = parse_scoped_resolution(json.dumps(self.resolution).encode())
        self.assertEqual(parsed.digest, VECTOR["scopedValuesDigest"])
        with self.assertRaises(ValueError):
            parse_scoped_resolution(json.dumps({**self.resolution, "tenantId": "forged"}).encode())

    def test_client_calls_only_identity_free_resolver_operations(self):
        plugin = FakePlugin(self.resolution)
        client = ScopedConfigurationClient(plugin)
        context = contract_pb2.CallContext(tenant_id="trusted-context")
        client.resolve(context)
        client.watch_revision(context, 0, VECTOR["scopedValuesDigest"], 1000)
        self.assertEqual([call[0]["operation"] for call in plugin.calls], ["resolve", "watchRevision"])
        self.assertEqual(plugin.calls[0][2], b"{}")
        self.assertNotIn(b"tenant", plugin.calls[1][2])


if __name__ == "__main__":
    unittest.main()
