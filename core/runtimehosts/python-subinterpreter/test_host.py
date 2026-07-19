import importlib.util
import json
import subprocess
import sys
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


if __name__ == "__main__":
    unittest.main()
