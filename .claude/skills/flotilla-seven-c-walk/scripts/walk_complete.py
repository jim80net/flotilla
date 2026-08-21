#!/usr/bin/env python3
"""Finalize a Seven-C walk only after every durable result exists."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys
from typing import Any

from walk_run import atomic_json, load_json, validate_output_dir


def finalize(
    walk_dir: Path,
    scorecard: Path,
    seeing_complete: int,
    seeing_total: int,
    generated_work: list[str],
) -> dict[str, Any]:
    validate_output_dir(walk_dir / "assets")
    if not scorecard.is_file():
        raise ValueError(f"scorecard does not exist: {scorecard}")
    if "parades" in scorecard.resolve().parts or "state" not in scorecard.resolve().parts:
        raise ValueError("scorecard must live beneath state/ and never parades/")
    run_path = walk_dir / "assets" / "walk-run.json"
    run = load_json(run_path)
    if run.get("completed") is not True or run.get("summary", {}).get("failures") != 0:
        raise ValueError("capture run is not complete and successful")
    if seeing_total < 1 or seeing_complete != seeing_total:
        raise ValueError("seeing must be complete (complete == total > 0)")
    work = [item.strip() for item in generated_work if item.strip()]
    if not work:
        raise ValueError("at least one generated-work reference is required")

    marker = {
        "schema": 1,
        "completed": True,
        "scorecard": str(scorecard.resolve()),
        "seeing": {"complete": seeing_complete, "total": seeing_total},
        "generated_work": work,
        "capture_manifest": str(run_path.resolve()),
    }
    atomic_json(walk_dir / "walk-complete.json", marker)
    return marker


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--walk-dir", type=Path, required=True)
    parser.add_argument("--scorecard", type=Path, required=True)
    parser.add_argument("--seeing-complete", type=int, required=True)
    parser.add_argument("--seeing-total", type=int, required=True)
    parser.add_argument("--generated-work", action="append", default=[])
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    marker = finalize(
        args.walk_dir,
        args.scorecard,
        args.seeing_complete,
        args.seeing_total,
        args.generated_work,
    )
    print(json.dumps(marker, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, json.JSONDecodeError) as error:
        print(f"walk-complete: {error}", file=sys.stderr)
        sys.exit(1)
