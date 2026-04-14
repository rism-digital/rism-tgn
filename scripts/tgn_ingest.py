#!/usr/bin/env python3
import argparse
import csv
import json
import os
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable


@dataclass(frozen=True)
class TableSpec:
    source_file: str
    stage_table: str
    expected_cols: int
    parser: str  # fixed|term


TABLE_SPECS = [
    TableSpec("SUBJECT.out", "subject_raw", 7, "fixed"),
    TableSpec("TERM.out", "term_raw", 13, "term"),
    TableSpec("LANGUAGE_RELS.out", "language_rels_raw", 8, "fixed"),
    TableSpec("SUBJECT_RELS.out", "subject_rels_raw", 9, "fixed"),
    TableSpec("SUBJECT_MERGE.out", "subject_merge_raw", 3, "fixed"),
    TableSpec("PTYPE_ROLE.out", "ptype_role_raw", 2, "fixed"),
    TableSpec("PTYPE_ROLE_RELS.out", "ptype_role_rels_raw", 8, "fixed"),
    TableSpec("COORDINATES.out", "coordinates_raw", 34, "fixed"),
]


class IngestError(Exception):
    pass


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Load selected Getty TGN REL files into PostgreSQL")
    p.add_argument("--input-dir", default="tgn_rel_0126", help="Directory containing *.out files (default: ./tgn_rel_0126)")
    p.add_argument("--db-url", default=os.getenv("DATABASE_URL"), help="PostgreSQL connection URL")
    p.add_argument("--release-id", required=True, help="Release label, e.g. tgn_rel_0126")
    p.add_argument("--psql-bin", default="psql", help="psql executable")
    p.add_argument("--keep-temp", action="store_true", help="Keep canonicalized temp files")
    return p.parse_args()


def run_psql(psql_bin: str, db_url: str, extra_args: list[str], *, capture_output: bool = False) -> subprocess.CompletedProcess:
    cmd = [psql_bin, db_url, "-v", "ON_ERROR_STOP=1", *extra_args]
    return subprocess.run(cmd, check=True, text=True, capture_output=capture_output)


def normalize_line(raw: str) -> str:
    return raw.rstrip("\n").rstrip("\r")


def parse_fixed(parts: list[str], expected_cols: int) -> tuple[list[str], bool]:
    anomaly = False
    if len(parts) < expected_cols:
        parts = parts + [""] * (expected_cols - len(parts))
        anomaly = True
    elif len(parts) > expected_cols:
        head = parts[: expected_cols - 1]
        tail = ["\t".join(parts[expected_cols - 1 :])]
        parts = head + tail
        anomaly = True
    return parts, anomaly


def parse_term(parts: list[str]) -> tuple[list[str], bool]:
    # TERM.out should be 13 fields. Rare rows can contain extra tabs inside term text.
    if len(parts) == 13:
        return parts, False
    if len(parts) > 13:
        head = parts[:10]
        tail = parts[-2:]
        term_text = "\t".join(parts[10:-2])
        return head + [term_text] + tail, True
    padded = parts + [""] * (13 - len(parts))
    return padded, True


def canonicalize_file(
    spec: TableSpec,
    input_path: Path,
    output_path: Path,
    row_counter: dict[str, int],
    anomaly_counter: dict[str, int],
) -> None:
    parser_fn: Callable[[list[str], int], tuple[list[str], bool]]
    if spec.parser == "fixed":
        parser_fn = parse_fixed
    elif spec.parser == "term":
        parser_fn = lambda p, _n: parse_term(p)
    else:
        raise IngestError(f"Unknown parser type: {spec.parser}")

    rows = 0
    anomalies = 0
    with input_path.open("r", encoding="utf-8", errors="replace", newline="") as src, output_path.open(
        "w", encoding="utf-8", newline=""
    ) as dst:
        writer = csv.writer(dst, delimiter="\t", quotechar='"', quoting=csv.QUOTE_MINIMAL, lineterminator="\n")
        for raw_line in src:
            line = normalize_line(raw_line)
            parts = line.split("\t")
            parsed, anomaly = parser_fn(parts, spec.expected_cols)
            if len(parsed) != spec.expected_cols:
                raise IngestError(f"{spec.source_file}: parser produced {len(parsed)} fields, expected {spec.expected_cols}")
            writer.writerow(parsed)
            rows += 1
            if anomaly:
                anomalies += 1

    row_counter[spec.stage_table] = rows
    anomaly_counter[spec.stage_table] = anomalies


def sql_string_literal(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def main() -> int:
    args = parse_args()
    if not args.db_url:
        print("Missing --db-url or DATABASE_URL", file=sys.stderr)
        return 2

    root = Path(__file__).resolve().parent.parent
    input_dir = Path(args.input_dir).resolve()
    run_id = int(datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S"))

    for spec in TABLE_SPECS:
        source_path = input_dir / spec.source_file
        if not source_path.exists():
            raise IngestError(f"Required input file is missing: {source_path}")

    row_counter: dict[str, int] = {}
    anomaly_counter: dict[str, int] = {}

    with tempfile.TemporaryDirectory(prefix="tgn_ingest_") as tmp:
        tmp_dir = Path(tmp)

        for spec in TABLE_SPECS:
            canonicalize_file(
                spec,
                input_dir / spec.source_file,
                tmp_dir / f"{spec.stage_table}.tsv",
                row_counter,
                anomaly_counter,
            )

        parser_stats = {
            "rows": row_counter,
            "anomalies": anomaly_counter,
        }
        parser_stats_sql = sql_string_literal(json.dumps(parser_stats, separators=(",", ":"))) + "::jsonb"

        run_psql(args.psql_bin, args.db_url, ["-f", str(root / "sql" / "10_schema.sql")])
        run_psql(args.psql_bin, args.db_url, ["-f", str(root / "sql" / "30_match.sql")])

        run_psql(
            args.psql_bin,
            args.db_url,
            [
                "-c",
                (
                    "INSERT INTO tgn.import_run (run_id, release_id, status, parser_stats) "
                    f"VALUES ({run_id}, {sql_string_literal(args.release_id)}, 'running', {parser_stats_sql}) "
                    "ON CONFLICT (run_id) DO UPDATE SET release_id = EXCLUDED.release_id, status = EXCLUDED.status, parser_stats = EXCLUDED.parser_stats"
                ),
            ],
        )

        try:
            run_psql(
                args.psql_bin,
                args.db_url,
                [
                    "-c",
                    "TRUNCATE TABLE tgn_stage.subject_raw, tgn_stage.term_raw, tgn_stage.language_rels_raw, tgn_stage.subject_rels_raw, tgn_stage.subject_merge_raw, tgn_stage.ptype_role_raw, tgn_stage.ptype_role_rels_raw, tgn_stage.coordinates_raw",
                ],
            )

            for spec in TABLE_SPECS:
                cols = ", ".join(f"c{i}" for i in range(1, spec.expected_cols + 1))
                data_file = str((tmp_dir / f"{spec.stage_table}.tsv").resolve()).replace("'", "''")
                copy_sql = (
                    f"\\copy tgn_stage.{spec.stage_table} ({cols}) "
                    f"FROM '{data_file}' "
                    "WITH (FORMAT csv, DELIMITER E'\\t', QUOTE '\"', ESCAPE '\"', NULL '')"
                )
                run_psql(args.psql_bin, args.db_url, ["-c", copy_sql])

            run_psql(args.psql_bin, args.db_url, ["-f", str(root / "sql" / "20_load.sql")])
            validation = run_psql(args.psql_bin, args.db_url, ["-f", str(root / "sql" / "40_validate.sql")], capture_output=True)

            load_stats_sql = sql_string_literal(json.dumps({"validation": validation.stdout}, separators=(",", ":"))) + "::jsonb"
            run_psql(
                args.psql_bin,
                args.db_url,
                [
                    "-c",
                    (
                        "UPDATE tgn.import_run "
                        "SET status = 'succeeded', finished_at = now(), load_stats = "
                        f"{load_stats_sql} "
                        f"WHERE run_id = {run_id}"
                    ),
                ],
            )

            print(f"Run {run_id} succeeded")
            print(json.dumps(parser_stats, indent=2, sort_keys=True))
            print("Validation summary:")
            print(validation.stdout.strip())

        except subprocess.CalledProcessError as e:
            message = f"psql failed with exit code {e.returncode}"
            run_psql(
                args.psql_bin,
                args.db_url,
                [
                    "-c",
                    (
                        "UPDATE tgn.import_run "
                        "SET status = 'failed', finished_at = now(), error_message = "
                        f"{sql_string_literal(message)} "
                        f"WHERE run_id = {run_id}"
                    ),
                ],
            )
            raise

        finally:
            if args.keep_temp:
                keep_dir = root / f".tgn_ingest_tmp_{run_id}"
                keep_dir.mkdir(parents=True, exist_ok=True)
                for spec in TABLE_SPECS:
                    src = tmp_dir / f"{spec.stage_table}.tsv"
                    dst = keep_dir / src.name
                    dst.write_bytes(src.read_bytes())
                print(f"Kept canonicalized files at {keep_dir}")

    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except IngestError as e:
        print(f"ERROR: {e}", file=sys.stderr)
        raise SystemExit(2)
