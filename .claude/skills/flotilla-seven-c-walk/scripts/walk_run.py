#!/usr/bin/env python3
"""Fail-soft Seven-C capture runner.

Selectors and routes belong to a versioned manifest. Each capture records a
canonical loading, populated, partial, empty, or error state. A missing anchor
never aborts later captures or prevents the final run manifest from being written.
"""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import sys
import tempfile
import time
from typing import Any
from urllib.parse import urljoin


def load_json(path: Path) -> dict[str, Any]:
    with path.open(encoding="utf-8") as handle:
        value = json.load(handle)
    if not isinstance(value, dict):
        raise ValueError(f"{path}: top level must be an object")
    return value


STATE_VOCABULARY = {"loading", "populated", "partial", "empty", "error"}


def validate_manifest(manifest: dict[str, Any]) -> None:
    if manifest.get("schema") != 2:
        raise ValueError("walk manifest schema must be 2")
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
            if not isinstance(attempt.get("anchor"), str) or not attempt["anchor"]:
                raise ValueError(f"{capture_id}: every attempt needs a stable anchor")
            states = attempt.get("states")
            if not isinstance(states, list) or not states:
                raise ValueError(f"{capture_id}: every attempt needs states")
            for state in states:
                if not isinstance(state.get("selector"), str) or state.get("state") not in STATE_VOCABULARY:
                    raise ValueError(f"{capture_id}: states need selector and canonical state")
                for field in ("text", "exclude_text"):
                    if field in state:
                        if not isinstance(state[field], str):
                            raise ValueError(f"{capture_id}: {field} must be a regex string")
                        re.compile(state[field])


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
        if not fixture_visible(visible.get(spec["anchor"])):
            return None
        for anchor in spec["states"]:
            fixture = visible.get(anchor["selector"])
            if fixture_visible(fixture) and text_matches(anchor, fixture_text(fixture)):
                return {
                    "route": spec["route"],
                    "anchor": spec["anchor"],
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
        try:
            self.page.locator(spec["anchor"]).first.wait_for(state="visible", timeout=self.timeout_ms)
        except Exception:
            if self.page_errors:
                raise RuntimeError(f"page errors: {len(self.page_errors)}")
            return None
        deadline = time.monotonic() + self.timeout_ms / 1000
        loading: dict[str, Any] | None = None
        while time.monotonic() < deadline:
            for anchor in spec["states"]:
                locator = self.page.locator(anchor["selector"]).first
                if locator.is_visible() and text_matches(anchor, locator.inner_text()):
                    result = {
                        "route": spec["route"], "anchor": spec["anchor"],
                        "selector": anchor["selector"], "state": anchor["state"],
                        "screenshot": capture_path.name,
                    }
                    if anchor["state"] == "loading":
                        loading = result
                    else:
                        if self.page_errors:
                            raise RuntimeError(f"page errors: {len(self.page_errors)}")
                        self.page.screenshot(path=str(capture_path), full_page=True)
                        return result
            self.page.wait_for_timeout(100)
        if loading is not None:
            if self.page_errors:
                raise RuntimeError(f"page errors: {len(self.page_errors)}")
            self.page.screenshot(path=str(capture_path), full_page=True)
            return loading
        if self.page_errors:
            raise RuntimeError(f"page errors: {len(self.page_errors)}")
        return None

    def close(self) -> None:
        self.browser.close()
        self.playwright.stop()


def fixture_visible(value: Any) -> bool:
    return bool(value.get("visible", True)) if isinstance(value, dict) else value is True


def fixture_text(value: Any) -> str:
    return str(value.get("text", "")) if isinstance(value, dict) else ""


def text_matches(spec: dict[str, Any], text: str) -> bool:
    required = spec.get("text")
    excluded = spec.get("exclude_text")
    return (required is None or re.search(required, text, re.IGNORECASE) is not None) and (
        excluded is None or re.search(excluded, text, re.IGNORECASE) is None
    )


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
                    # Selector absence is the only fail-soft condition. A route,
                    # action, browser, or page error must not be hidden by a
                    # successful fallback attempt.
                    break
                if result is not None:
                    break
            if result is None:
                required = bool(capture.get("required", False))
                outcome = "failed" if attempt_errors else "anchor-missing"
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
                unhealthy = result["state"] in {"loading", "error"}
                failures += int(unhealthy)
                results.append(
                    {
                        "id": capture["id"],
                        "outcome": "failed" if unhealthy else "captured",
                        "required": bool(capture.get("required", False)),
                        **result,
                    }
                )
    finally:
        probe.close()
        summary = {
            "captured": sum(item["outcome"] == "captured" for item in results),
            "anchor_missing": sum(item["outcome"] == "anchor-missing" for item in results),
            "failed": sum(item["outcome"] == "failed" for item in results),
            "failures": failures,
        }
        run_manifest = {
            "schema": 2,
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
    parser.add_argument("--manifest", type=Path, default=skill_dir / "walk-manifest.v2.json")
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
