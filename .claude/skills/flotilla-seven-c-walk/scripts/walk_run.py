#!/usr/bin/env python3
"""Fail-soft Seven-C capture runner.

Selectors and routes belong to a versioned manifest. An absent optional state is
recorded as unavailable; it never aborts later captures or prevents the final
run manifest from being written.
"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import sys
import tempfile
from typing import Any
from urllib.parse import urljoin


def load_json(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as handle:
        value = json.load(handle)
    if not isinstance(value, dict):
        raise ValueError(f"{path}: top level must be an object")
    return value


def validate_manifest(manifest: dict[str, Any]) -> None:
    if manifest.get("schema") != 1:
        raise ValueError("walk manifest schema must be 1")
    captures = manifest.get("captures")
    if not isinstance(captures, list) or not captures:
        raise ValueError("walk manifest must contain captures")
    seen: set[str] = set()
    for capture in captures:
        capture_id = capture.get("id")
        if not isinstance(capture_id, str) or re.fullmatch(r"[a-z0-9][a-z0-9-]*", capture_id) is None:
            raise ValueError("every capture id must be a lowercase path-safe slug")
        if capture_id in seen:
            raise ValueError(f"duplicate capture id: {capture_id}")
        seen.add(capture_id)
        attempts = capture.get("attempts")
        if not isinstance(attempts, list) or not attempts:
            raise ValueError(f"{capture_id}: attempts must be non-empty")
        for attempt in attempts:
            route = attempt.get("route")
            if not isinstance(route, str) or not route.startswith("/") or route.startswith("//"):
                raise ValueError(f"{capture_id}: every attempt needs a same-origin absolute route")
            anchors = attempt.get("anchors")
            if not isinstance(anchors, list) or not anchors:
                raise ValueError(f"{capture_id}: every attempt needs anchors")
            for anchor in anchors:
                if not isinstance(anchor.get("selector"), str) or not isinstance(anchor.get("state"), str):
                    raise ValueError(f"{capture_id}: anchors need selector and state")


def validate_output_dir(output_dir: Path) -> None:
    parts = output_dir.resolve().parts
    if "parades" in parts:
        raise ValueError("walk evidence must never be written under parades/")
    if "state" not in parts:
        raise ValueError("walk output must live beneath the roster state/ tree")


def atomic_json(path: Path, value: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(
        mode="w", encoding="utf-8", dir=path.parent, prefix=f".{path.name}.", delete=False
    ) as handle:
        json.dump(value, handle, indent=2, sort_keys=True)
        handle.write("\n")
        temp_name = handle.name
    os.replace(temp_name, path)


class FixtureProbe:
    """No-browser probe used to lock route/selector recovery in CI."""

    def __init__(self, fixture: dict[str, Any]):
        self.routes = fixture.get("routes", {})

    def attempt(self, spec: dict[str, Any], _capture_path: Path) -> dict[str, Any] | None:
        visible = self.routes.get(spec["route"], {})
        for anchor in spec["anchors"]:
            if visible.get(anchor["selector"], False):
                return {
                    "route": spec["route"],
                    "selector": anchor["selector"],
                    "state": anchor["state"],
                    "screenshot": None,
                }
        return None

    def close(self) -> None:
        return


class PlaywrightProbe:
    def __init__(self, base_url: str, viewport: dict[str, int], timeout_ms: int):
        try:
            from playwright.sync_api import sync_playwright
        except ImportError as error:
            raise RuntimeError(
                "Playwright is required for live captures; use the throwaway-venv recipe in SKILL.md"
            ) from error
        self.base_url = base_url.rstrip("/") + "/"
        self.timeout_ms = timeout_ms
        self.playwright = sync_playwright().start()
        self.browser = self.playwright.chromium.launch()
        self.page = self.browser.new_page(viewport=viewport)
        self.page_errors: list[str] = []
        self.page.on("pageerror", lambda error: self.page_errors.append(str(error)))

    def attempt(self, spec: dict[str, Any], capture_path: Path) -> dict[str, Any] | None:
        self.page_errors.clear()
        self.page.goto(urljoin(self.base_url, spec["route"].lstrip("/")), wait_until="domcontentloaded")
        for selector in spec.get("actions", []):
            action = self.page.locator(selector)
            action.wait_for(state="visible", timeout=self.timeout_ms)
            action.click()
        for anchor in spec["anchors"]:
            locator = self.page.locator(anchor["selector"]).first
            try:
                locator.wait_for(state="visible", timeout=self.timeout_ms)
            except Exception:
                continue
            if self.page_errors:
                raise RuntimeError(f"page errors: {len(self.page_errors)}")
            self.page.screenshot(path=str(capture_path), full_page=True)
            return {
                "route": spec["route"],
                "selector": anchor["selector"],
                "state": anchor["state"],
                "screenshot": capture_path.name,
            }
        return None

    def close(self) -> None:
        self.browser.close()
        self.playwright.stop()


def run(
    manifest: dict[str, Any],
    probe: FixtureProbe | PlaywrightProbe,
    output_dir: Path,
) -> tuple[dict[str, Any], int]:
    results: list[dict[str, Any]] = []
    failures = 0
    try:
        for capture in manifest["captures"]:
            result: dict[str, Any] | None = None
            attempt_errors: list[str] = []
            for attempt in capture["attempts"]:
                try:
                    result = probe.attempt(attempt, output_dir / f"{capture['id']}-390.png")
                except Exception as error:
                    # Do not include page content, URLs with hosts, or exception reprs in
                    # the durable manifest. Route + error class are enough to diagnose.
                    attempt_errors.append(f"{attempt['route']}: {type(error).__name__}")
                if result is not None:
                    break
            if result is None:
                required = bool(capture.get("required", False))
                outcome = "failed" if attempt_errors else "unavailable"
                failures += int(required or outcome == "failed")
                results.append(
                    {
                        "id": capture["id"],
                        "outcome": outcome,
                        "required": required,
                        "attempts": len(capture["attempts"]),
                        "errors": attempt_errors,
                    }
                )
            else:
                results.append(
                    {
                        "id": capture["id"],
                        "outcome": "captured",
                        "required": bool(capture.get("required", False)),
                        **result,
                    }
                )
    finally:
        probe.close()
        summary = {
            "captured": sum(item["outcome"] == "captured" for item in results),
            "unavailable": sum(item["outcome"] == "unavailable" for item in results),
            "failed": sum(item["outcome"] == "failed" for item in results),
            "failures": failures,
        }
        run_manifest = {
            "schema": 1,
            "source_manifest_schema": manifest["schema"],
            "completed": True,
            "summary": summary,
            "captures": results,
        }
        atomic_json(output_dir / "walk-run.json", run_manifest)
    return run_manifest, int(failures > 0)


def parse_args() -> argparse.Namespace:
    skill_dir = Path(__file__).resolve().parent.parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=skill_dir / "walk-manifest.v1.json")
    parser.add_argument("--base-url", default="http://127.0.0.1:8787")
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--fixture", type=Path, help="probe route/selector availability without a browser")
    parser.add_argument("--timeout-ms", type=int, default=2500)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    manifest = load_json(args.manifest)
    validate_manifest(manifest)
    validate_output_dir(args.output_dir)
    args.output_dir.mkdir(parents=True, exist_ok=True)
    if args.fixture:
        probe: FixtureProbe | PlaywrightProbe = FixtureProbe(load_json(args.fixture))
    else:
        probe = PlaywrightProbe(args.base_url, manifest["viewport"], args.timeout_ms)
    result, status = run(manifest, probe, args.output_dir)
    print(json.dumps(result["summary"], sort_keys=True))
    return status


if __name__ == "__main__":
    sys.exit(main())
