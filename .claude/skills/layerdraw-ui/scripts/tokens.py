#!/usr/bin/env python3
"""Flatten brand/tokens.json into a greppable cheat sheet.

Usage:
  python3 tokens.py                       # all primitives + light theme
  python3 tokens.py --theme dark          # dark theme semantics
  python3 tokens.py --grep action         # filter rows
  python3 tokens.py --resolve             # resolve {token.path} references

Standard library only. No network access.
"""
import argparse
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve()
for parent in ROOT.parents:
    if (parent / "brand" / "tokens.json").exists():
        TOKENS = parent / "brand" / "tokens.json"
        break
else:
    sys.exit("brand/tokens.json not found above this script")


def flatten(node, path, out):
    if isinstance(node, dict):
        if "$value" in node:
            out[path] = node["$value"]
            return
        for key, value in node.items():
            if key.startswith("$"):
                continue
            flatten(value, f"{path}.{key}" if path else key, out)


def resolve(value, table, depth=0):
    if depth > 8:
        return value
    if isinstance(value, str) and value.startswith("{") and value.endswith("}"):
        return resolve(table.get(value[1:-1], value), table, depth + 1)
    return value


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--theme", choices=["light", "dark", "highContrast"], default="light")
    parser.add_argument("--grep", default=None)
    parser.add_argument("--resolve", action="store_true")
    args = parser.parse_args()

    data = json.loads(TOKENS.read_text())
    table = {}
    for section in ("color", "font", "typography", "radius", "space", "shadow", "motion", "dimension", "component"):
        if section in data:
            flatten(data[section], section, table)
    theme = data.get("theme", {}).get(args.theme, {})
    flatten(theme, f"theme.{args.theme}", table)

    for path in sorted(table):
        if not path.startswith(("theme.", "color.", "font.", "typography.", "radius.", "space.", "shadow.", "motion.", "dimension.", "component.")):
            continue
        if args.grep and args.grep.lower() not in path.lower():
            continue
        value = table[path]
        if args.resolve:
            value = resolve(value, table)
        print(f"{path} = {value}")


if __name__ == "__main__":
    main()
