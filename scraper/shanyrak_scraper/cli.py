from __future__ import annotations

import argparse
import logging
import os
import re
import sys
import time

import requests

from . import config, db
from .feed import Feed, FeedError
from .scan import scan

DEFAULT_CONFIG = os.environ.get("SHANYRAK_CONFIG", "shanyrak.toml")
DURATION = re.compile(r"^(\d+)([smhd])$")
SECONDS = {"s": 1, "m": 60, "h": 3600, "d": 86400}

log = logging.getLogger("shanyrak")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="shanyrak-scraper",
        description="Collects krisha.kz listings into SQLite for the Shanyrak map.",
    )
    parser.add_argument("--config", default=DEFAULT_CONFIG, help="path to shanyrak.toml")
    parser.add_argument("-v", "--verbose", action="store_true")
    commands = parser.add_subparsers(dest="command", required=True)

    scan_cmd = commands.add_parser("scan", help="fetch listings into the database")
    scan_cmd.add_argument("searches", nargs="*", help="which searches to run")
    scan_cmd.add_argument(
        "--every", metavar="DURATION", help="keep running, e.g. 6h, 90m, 3600s"
    )

    stats_cmd = commands.add_parser("stats", help="what is in the database")
    stats_cmd.add_argument("searches", nargs="*")

    args = parser.parse_args(argv)
    logging.basicConfig(
        level=logging.DEBUG if args.verbose else logging.INFO,
        format="%(asctime)s %(levelname)-7s %(message)s",
        datefmt="%H:%M:%S",
    )

    try:
        conf = config.load(args.config)
        searches = conf.select(args.searches)
        if args.command == "stats":
            return _stats(conf, searches)
        return _scan(conf, searches, every=_duration(args.every))
    except (config.ConfigError, FeedError, ValueError) as exc:
        log.error("%s", exc)
        return 1
    except KeyboardInterrupt:
        log.info("interrupted")
        return 130


def _scan(conf, searches, every: int | None) -> int:
    failed = False
    session = requests.Session()

    while True:
        for search in searches:
            feed = Feed(
                timeout=search.request_timeout,
                retries=search.retries,
                delay=search.delay,
                session=session,
            )
            conn = db.connect(conf.db_path(search))
            try:
                report = scan(search, conn, feed)
            finally:
                conn.close()
            log.info("%s", report.line())
            failed = failed or report.blocked

        if not every:
            return 1 if failed else 0
        log.info("sleeping for %ds", every)
        time.sleep(every)


def _stats(conf, searches) -> int:
    for search in searches:
        path = conf.db_path(search)
        if not path.exists():
            log.info("%s: no database yet (%s)", search.name, path)
            continue
        conn = db.connect(path)
        try:
            values = db.counts(conn)
        finally:
            conn.close()
        log.info(
            "%s: %s",
            search.name,
            ", ".join(f"{key} {value}" for key, value in values.items()),
        )
    return 0


def _duration(value: str | None) -> int | None:
    if not value:
        return None
    match = DURATION.match(value)
    if not match:
        raise ValueError(f"bad duration {value!r}, expected forms like 6h, 90m, 3600s")
    return int(match.group(1)) * SECONDS[match.group(2)]


if __name__ == "__main__":
    sys.exit(main())
