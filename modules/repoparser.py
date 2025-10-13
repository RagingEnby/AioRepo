from contextlib import suppress
from typing import cast
from datetime import datetime
from zoneinfo import ZoneInfo
import re

from modules import typings, asyncreqs
import constants

LOCAL_TZ = ZoneInfo("America/New_York")


def format_timestamp(timestamp: str) -> datetime:
    dt = None
    with suppress(ValueError):
        dt = datetime.fromisoformat(timestamp)
    if dt is None:
        s = timestamp.strip().replace("Z", "+00:00")
        if "T" in s:
            date_part, time_part = s.split("T", 1)
        elif " " in s:
            date_part, time_part = s.split(" ", 1)
        else:
            date_part, time_part = s, ""

        if "-" in date_part:
            parts = date_part.split("-")
            if len(parts) >= 3:
                year, month, day = parts[0], parts[1], parts[2]
                date_part = f"{year}-{month.zfill(2)}-{day.zfill(2)}"

        s = f"{date_part}T{time_part}" if time_part else date_part
        s = re.sub(r"([+-]\d{2})(\d{2})$", r"\1:\2", s)

        with suppress(ValueError):
            dt = datetime.fromisoformat(s)
        if dt is None:
            dt = datetime.strptime(date_part, "%Y-%m-%d")

    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=LOCAL_TZ)
    else:
        dt = dt.astimezone(LOCAL_TZ)
    return dt


class InvalidBundleIdentifierError(Exception):
    pass


class InvalidAppsError(Exception):
    pass


class AppVersion:
    def __init__(
        self,
        version: str,
        build_version: str,
        marketing_version: str | None,
        date: datetime,
        localized_description: str | None,
        download_url: str,
        size: int,
        asset_urls: dict[str, str] | None,
        min_os_version: str | None,
        max_os_version: str | None,
    ):
        self.version = version
        self.build_version = build_version
        self.marketing_version = marketing_version
        self.date = date
        self.localized_description = localized_description
        self.download_url = download_url
        self.size = size
        self.asset_urls = asset_urls
        self.min_os_version = min_os_version
        self.max_os_version = max_os_version
        
    def to_dict(self) -> typings.Version:
        return {
            "version": self.version,
            "buildVersion": self.build_version,
            "marketingVersion": self.marketing_version,
            "date": self.date.isoformat(),
            "localizedDescription": self.localized_description,
            "downloadURL": self.download_url,
            "size": self.size,
            "assetURLs": self.asset_urls,
            "minOSVersion": self.min_os_version,
            "maxOSVersion": self.max_os_version,
        }
        
    @classmethod
    def from_dict(cls, data: typings.Version) -> "AppVersion":
        return cls(
            version=data["version"],
            build_version=data.get("buildVersion"),
            marketing_version=data.get("marketingVersion"),
            date=format_timestamp(data["date"]),
            localized_description=data.get("localizedDescription"),
            download_url=data["downloadURL"],
            size=data["size"],
            asset_urls=data.get("assetURLs"),
            min_os_version=data.get("minOSVersion"),
            max_os_version=data.get("maxOSVersion"),
        )


class App:
    def __init__(
        self,
        name: str,
        bundle_identifier: str,
        marketplace_id: str,
        developer_name: str,
        subtitle: str | None,
        localized_description: str,
        icon_url: str,
        tint_color: str | None,
        category: typings.Category | None,
        screenshots: list[typings.Screenshot] | None,
        versions: list[AppVersion],
        app_permissions: typings.Permissions,
        patreon: typings.Patreon | None,
    ):
        self.name = name
        self.bundle_identifier = bundle_identifier
        self.marketplace_id = marketplace_id
        self.developer_name = developer_name
        self.subtitle = subtitle
        self.localized_description = localized_description
        self.icon_url = icon_url
        self.tint_color = tint_color
        self.category = category
        self.screenshots = screenshots
        self._versions = versions
        self.app_permissions = app_permissions
        self.patreon = patreon
        
    @property
    def versions(self) -> list[AppVersion]:
        versions: dict[str, AppVersion] = {}
        for version in sorted(self._versions, key=lambda x: x.date, reverse=True):
            if version.version not in versions:
                versions[version.version] = version
        return list(versions.values())
        
    @property
    def latest_version(self) -> AppVersion | None:
        try:
            return self.versions[0]
        except IndexError:
            return None
        
    @property
    def last_updated(self) -> datetime:
        latest_version = self.latest_version
        return latest_version.date if latest_version else datetime.fromtimestamp(0, LOCAL_TZ)
        
    def to_dict(self) -> typings.App:
        return {
            "name": self.name,
            "bundleIdentifier": self.bundle_identifier,
            "marketplaceID": self.marketplace_id,
            "developerName": self.developer_name,
            "subtitle": self.subtitle,
            "localizedDescription": self.localized_description,
            "iconURL": self.icon_url,
            "tintColor": self.tint_color,
            "category": self.category,  # type: ignore
            "screenshots": self.screenshots,
            "versions": [version.to_dict() for version in self.versions],
            "appPermissions": self.app_permissions,
            "patreon": self.patreon,
        }
        
    @classmethod
    def from_dict(cls, data: typings.App) -> "App":
        if not data.get("bundleIdentifier"):
            raise InvalidBundleIdentifierError(f"Invalid bundle identifier for {data['name']}: {data.get('bundleIdentifier')}")
        return cls(
            name=data["name"],
            bundle_identifier=data["bundleIdentifier"],
            marketplace_id=data.get("marketplaceID"),
            developer_name=data.get("developerName", "Unknown"),
            subtitle=data.get("subtitle"),
            localized_description=data.get("localizedDescription", data["name"]),
            icon_url=data["iconURL"] or constants.ICON_URL,
            tint_color=data.get("tintColor"),
            category=data.get("category"),
            screenshots=data.get("screenshots"),
            versions=[AppVersion.from_dict(version) for version in data.get("versions", [])],
            app_permissions=data.get("appPermissions", {"entitlements": [], "privacy": {}}),
            patreon=data.get("patreon"),
        )
        
        
class Source:
    def __init__(
        self,
        name: str,
        subtitle: str | None,
        description: str | None,
        icon_url: str | None,
        header_url: str | None,
        website: str | None,
        fedi_username: str | None,
        patreon_url: str | None,
        tint_color: str | None,
        featured_apps: list[str] | None,
        apps: list[App],
        news: list[typings.News],
    ):
        self.name = name
        self.subtitle = subtitle
        self.description = description
        self.icon_url = icon_url
        self.header_url = header_url
        self.website = website
        self.fedi_username = fedi_username
        self.patreon_url = patreon_url
        self.tint_color = tint_color
        self.featured_apps = featured_apps
        self.apps = apps
        self.news = news
        
    def to_dict(self) -> typings.Source:
        return {
            "name": self.name,
            "subtitle": self.subtitle,
            "description": self.description,
            "iconURL": self.icon_url,
            "headerURL": self.header_url,
            "website": self.website,
            "fediUsername": self.fedi_username,
            "patreonURL": self.patreon_url,
            "tintColor": self.tint_color,
            "featuredApps": self.featured_apps,
            "apps": [app.to_dict() for app in self.apps],
            "news": self.news,
        }
        
    @classmethod
    def from_dict(cls, data: typings.Source) -> "Source":
        if not data.get("apps"):
            raise InvalidAppsError(f"No apps found for {data['name']}")
        apps = []
        for app in data["apps"]:
            with suppress(InvalidBundleIdentifierError):
                apps.append(App.from_dict(app))
        return cls(
            name=data["name"],
            subtitle=data.get("subtitle"),
            description=data.get("description"),
            icon_url=data.get("iconURL"),
            header_url=data.get("headerURL"),
            website=data.get("website"),
            fedi_username=data.get("fediUsername"),
            patreon_url=data.get("patreonURL"),
            tint_color=data.get("tintColor"),
            featured_apps=data.get("featuredApps"),
            apps=apps,
            news=data.get("news", []),
        )
        
        
async def get_source(url: str) -> Source:
    response = await asyncreqs.get(url)
    return Source.from_dict(response.json())
