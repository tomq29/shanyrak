from __future__ import annotations

import logging
from dataclasses import dataclass, field

from . import db
from .config import Search
from .feed import BlockedError, Feed, section_from_area
from .parse import city_for_section, max_page, parse_page, passes_local, with_flags

log = logging.getLogger(__name__)


@dataclass
class ScanReport:
    search: str
    fetched: int = 0
    new: int = 0
    price_down: int = 0
    price_up: int = 0
    seen: int = 0
    filtered: int = 0
    skipped: int = 0
    gone: int = 0
    without_coordinates: int = 0
    complete: bool = True
    blocked: bool = False
    outcomes: dict = field(default_factory=dict)

    def line(self) -> str:
        return (
            f"{self.search}: {self.fetched} listings "
            f"(new {self.new}, cheaper {self.price_down}, dearer {self.price_up}, "
            f"unchanged {self.seen}); filtered {self.filtered}, "
            f"rejected earlier {self.skipped}, delisted {self.gone}"
        )


def scan(search: Search, conn, feed: Feed) -> ScanReport:
    report = ScanReport(search=search.name)
    rejected = db.skip_ids(conn)
    stamp = db.now()
    seen_ids: set[str] = set()

    for area in search.areas or ("",):
        section = _section_for(search, area)
        try:
            complete = _scan_area(
                search, conn, feed, area, section, seen_ids, rejected, stamp, report
            )
        except BlockedError as exc:
            log.error("%s", exc)
            report.blocked = True
            complete = False
        report.complete = report.complete and complete

    if report.complete:
        report.gone = db.mark_gone(conn, seen_ids, stamp)
    else:
        log.warning(
            "%s: crawl incomplete, not marking delisted listings", search.name
        )
    conn.commit()
    return report


def _scan_area(
    search, conn, feed, area, section, seen_ids, rejected, stamp, report
) -> bool:
    city = city_for_section(section)
    last_page = None

    for page in range(1, search.max_pages + 1):
        payload = feed.page(section, search.filters, area, page)
        listings = parse_page(payload, city=city)
        if not listings:
            return True

        last_page = max_page(payload.get("pager", "")) or last_page
        _store(search, conn, listings, seen_ids, rejected, stamp, report)
        conn.commit()

        log.info(
            "%s: page %d/%s, %d listings, %d kept so far",
            search.name, page, last_page or "?", len(listings), report.fetched,
        )

        if last_page and page >= last_page:
            return True

    log.warning(
        "%s: stopped at max_pages=%d, there is more to fetch",
        search.name, search.max_pages,
    )
    return False


def _store(search, conn, listings, seen_ids, rejected, stamp, report) -> None:
    for listing in listings:
        if listing.id in seen_ids:
            continue
        seen_ids.add(listing.id)

        if listing.id in rejected:
            report.skipped += 1
            continue
        if not passes_local(listing, search.local):
            report.filtered += 1
            continue

        listing = with_flags(listing, search.local.flag_words)
        if listing.lat is None:
            report.without_coordinates += 1

        outcome = db.upsert(conn, listing, stamp)
        setattr(report, outcome, getattr(report, outcome) + 1)
        report.fetched += 1


def _section_for(search: Search, area: str) -> str:
    return section_from_area(area) or search.section
