from __future__ import annotations

import os
import sqlite3
from dataclasses import asdict
from datetime import datetime
from pathlib import Path

from .parse import Listing

SCHEMA_CANDIDATES = (
    Path(__file__).resolve().parents[2] / "schema" / "schema.sql",
    Path.cwd() / "schema" / "schema.sql",
    Path("/app/schema/schema.sql"),
)

AD_COLUMNS = (
    "title",
    "price",
    "area",
    "rooms",
    "floor",
    "total_floors",
    "year_built",
    "address",
    "city",
    "district",
    "complex",
    "complex_id",
    "seller",
    "owner_name",
    "photo",
    "lat",
    "lon",
    "flags",
    "date_posted",
    "link",
)

REJECTED = ("disliked", "hidden")


def now() -> str:
    return datetime.now().isoformat(timespec="seconds")


def schema_sql() -> str:
    """The schema is shared with the Go server, so it lives outside the package
    and has to be found both in a checkout and in the installed container."""
    override = os.environ.get("SHANYRAK_SCHEMA")
    candidates = (Path(override),) if override else SCHEMA_CANDIDATES
    for candidate in candidates:
        if candidate.is_file():
            return candidate.read_text(encoding="utf-8")
    tried = ", ".join(str(candidate) for candidate in candidates)
    raise FileNotFoundError(f"schema.sql not found, looked in: {tried}")


def connect(path: str | Path) -> sqlite3.Connection:
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)

    conn = sqlite3.connect(str(path))
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA busy_timeout=5000")
    conn.execute("PRAGMA foreign_keys=ON")
    conn.executescript(schema_sql())
    _add_missing_columns(conn)
    conn.execute("UPDATE statuses SET status='disliked' WHERE status='hidden'")
    conn.commit()
    return conn


def _add_missing_columns(conn: sqlite3.Connection) -> None:
    """Databases created by earlier versions lack some of the columns."""
    existing = {row["name"] for row in conn.execute("PRAGMA table_info(ads)")}
    for column in AD_COLUMNS:
        if column not in existing:
            kind = "REAL" if column in ("price", "area", "lat", "lon") else "TEXT"
            conn.execute(f"ALTER TABLE ads ADD COLUMN {column} {kind}")


def upsert(conn: sqlite3.Connection, listing: Listing, stamp: str | None = None) -> str:
    """Returns what happened: new, price_up, price_down or seen."""
    stamp = stamp or now()
    values = {key: value for key, value in asdict(listing).items() if key in AD_COLUMNS}
    row = conn.execute("SELECT price FROM ads WHERE id=?", (listing.id,)).fetchone()

    if row is None:
        columns = ("id", *AD_COLUMNS, "first_seen", "last_seen")
        placeholders = ", ".join("?" * len(columns))
        conn.execute(
            f"INSERT INTO ads ({', '.join(columns)}) VALUES ({placeholders})",
            [listing.id, *(values[column] for column in AD_COLUMNS), stamp, stamp],
        )
        _add_price(conn, listing.id, stamp, listing.price)
        return "new"

    filled = {key: value for key, value in values.items() if value not in (None, "")}
    if filled:
        assignments = ", ".join(f"{key}=?" for key in filled)
        conn.execute(
            f"UPDATE ads SET {assignments} WHERE id=?", [*filled.values(), listing.id]
        )
    conn.execute(
        "UPDATE ads SET last_seen=?, gone_at=NULL WHERE id=?", (stamp, listing.id)
    )

    old_price, new_price = row["price"], listing.price
    if new_price and old_price and abs(new_price - old_price) >= 1:
        _add_price(conn, listing.id, stamp, new_price)
        return "price_down" if new_price < old_price else "price_up"
    return "seen"


def _add_price(conn: sqlite3.Connection, ad_id: str, stamp: str, price) -> None:
    conn.execute(
        "INSERT INTO price_history (ad_id, seen_at, price) VALUES (?,?,?)",
        (ad_id, stamp, price),
    )


def mark_gone(conn: sqlite3.Connection, alive_ids, stamp: str | None = None) -> int:
    """Call only after a complete crawl, otherwise live listings get flagged."""
    stamp = stamp or now()
    alive = list(alive_ids)
    placeholders = ",".join("?" * len(alive)) if alive else "''"
    rejected = ",".join("?" * len(REJECTED))
    cursor = conn.execute(
        f"""
        UPDATE ads SET gone_at=?
         WHERE gone_at IS NULL
           AND id NOT IN ({placeholders})
           AND id NOT IN (SELECT ad_id FROM statuses WHERE status IN ({rejected}))
        """,
        [stamp, *alive, *REJECTED],
    )
    return cursor.rowcount


def skip_ids(conn: sqlite3.Connection) -> set[str]:
    rejected = ",".join("?" * len(REJECTED))
    rows = conn.execute(
        f"SELECT ad_id FROM statuses WHERE status IN ({rejected})", REJECTED
    )
    return {row["ad_id"] for row in rows}


def counts(conn: sqlite3.Connection) -> dict[str, int]:
    scalar = lambda sql: conn.execute(sql).fetchone()[0]  # noqa: E731
    return {
        "listings": scalar("SELECT COUNT(*) FROM ads"),
        "active": scalar("SELECT COUNT(*) FROM ads WHERE gone_at IS NULL"),
        "delisted": scalar("SELECT COUNT(*) FROM ads WHERE gone_at IS NOT NULL"),
        "with_coordinates": scalar("SELECT COUNT(*) FROM ads WHERE lat IS NOT NULL"),
        "price_changes": scalar(
            "SELECT COUNT(*) FROM price_history WHERE ad_id IN "
            "(SELECT ad_id FROM price_history GROUP BY ad_id HAVING COUNT(*) > 1)"
        ),
        "marks": scalar("SELECT COUNT(*) FROM statuses"),
    }
