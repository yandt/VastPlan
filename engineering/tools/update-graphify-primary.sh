#!/usr/bin/env bash
set -euo pipefail

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
GRAPHIFY_BIN=${GRAPHIFY_BIN:-graphify}
GRAPH="$ROOT/graphify-out/graph.json"
PYTHON_FILE="$ROOT/graphify-out/.graphify_python"

if [[ -f "$PYTHON_FILE" ]]; then
    PYTHON=$(<"$PYTHON_FILE")
else
    PYTHON=$(command -v python3)
fi

if [[ "${1:-}" != "--optimize-only" ]]; then
    "$GRAPHIFY_BIN" update "$ROOT"
fi

"$PYTHON" "$ROOT/engineering/tools/graphify-primary.py" --graph "$GRAPH"
"$GRAPHIFY_BIN" cluster-only "$ROOT"
"$PYTHON" "$ROOT/engineering/tools/graphify-primary.py" --graph "$GRAPH"
"$PYTHON" "$ROOT/engineering/tools/graphify-primary.py" --graph "$GRAPH" --check
