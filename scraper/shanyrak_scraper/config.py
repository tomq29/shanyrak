from __future__ import annotations

import tomllib
from dataclasses import dataclass, field
from pathlib import Path

FILTER_KEYS = frozenset(
    {
        "price_from",
        "price_to",
        "square_from",
        "square_to",
        "rooms",
        "floor_from",
        "floor_to",
        "not_first_floor",
        "not_last_floor",
        "house_year_from",
        "house_year_to",
        "who",
        "has_photo",
        "not_dorm",
        "mortgage",
        "text",
    }
)

LOCAL_KEYS = frozenset({"min_price_per_m2", "max_price_per_m2", "flag_words"})


class ConfigError(Exception):
    pass


@dataclass(frozen=True)
class Local:
    min_price_per_m2: float | None = None
    max_price_per_m2: float | None = None
    flag_words: tuple[str, ...] = ()


@dataclass(frozen=True)
class Search:
    name: str
    section: str
    areas: tuple[str, ...] = ()
    filters: dict = field(default_factory=dict)
    local: Local = Local()
    max_pages: int = 40
    delay: tuple[float, float] = (1.0, 2.0)
    request_timeout: float = 25.0
    retries: int = 3

    @property
    def db_name(self) -> str:
        return f"{self.name}.db"


@dataclass(frozen=True)
class Config:
    path: Path
    data_dir: Path
    searches: tuple[Search, ...]

    def get(self, name: str) -> Search:
        for search in self.searches:
            if search.name == name:
                return search
        known = ", ".join(s.name for s in self.searches)
        raise ConfigError(f"unknown search {name!r}; configured: {known}")

    def select(self, names: list[str] | None) -> tuple[Search, ...]:
        if not names:
            return self.searches
        return tuple(self.get(name) for name in names)

    def db_path(self, search: Search) -> Path:
        return self.data_dir / search.db_name


def load(path: str | Path) -> Config:
    path = Path(path)
    try:
        raw = tomllib.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        raise ConfigError(
            f"{path} not found; copy shanyrak.example.toml to {path.name} and edit it"
        ) from None
    except tomllib.TOMLDecodeError as exc:
        raise ConfigError(f"{path}: {exc}") from None

    defaults = raw.get("defaults", {})
    sections = raw.get("searches", {})
    if not sections:
        raise ConfigError(f"{path}: no [searches.*] sections")

    searches = tuple(
        _search(name, body, defaults) for name, body in sorted(sections.items())
    )
    data_dir = path.parent / raw.get("data_dir", "data")
    return Config(path=path, data_dir=data_dir, searches=searches)


def _search(name: str, body: dict, defaults: dict) -> Search:
    if not name.replace("_", "").replace("-", "").isalnum():
        raise ConfigError(f"search name {name!r} must be alphanumeric")

    section = body.get("section") or defaults.get("section")
    areas = _as_list(body.get("areas", defaults.get("areas", [])))
    if not section and not areas:
        raise ConfigError(f"[searches.{name}]: set `section`, `areas`, or both")

    filters = _merge(name, "filters", defaults, body, FILTER_KEYS)
    local = _merge(name, "local", defaults, body, LOCAL_KEYS)

    delay = _as_list(body.get("delay", defaults.get("delay", (1.0, 2.0))))
    if len(delay) != 2 or delay[0] > delay[1]:
        raise ConfigError(f"[searches.{name}]: delay must be [min, max]")

    return Search(
        name=name,
        section=_normalize_section(section or ""),
        areas=tuple(areas),
        filters=filters,
        local=Local(
            min_price_per_m2=local.get("min_price_per_m2"),
            max_price_per_m2=local.get("max_price_per_m2"),
            flag_words=tuple(local.get("flag_words", ())),
        ),
        max_pages=int(body.get("max_pages", defaults.get("max_pages", 40))),
        delay=(float(delay[0]), float(delay[1])),
        request_timeout=float(
            body.get("request_timeout", defaults.get("request_timeout", 25))
        ),
        retries=int(body.get("retries", defaults.get("retries", 3))),
    )


def _merge(name: str, key: str, defaults: dict, body: dict, allowed: frozenset) -> dict:
    merged = {**defaults.get(key, {}), **body.get(key, {})}
    unknown = set(merged) - allowed
    if unknown:
        raise ConfigError(
            f"[searches.{name}.{key}]: unknown keys {sorted(unknown)}; "
            f"allowed: {sorted(allowed)}"
        )
    return merged


def _as_list(value) -> list:
    if value is None:
        return []
    if isinstance(value, (list, tuple)):
        return list(value)
    return [value]


def _normalize_section(section: str) -> str:
    section = section.strip()
    if not section:
        return ""
    if not section.startswith("/"):
        section = "/" + section
    return section if section.endswith("/") else section + "/"
