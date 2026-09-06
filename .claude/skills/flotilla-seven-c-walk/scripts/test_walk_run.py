#!/usr/bin/env python3

import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from walk_run import FixtureProbe, load_json, run, validate_manifest


SCRIPT_DIR = Path(__file__).resolve().parent
RUNNER = SCRIPT_DIR / "walk_run.py"
MANIFEST = SCRIPT_DIR.parent / "walk-manifest.v2.json"
FIXTURE = SCRIPT_DIR / "testdata" / "current-surfaces.json"
REPO = SCRIPT_DIR.parents[3]


class WalkRunTests(unittest.TestCase):
    def test_current_common_matrix_uses_stable_anchors_and_states(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            output = Path(root) / "state" / "flotilla-dev-walk-generic" / "assets"
            completed = subprocess.run(
                [
                    sys.executable,
                    str(RUNNER),
                    "--manifest",
                    str(MANIFEST),
                    "--fixture",
                    str(FIXTURE),
                    "--output-dir",
                    str(output),
                ],
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertEqual(completed.returncode, 0, completed.stderr)
            result = json.loads((output / "walk-run.json").read_text(encoding="utf-8"))
            self.assertTrue(result["completed"])
            self.assertEqual(
                result["summary"],
                {
                    "anchor_missing": 0,
                    "captured": 6,
                    "failed": 0,
                    "failures": 0,
                },
            )
            by_id = {capture["id"]: capture for capture in result["captures"]}
            self.assertEqual(set(by_id), {"conversations", "goals", "issues", "research-decide", "research-learn", "parade"})
            self.assertEqual(by_id["issues"]["state"], "partial")
            self.assertEqual(by_id["issues"]["anchor"], "#view-issues:not(.hidden)")
            self.assertEqual(by_id["parade"]["state"], "empty")

    def test_runner_distinguishes_all_five_state_classes(self) -> None:
        for state in ("loading", "populated", "partial", "empty", "error"):
            spec = {
                "route": "/surface",
                "anchor": "#surface",
                "states": [{"selector": f".{state}", "state": state}],
            }
            probe = FixtureProbe({"routes": {"/surface": {"#surface": True, f".{state}": True}}})
            result = probe.attempt(spec, Path("unused.png"))
            self.assertIsNotNone(result)
            self.assertEqual(result["state"], state)

    def test_loading_and_error_are_captured_but_fail_the_run(self) -> None:
        for state in ("loading", "error"):
            manifest = {"schema": 2, "captures": [{
                "id": f"surface-{state}", "required": True,
                "attempts": [{"route": "/surface", "anchor": "#surface",
                              "states": [{"selector": f".{state}", "state": state}]}],
            }]}
            fixture = {"routes": {"/surface": {"#surface": True, f".{state}": True}}}
            with tempfile.TemporaryDirectory() as root:
                output = Path(root) / "state" / "generic-walk" / state
                result, status = run(manifest, FixtureProbe(fixture), output)
                self.assertEqual(status, 1)
                self.assertEqual(result["captures"][0]["outcome"], "failed")
                self.assertEqual(result["captures"][0]["state"], state)

    def test_required_current_state_anchors_exist_in_checked_in_html(self) -> None:
        manifest = load_json(MANIFEST)
        validate_manifest(manifest)
        route_file = {
            "/": REPO / "internal/dash/assets/index.html",
            "/research?focus=decisions": REPO / "internal/dash/assets/research.html",
            "/research?focus=learn": REPO / "internal/dash/assets/research.html",
            "/parade": REPO / "internal/dash/assets/parade.html",
        }
        for capture in manifest["captures"]:
            self.assertTrue(capture["required"])
            for attempt in capture["attempts"]:
                html = route_file[attempt["route"]].read_text(encoding="utf-8")
                anchor = attempt["anchor"]
                if anchor.startswith("#"):
                    anchor_id = anchor[1:].split(":", 1)[0].split(".", 1)[0]
                    self.assertIn(f'id="{anchor_id}"', html, f"{capture['id']}: {anchor}")
                elif anchor.startswith("body."):
                    class_name = anchor.split(".", 1)[1]
                    self.assertRegex(html, rf'<body[^>]*class="[^"]*\b{class_name}\b')
                else:
                    self.fail(f"unsupported stable-anchor fixture grammar: {anchor}")

    def test_missing_required_current_anchor_is_nonzero_but_matrix_completes(self) -> None:
        manifest = load_json(MANIFEST)
        fixture = load_json(FIXTURE)
        del fixture["routes"]["/"]["#view-issues:not(.hidden)"]
        with tempfile.TemporaryDirectory() as root:
            output = Path(root) / "state" / "generic-walk" / "assets"
            result, status = run(manifest, FixtureProbe(fixture), output)
            self.assertEqual(status, 1)
            self.assertEqual(result["summary"]["anchor_missing"], 1)
            self.assertEqual(result["summary"]["captured"], 5)
            by_id = {capture["id"]: capture for capture in result["captures"]}
            self.assertEqual(by_id["issues"]["outcome"], "anchor-missing")

    def test_public_or_parade_output_is_refused(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            output = Path(root) / "parades" / "assets"
            completed = subprocess.run(
                [
                    sys.executable,
                    str(RUNNER),
                    "--fixture",
                    str(FIXTURE),
                    "--output-dir",
                    str(output),
                ],
                check=False,
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(completed.returncode, 0)
            self.assertFalse((output / "walk-run.json").exists())

    def test_optional_page_error_fails_loud_and_does_not_hide_behind_fallback(self) -> None:
        class Probe:
            def __init__(self) -> None:
                self.routes: list[str] = []

            def attempt(self, spec: dict, _capture_path: Path) -> dict | None:
                self.routes.append(spec["route"])
                if spec["route"] == "/broken":
                    raise RuntimeError("generic page error")
                return {
                    "route": spec["route"],
                    "anchor": spec["anchor"],
                    "selector": spec["states"][0]["selector"],
                    "state": spec["states"][0]["state"],
                    "screenshot": None,
                }

            def close(self) -> None:
                return

        manifest = {
            "schema": 2,
            "captures": [
                {
                    "id": "optional-error",
                    "required": False,
                    "attempts": [
                        {
                            "route": "/broken",
                            "anchor": "#broken-surface",
                            "states": [{"selector": "#optional", "state": "populated"}],
                        },
                        {
                            "route": "/fallback",
                            "anchor": "#fallback-surface",
                            "states": [{"selector": "#fallback", "state": "populated"}],
                        },
                    ],
                },
                {
                    "id": "later-required",
                    "required": True,
                    "attempts": [
                        {
                            "route": "/later",
                            "anchor": "#later-surface",
                            "states": [{"selector": "#later", "state": "populated"}],
                        }
                    ],
                },
            ],
        }
        with tempfile.TemporaryDirectory() as root:
            output = Path(root) / "state" / "flotilla-dev-walk-generic" / "assets"
            probe = Probe()
            result, status = run(manifest, probe, output)
            self.assertEqual(status, 1)
            self.assertEqual(
                result["summary"],
                {"anchor_missing": 0, "captured": 1, "failed": 1, "failures": 1},
            )
            by_id = {capture["id"]: capture for capture in result["captures"]}
            self.assertEqual(by_id["optional-error"]["outcome"], "failed")
            self.assertEqual(by_id["later-required"]["outcome"], "captured")
            self.assertEqual(probe.routes, ["/broken", "/later"])
            self.assertTrue((output / "walk-run.json").exists())


if __name__ == "__main__":
    unittest.main()
