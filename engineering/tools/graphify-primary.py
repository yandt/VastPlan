#!/usr/bin/env python3
"""Build VastPlan's low-noise primary view from Graphify's graph.json.

Graphify deliberately captures a broad corpus. VastPlan's default query graph is
narrower: product code, architecture documentation and compact protocol contract
declarations. Test implementations, generated protobuf boilerplate, query memory
and agent instruction echoes are removed before the graph is used for analysis.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import tempfile
from collections import Counter
from pathlib import Path, PurePosixPath
from typing import Any


POLICY_VERSION = 1
GENERATED_PYTHON_RE = re.compile(r"_pb2(?:_grpc)?\.py$")
TYPESCRIPT_TEST_RE = re.compile(r"\.(?:test|spec)\.(?:[cm]?[jt]sx?)$")


def normalize_source(value: Any) -> str:
    return str(value or "").replace("\\", "/").lstrip("./")


def source_scope(source_file: Any) -> str:
    source = normalize_source(source_file)
    name = PurePosixPath(source).name

    if source.startswith("graphify-out/memory/"):
        return "memory"
    if source in {"AGENTS.md", "CLAUDE.md"}:
        return "agent-guidance"
    if (
        name.endswith("_test.go")
        or TYPESCRIPT_TEST_RE.search(name)
        or "/testdata/" in f"/{source}"
        or "/fixtures/" in f"/{source}"
        or source.startswith("engineering/arch/")
        or source.startswith("engineering/e2e/")
    ):
        return "test"
    if name.endswith(".pb.go") or GENERATED_PYTHON_RE.search(name):
        return "generated"
    if source.startswith("docs/") or name.endswith((".md", ".mdx", ".rst", ".txt")):
        return "documentation"
    if not source:
        return "external"
    return "production"


def is_generated_contract_declaration(node: dict[str, Any]) -> bool:
    """Keep exported protobuf declarations, not generated methods/helpers."""
    source = normalize_source(node.get("source_file"))
    label = str(node.get("label") or "")
    return (
        source.endswith(".pb.go")
        and bool(label)
        and label[0].isupper()
        and "." not in label
        and "(" not in label
    )


def keep_node(node: dict[str, Any]) -> bool:
    scope = source_scope(node.get("source_file"))
    if scope == "generated":
        return is_generated_contract_declaration(node)
    return scope not in {"test", "memory", "agent-guidance"}


def tag_node(node: dict[str, Any]) -> dict[str, Any]:
    tagged = dict(node)
    scope = source_scope(tagged.get("source_file"))
    tagged["source_scope"] = (
        "generated-contract" if scope == "generated" else scope
    )
    return tagged


def tag_edge(edge: dict[str, Any]) -> dict[str, Any]:
    tagged = dict(edge)
    scope = source_scope(tagged.get("source_file"))
    tagged["source_scope"] = (
        "generated-contract" if scope == "generated" else scope
    )
    tagged["direction_semantics"] = "source-to-target"
    return tagged


def compact_graph(raw: dict[str, Any]) -> tuple[dict[str, Any], dict[str, Any]]:
    nodes = [node for node in raw.get("nodes", []) if isinstance(node, dict)]
    links_key = "links" if "links" in raw else "edges"
    links = [edge for edge in raw.get(links_key, []) if isinstance(edge, dict)]

    before_node_scopes = Counter(source_scope(n.get("source_file")) for n in nodes)
    before_edge_scopes = Counter(source_scope(e.get("source_file")) for e in links)

    kept_nodes = [tag_node(node) for node in nodes if keep_node(node)]
    kept_ids = {str(node.get("id")) for node in kept_nodes if node.get("id") is not None}

    kept_links: list[dict[str, Any]] = []
    for edge in links:
        source = str(edge.get("source"))
        target = str(edge.get("target"))
        if source not in kept_ids or target not in kept_ids:
            continue
        if source_scope(edge.get("source_file")) in {
            "test",
            "memory",
            "agent-guidance",
            "generated",
        }:
            continue
        kept_links.append(tag_edge(edge))

    # Unresolved/external symbols are useful only when retained code or docs
    # actually reference them. Remove isolated extractor stubs after edge pruning.
    incident_ids = {
        str(endpoint)
        for edge in kept_links
        for endpoint in (edge.get("source"), edge.get("target"))
    }
    kept_nodes = [
        node
        for node in kept_nodes
        if node.get("source_scope") != "external" or str(node.get("id")) in incident_ids
    ]
    kept_ids = {str(node.get("id")) for node in kept_nodes if node.get("id") is not None}
    kept_links = [
        edge
        for edge in kept_links
        if str(edge.get("source")) in kept_ids and str(edge.get("target")) in kept_ids
    ]

    graph_meta = dict(raw.get("graph") or {})
    hyperedges = []
    for item in graph_meta.get("hyperedges", []):
        if not isinstance(item, dict):
            continue
        if source_scope(item.get("source_file")) in {
            "test",
            "memory",
            "agent-guidance",
            "generated",
        }:
            continue
        members = item.get("nodes", [])
        if isinstance(members, list) and all(str(member) in kept_ids for member in members):
            tagged = dict(item)
            tagged["source_scope"] = source_scope(tagged.get("source_file"))
            hyperedges.append(tagged)
    graph_meta["hyperedges"] = hyperedges

    after_node_scopes = Counter(node.get("source_scope") for node in kept_nodes)
    after_edge_scopes = Counter(edge.get("source_scope") for edge in kept_links)
    diagnostics = {
        "policy_version": POLICY_VERSION,
        "nodes_before": len(nodes),
        "nodes_after": len(kept_nodes),
        "links_before": len(links),
        "links_after": len(kept_links),
        "node_scopes_before": dict(sorted(before_node_scopes.items())),
        "edge_scopes_before": dict(sorted(before_edge_scopes.items())),
        "node_scopes_after": dict(sorted(after_node_scopes.items())),
        "edge_scopes_after": dict(sorted(after_edge_scopes.items())),
        "hyperedges_after": len(hyperedges),
    }
    graph_meta["vastplan_primary"] = {
        "policy_version": POLICY_VERSION,
        "purpose": "product architecture and runtime call-chain navigation",
        "excluded_scopes": ["test", "memory", "agent-guidance", "generated-boilerplate"],
        "direction_semantics": (
            "links store source-to-target direction; the graph stays undirected "
            "for broad natural-language traversal"
        ),
        "diagnostics": diagnostics,
    }

    result = dict(raw)
    result["directed"] = False
    result["multigraph"] = False
    result["graph"] = graph_meta
    result["nodes"] = kept_nodes
    result[links_key] = kept_links
    if links_key == "links":
        result.pop("edges", None)
    else:
        result.pop("links", None)
    return result, diagnostics


def validate_primary(raw: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    nodes = [node for node in raw.get("nodes", []) if isinstance(node, dict)]
    links = raw.get("links", raw.get("edges", []))
    ids = {str(node.get("id")) for node in nodes if node.get("id") is not None}

    if (raw.get("graph") or {}).get("vastplan_primary", {}).get("policy_version") != POLICY_VERSION:
        errors.append("missing or stale vastplan_primary policy metadata")
    for node in nodes:
        if not keep_node(node):
            errors.append(f"excluded node remains: {node.get('id')}")
        if not node.get("source_scope"):
            errors.append(f"node lacks source_scope: {node.get('id')}")
    for edge in links:
        if str(edge.get("source")) not in ids or str(edge.get("target")) not in ids:
            errors.append(f"dangling edge: {edge.get('source')} -> {edge.get('target')}")
        if not edge.get("source_scope"):
            errors.append(f"edge lacks source_scope: {edge.get('source')} -> {edge.get('target')}")
        if edge.get("direction_semantics") != "source-to-target":
            errors.append(f"edge lacks direction semantics: {edge.get('source')} -> {edge.get('target')}")
    return errors


def atomic_write(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temp_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, ensure_ascii=False, indent=2)
            handle.write("\n")
        os.replace(temp_name, path)
    except Exception:
        try:
            os.unlink(temp_name)
        except FileNotFoundError:
            pass
        raise


def reuse_baseline_if_same_result(
    graph: dict[str, Any], diagnostics: dict[str, Any], diagnostics_path: Path
) -> dict[str, Any]:
    """Keep the pre-compaction baseline across the post-cluster metadata pass."""
    if not diagnostics_path.exists():
        return diagnostics
    try:
        previous = json.loads(diagnostics_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return diagnostics
    if (
        previous.get("policy_version") == POLICY_VERSION
        and previous.get("nodes_after") == diagnostics.get("nodes_after")
        and previous.get("links_after") == diagnostics.get("links_after")
        and previous.get("nodes_before", 0) >= diagnostics.get("nodes_before", 0)
        and previous.get("links_before", 0) >= diagnostics.get("links_before", 0)
    ):
        graph["graph"]["vastplan_primary"]["diagnostics"] = previous
        return previous
    return diagnostics


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--graph",
        type=Path,
        default=Path("graphify-out/graph.json"),
        help="Graphify JSON to optimize in place",
    )
    parser.add_argument(
        "--diagnostics",
        type=Path,
        default=Path("graphify-out/.vastplan-primary.json"),
        help="Local diagnostics output",
    )
    parser.add_argument("--check", action="store_true", help="Validate without writing")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    raw = json.loads(args.graph.read_text(encoding="utf-8"))
    if args.check:
        errors = validate_primary(raw)
        if errors:
            for error in errors[:20]:
                print(f"ERROR: {error}")
            if len(errors) > 20:
                print(f"ERROR: {len(errors) - 20} additional validation error(s)")
            return 1
        profile = (raw.get("graph") or {}).get("vastplan_primary", {})
        print(json.dumps(profile.get("diagnostics", {}), ensure_ascii=False, indent=2))
        return 0

    compacted, diagnostics = compact_graph(raw)
    diagnostics = reuse_baseline_if_same_result(compacted, diagnostics, args.diagnostics)
    errors = validate_primary(compacted)
    if errors:
        raise ValueError("; ".join(errors[:5]))
    atomic_write(args.graph, compacted)
    atomic_write(args.diagnostics, diagnostics)
    print(json.dumps(diagnostics, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
