from __future__ import annotations

import re
from dataclasses import dataclass, replace
from typing import Mapping

SITE = "https://krisha.kz"

SELLER_BY_USER_TYPE = {
    "owner": "owner",
    "specialist": "agent",
    "agent": "agent",
    "agency": "agency",
    "developer": "developer",
}

CITY_BY_SLUG = {
    "almaty": "Алматы",
    "astana": "Астана",
    "nur-sultan": "Астана",
    "shymkent": "Шымкент",
    "karaganda": "Караганда",
    "aktobe": "Актобе",
    "atyrau": "Атырау",
    "taraz": "Тараз",
    "pavlodar": "Павлодар",
    "ust-kamenogorsk": "Усть-Каменогорск",
    "semej": "Семей",
    "kostanaj": "Костанай",
    "kyzylorda": "Кызылорда",
    "uralsk": "Уральск",
    "petropavlovsk": "Петропавловск",
    "aktau": "Актау",
    "temirtau": "Темиртау",
    "turkestan": "Туркестан",
    "kokshetau": "Кокшетау",
    "taldykorgan": "Талдыкорган",
    "ekibastuz": "Экибастуз",
}

RE_ROOMS = re.compile(r"(\d+)-комн")
RE_AREA = re.compile(r"([\d]+[.,]?\d*)\s*м²")
RE_FLOOR = re.compile(r"(\d+)\s*/\s*(\d+)\s*этаж")
RE_FLOOR_ONLY = re.compile(r"(?<!/)\b(\d+)\s*этаж")
RE_CARD = re.compile(r'data-id="(\d+)"')
RE_DATE = re.compile(r'sidebar-item__date">\s*([^<]+?)\s*<')
RE_PAGE = re.compile(r"[?&]page=(\d+)")


@dataclass(frozen=True)
class Listing:
    id: str
    title: str = ""
    price: float | None = None
    area: float | None = None
    rooms: int | None = None
    floor: int | None = None
    total_floors: int | None = None
    year_built: int | None = None
    address: str = ""
    city: str = ""
    district: str = ""
    complex: str = ""
    complex_id: str = ""
    seller: str = ""
    owner_name: str = ""
    photo: str = ""
    lat: float | None = None
    lon: float | None = None
    flags: str = ""
    date_posted: str = ""
    link: str = ""

    @property
    def price_per_m2(self) -> float | None:
        if not self.price or not self.area:
            return None
        return self.price / self.area


def parse_page(payload: Mapping, *, city: str = "") -> list[Listing]:
    adverts = payload.get("adverts") or {}
    dates = card_dates(payload.get("html") or "")

    listings = []
    for advert in adverts.values():
        listing = _listing(advert, city=city, dates=dates)
        if listing is not None:
            listings.append(listing)
    return listings


def _listing(advert: Mapping, *, city, dates) -> Listing | None:
    ad_id = str(advert.get("id") or "").strip()
    if not ad_id or not advert.get("price"):
        return None

    title = str(advert.get("title") or "")
    rooms, area, floor, total_floors = parse_title(title)
    coords = advert.get("map") or {}
    photos = advert.get("photos") or []

    return Listing(
        id=ad_id,
        title=title,
        price=float(advert["price"]),
        area=_float(advert.get("square")) or area,
        rooms=_int(advert.get("rooms")) or rooms,
        floor=floor,
        total_floors=total_floors,
        address=str(advert.get("addressTitle") or ""),
        city=city,
        complex_id=str(advert.get("complexId") or ""),
        seller=SELLER_BY_USER_TYPE.get(str(advert.get("userType") or ""), "unknown"),
        owner_name=str(advert.get("ownerName") or ""),
        photo=str(photos[0].get("src") or "") if photos else "",
        lat=_float(coords.get("lat")),
        lon=_float(coords.get("lon")),
        date_posted=dates.get(ad_id, ""),
        link=f"{SITE}/a/show/{ad_id}",
    )


def parse_title(title: str) -> tuple[int | None, float | None, int | None, int | None]:
    rooms = area = floor = total_floors = None

    match = RE_ROOMS.search(title)
    if match:
        rooms = int(match.group(1))

    match = RE_AREA.search(title)
    if match:
        area = float(match.group(1).replace(",", "."))

    match = RE_FLOOR.search(title)
    if match:
        floor, total_floors = int(match.group(1)), int(match.group(2))
    else:
        match = RE_FLOOR_ONLY.search(title)
        if match:
            floor = int(match.group(1))

    return rooms, area, floor, total_floors


def card_dates(html: str) -> dict[str, str]:
    """Posting dates live in the rendered cards, not in the JSON adverts."""
    dates: dict[str, str] = {}
    blocks = RE_CARD.split(html)
    for ad_id, block in zip(blocks[1::2], blocks[2::2]):
        if ad_id in dates:
            continue
        match = RE_DATE.search(block)
        if match:
            dates[ad_id] = match.group(1)
    return dates


def max_page(pager_html: str) -> int | None:
    pages = [int(m.group(1)) for m in RE_PAGE.finditer(pager_html or "")]
    return max(pages) if pages else None


def city_for_section(section: str) -> str:
    slug = section.strip("/").split("/")[-1]
    for candidate in sorted(CITY_BY_SLUG, key=len, reverse=True):
        if slug == candidate or slug.startswith(candidate + "-"):
            return CITY_BY_SLUG[candidate]
    return ""


def with_flags(listing: Listing, words) -> Listing:
    if not words:
        return listing
    haystack = f"{listing.title} {listing.address} {listing.owner_name}".lower()
    hits = sorted({word for word in words if word.lower() in haystack})
    return replace(listing, flags=", ".join(hits))


def passes_local(listing: Listing, local) -> bool:
    ppm = listing.price_per_m2
    if ppm is None:
        return False
    if local.min_price_per_m2 and ppm < local.min_price_per_m2:
        return False
    if local.max_price_per_m2 and ppm > local.max_price_per_m2:
        return False
    return True


def _float(value) -> float | None:
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def _int(value) -> int | None:
    try:
        return int(value)
    except (TypeError, ValueError):
        return None
