#!/usr/bin/env python3

import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


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


if __name__ == "__main__":
    unittest.main()
