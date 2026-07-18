import json
import time
import uuid

from contract.v1 import contract_pb2
from vastplan_plugin import Contribution, Plugin


DESCRIPTOR = json.dumps({
    "title": "Python Hello 工具包",
    "subcommands": [{
        "name": "echo",
        "description": "由 Python 插件回显输入",
        "paramsSchema": {
            "type": "object",
            "properties": {"text": {"type": "string"}},
            "required": ["text"],
        },
    }],
}, ensure_ascii=False).encode()


def echo(context, host, call_context, payload):
    context.raise_if_cancelled()
    value = json.loads(payload or b"{}")
    output = json.dumps({
        "echo": value.get("text", ""),
        "runtime": "python",
        "tenant": call_context.tenant_id,
    }, ensure_ascii=False).encode()
    host.publish_event(contract_pb2.CallEvent(
        id=str(uuid.uuid4()),
        type="python.hello.invoked",
        occurred_at_unix_ms=int(time.time() * 1000),
        tenant_id=call_context.tenant_id,
    ))
    return contract_pb2.CallResult(status=contract_pb2.CallResult.STATUS_OK), output


plugin = Plugin("com.vastplan.python-hello", "0.1.0", {"backend": "^0.1 || ^1.0"})
plugin.contribute(Contribution(
    extension_point="tool.package",
    id="vastplan.python-hello",
    descriptor=DESCRIPTOR,
    handlers={"echo": echo},
))
plugin.serve()
