import json

from vastplan_subinterpreter import Contribution, Plugin, call_ok


def echo(invocation, _host, call_context, payload):
    invocation.raise_if_cancelled()
    value = json.loads(payload or b"{}")
    return call_ok(json.dumps({
        "echo": value.get("text", ""),
        "runtime": "python-subinterpreter",
        "tenant": call_context.get("tenant_id", ""),
    }, ensure_ascii=False).encode())


plugin = Plugin(
    "cn.vastplan.foundation.backend.runtime.python-subinterpreter-hello",
    "0.1.0",
    {"backend": "^0.1 || ^1.0"},
)
plugin.contribute(Contribution(
    extension_point="tool.package",
    id="vastplan.python-subinterpreter-hello",
    descriptor=json.dumps({
        "title": "Python Subinterpreter Hello",
        "subcommands": [{
            "name": "echo",
            "description": "由 CPython 子解释器回显输入",
            "paramsSchema": {
                "type": "object",
                "properties": {"text": {"type": "string"}},
                "required": ["text"],
            },
        }],
    }, ensure_ascii=False).encode(),
    handlers={"echo": echo},
))
plugin.serve()
