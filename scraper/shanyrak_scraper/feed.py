from __future__ import annotations

import logging
import random
import time
from urllib.parse import parse_qs, urlencode, urlparse

import requests

SITE = "https://krisha.kz"
LIST_PATH = "/a/ajax-map-list/map"

HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 "
        "(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
    ),
    "Accept": "application/json, text/javascript, */*; q=0.01",
    "Accept-Language": "ru-RU,ru;q=0.9",
    "X-Requested-With": "XMLHttpRequest",
}

RANGE_FILTERS = {
    "price_from": ("price", "from"),
    "price_to": ("price", "to"),
    "square_from": ("live.square", "from"),
    "square_to": ("live.square", "to"),
    "floor_from": ("house.floor_num", "from"),
    "floor_to": ("house.floor_num", "to"),
    "house_year_from": ("house.year", "from"),
    "house_year_to": ("house.year", "to"),
}

FLAG_FILTERS = {
    "not_first_floor": ("das[floor_not_first]", 1),
    "not_last_floor": ("das[floor_not_last]", 1),
    "has_photo": ("das[_sys.hasphoto]", 1),
    "not_dorm": ("das[flat.priv_dorm]", 2),
}

log = logging.getLogger(__name__)


class FeedError(Exception):
    pass


class BlockedError(FeedError):
    """The site answered with its bot-protection challenge instead of data."""


def area_params(area: str) -> dict[str, str]:
    """Accepts a krisha map URL or a bare `p<lat>,<lon>,...` polygon."""
    area = (area or "").strip()
    if not area:
        return {}
    if area.startswith("p"):
        return {"areas": area}

    query = parse_qs(urlparse(area).query)
    return {
        key: query[key][0]
        for key in ("areas", "lat", "lon", "zoom")
        if query.get(key)
    }


def section_from_area(area: str) -> str:
    """The map tab lives on /map/prodazha/..., the feed wants /prodazha/..."""
    path = urlparse((area or "").strip()).path
    if path.startswith("/map/"):
        path = path[4:]
    if "/kvartiry/" not in path and "/doma/" not in path:
        return ""
    return path if path.endswith("/") else path + "/"


def build_query(filters: dict, area: str = "", page: int = 1) -> str:
    params: list[tuple[str, object]] = list(area_params(area).items())

    for key, (group, bound) in RANGE_FILTERS.items():
        value = filters.get(key)
        if value is not None:
            params.append((f"das[{group}][{bound}]", value))

    for key, (param, value) in FLAG_FILTERS.items():
        if filters.get(key):
            params.append((param, value))

    rooms = filters.get("rooms")
    if isinstance(rooms, (list, tuple)):
        params.extend(("das[live.rooms][]", room) for room in rooms)
    elif rooms is not None:
        params.append(("das[live.rooms]", rooms))

    for key, param in (("who", "das[who]"), ("mortgage", "das[mortgage]")):
        value = filters.get(key)
        if value is not None:
            params.append((param, value))

    if filters.get("text"):
        params.append(("_txt_", filters["text"]))

    params.append(("page", page))
    return urlencode(params)


class Feed:
    """Reads the public JSON feed behind the map tab of krisha.kz."""

    def __init__(
        self,
        *,
        timeout: float = 25,
        retries: int = 3,
        delay: tuple[float, float] = (1.0, 2.0),
        session: requests.Session | None = None,
        sleep=time.sleep,
    ):
        self.timeout = timeout
        self.retries = retries
        self.delay = delay
        self.sleep = sleep
        self.session = session or requests.Session()
        self.session.headers.update(HEADERS)
        self._last_request = 0.0

    def page(self, section: str, filters: dict, area: str = "", page: int = 1) -> dict:
        url = f"{SITE}{LIST_PATH}{section}?{build_query(filters, area, page)}"
        return self._get(url, referer=f"{SITE}/map{section}")

    def _get(self, url: str, *, referer: str):
        self._pace()
        last_error = ""

        for attempt in range(1, self.retries + 1):
            try:
                response = self.session.get(
                    url, timeout=self.timeout, headers={"Referer": referer}
                )
            except requests.RequestException as exc:
                last_error = f"{exc.__class__.__name__}: {exc}"
                log.warning("request failed (%s), attempt %d", last_error, attempt)
                self.sleep(2 * attempt)
                continue

            if response.status_code == 200:
                try:
                    return response.json()
                except ValueError:
                    raise FeedError(f"{url}: response is not JSON") from None

            if _is_challenge(response):
                raise BlockedError(
                    f"{url}: krisha.kz answered with a bot-protection challenge "
                    f"(HTTP {response.status_code})"
                )

            last_error = f"HTTP {response.status_code}"
            wait = 10 * attempt if response.status_code in (403, 429) else 2 * attempt
            log.warning("%s, attempt %d, waiting %ds", last_error, attempt, wait)
            self.sleep(wait)

        raise FeedError(f"{url}: giving up after {self.retries} attempts ({last_error})")

    def _pace(self):
        elapsed = time.monotonic() - self._last_request
        gap = random.uniform(*self.delay)
        if self._last_request and elapsed < gap:
            self.sleep(gap - elapsed)
        self._last_request = time.monotonic()


def _is_challenge(response) -> bool:
    if response.status_code == 468:
        return True
    body = response.text[:2000].lower()
    return "safeline" in body or "slg-title" in body
