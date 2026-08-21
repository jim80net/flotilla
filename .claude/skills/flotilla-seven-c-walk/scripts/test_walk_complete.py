#!/usr/bin/env python3

import json
from pathlib import Path
import tempfile
import unittest

from walk_complete import finalize


class WalkCompleteTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        state = Path(self.temp.name) / "state"
        self.walk = state / "xo-walk-generic"
        assets = self.walk / "assets"
        assets.mkdir(parents=True)
        (assets / "walk-run.json").write_text(
            json.dumps({"completed": True, "summary": {"failures": 0}}),
            encoding="utf-8",
        )
        self.scorecard = state / "xo-sevenc-scorecard-generic.md"
        self.scorecard.write_text("# scorecard\n", encoding="utf-8")

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_finalize_requires_and_records_complete_package(self) -> None:
        marker = finalize(self.walk, self.scorecard, 93, 93, ["issue: generic-1"])
        self.assertTrue(marker["completed"])
        self.assertEqual(marker["seeing"], {"complete": 93, "total": 93})
        self.assertEqual(marker["generated_work"], ["issue: generic-1"])
        durable = json.loads((self.walk / "walk-complete.json").read_text(encoding="utf-8"))
        self.assertEqual(durable, marker)

    def test_finalize_refuses_partial_seeing_without_marker(self) -> None:
        with self.assertRaisesRegex(ValueError, "seeing must be complete"):
            finalize(self.walk, self.scorecard, 92, 93, ["issue: generic-1"])
        self.assertFalse((self.walk / "walk-complete.json").exists())

    def test_finalize_refuses_missing_generated_work_without_marker(self) -> None:
        with self.assertRaisesRegex(ValueError, "generated-work"):
            finalize(self.walk, self.scorecard, 93, 93, [])
        self.assertFalse((self.walk / "walk-complete.json").exists())


if __name__ == "__main__":
    unittest.main()
