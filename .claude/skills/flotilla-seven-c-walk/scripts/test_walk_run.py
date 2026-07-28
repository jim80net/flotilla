#!/usr/bin/env python3

import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from walk_run import run


SCRIPT_DIR = Path(__file__).resolve().parent
RUNNER = SCRIPT_DIR / "walk_run.py"
MANIFEST = SCRIPT_DIR.parent / "walk-manifest.v1.json"
FIXTURE = SCRIPT_DIR / "testdata" / "stale-selectors.json"


class WalkRunTests(unittest.TestCase):
    def test_absent_optional_state_and_moved_route_complete_matrix(self) -> None:
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
                    "captured": 2,
                    "failed": 0,
                    "failures": 0,
                    "unavailable": 1,
                },
            )
            by_id = {capture["id"]: capture for capture in result["captures"]}
            self.assertEqual(by_id["issues-window"]["outcome"], "captured")
            self.assertEqual(by_id["issues-shipped"]["outcome"], "unavailable")
            self.assertFalse(by_id["issues-shipped"]["required"])
            self.assertEqual(by_id["decisions-summary"]["outcome"], "captured")
            self.assertEqual(
                by_id["decisions-summary"]["route"], "/research?focus=decisions"
            )
            self.assertNotIn(".gdec-summary", by_id["decisions-summary"]["selector"])

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
                    "selector": spec["anchors"][0]["selector"],
                    "state": spec["anchors"][0]["state"],
                    "screenshot": None,
                }

            def close(self) -> None:
                return

        manifest = {
            "schema": 1,
            "captures": [
                {
                    "id": "optional-error",
                    "required": False,
                    "attempts": [
                        {
                            "route": "/broken",
                            "anchors": [{"selector": "#optional", "state": "populated"}],
                        },
                        {
                            "route": "/fallback",
                            "anchors": [{"selector": "#fallback", "state": "legacy"}],
                        },
                    ],
                },
                {
                    "id": "later-required",
                    "required": True,
                    "attempts": [
                        {
                            "route": "/later",
                            "anchors": [{"selector": "#later", "state": "loaded"}],
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
                {"captured": 1, "failed": 1, "failures": 1, "unavailable": 0},
            )
            by_id = {capture["id"]: capture for capture in result["captures"]}
            self.assertEqual(by_id["optional-error"]["outcome"], "failed")
            self.assertEqual(by_id["later-required"]["outcome"], "captured")
            self.assertEqual(probe.routes, ["/broken", "/later"])
            self.assertTrue((output / "walk-run.json").exists())


if __name__ == "__main__":
    unittest.main()
