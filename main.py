import asyncio
import json
import aiofiles
import datetime
from curl_cffi.requests import exceptions as curl_exceptions

from modules import asyncreqs, reposources, repoparser
import constants


async def write(file_path: str, data: dict | list | str):
    if isinstance(data, dict) or isinstance(data, list):
        data = json.dumps(data, indent=2)

    async with aiofiles.open(file_path, "w") as f:
        await f.write(data)


async def get_source(url: str) -> repoparser.Source | None:
    try:
        source = await repoparser.get_source(url)
        # await write(f"output/{source.name}.json", source.to_dict())  # type: ignore
        print(source.name)
        return source
    except (
        curl_exceptions.DNSError,
        curl_exceptions.Timeout,
        repoparser.InvalidAppsError,
        repoparser.InvalidNameError,
        json.JSONDecodeError,
        curl_exceptions.InvalidURL,
        curl_exceptions.CertificateVerifyError,
        curl_exceptions.ConnectionError,
    ) as e:
        print("Error processing", url, e)
        return None


async def get_sources(*urls: str) -> list[repoparser.Source]:
    sources = await asyncio.gather(*[get_source(url) for url in urls])
    return [source for source in sources if source is not None]


async def write_source(
    apps: list[repoparser.App], spam: bool = False
) -> repoparser.Source:
    now = datetime.datetime.now()
    date_str = (
        f"[{now.month}/{now.day}/{now.year % 100:02d} {now.hour:02d}:{now.minute:02d}]"
    )
    description = "I got really sick and tired of typing in a billion different repos. So this one repo scrapes 200-300 AltSources and combines them into one, beautiful, united repository."
    subtitle = f"{date_str} Every single repo I could find, united."
    if spam:
        description = (
            "RagingEnby's AIO Source but with ONLY spam apps (99% Nabzclan slop)"
        )
        subtitle = f"{date_str} {description}"
    aio_source = repoparser.Source(
        name=f"RagingEnby's AIO Source{'++' if spam else ''}",
        subtitle=subtitle,
        description=description,
        icon_url=constants.ICON_URL,
        header_url=None,
        website="https://ragingenby.dev/",
        fedi_username=None,
        patreon_url="https://patreon.com/RagingEnby",
        tint_color=None,
        featured_apps=None,
        apps=sorted(apps, key=lambda app: app.last_updated, reverse=True),
        news=[],
    )
    file_name = "repo.json" if not spam else "repo++.json"
    await write(file_name, aio_source.to_dict())  # type: ignore
    return aio_source


def is_spam(app: repoparser.App) -> bool:
    if not app.subtitle:
        return False
    if app.subtitle.startswith("Injected with "):
        return True
    if app.developer_name == "Holloway":
        return True
    return False


async def main():
    try:
        repo_urls = await reposources.all()
        print(len(repo_urls))
        sources = await get_sources(*repo_urls)
        apps: list[repoparser.App] = []
        for source in sources:
            apps.extend(source.apps)
        # await write("output/apps.json", [app.to_dict() for app in apps])
        no_duplicates: dict[str, repoparser.App] = {}
        for app in apps:
            if not app.versions or app.is_pal:
                continue
            if app.bundle_identifier in no_duplicates:
                last_updated = no_duplicates[app.bundle_identifier].last_updated
                if app.last_updated > last_updated:
                    no_duplicates[app.bundle_identifier] = app
            else:
                no_duplicates[app.bundle_identifier] = app
        # await write("output/filtered_apps.json", {k: app.to_dict() for k, app in filtered_apps.items()})
        spam_apps = []
        rest_apps = []
        for app in no_duplicates.values():
            if is_spam(app):
                spam_apps.append(app)
            else:
                rest_apps.append(app)
        await asyncio.gather(
            write_source(rest_apps, spam=False),
            write_source(spam_apps, spam=True),
        )
    finally:
        await asyncreqs.close()


if __name__ == "__main__":
    asyncio.run(main())
