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


async def main():
    try:
        repo_urls = await reposources.all()
        print(len(repo_urls))
        sources = await get_sources(*repo_urls)
        apps: list[repoparser.App] = []
        for source in sources:
            apps.extend(source.apps)
        # await write("output/apps.json", [app.to_dict() for app in apps])
        filtered_apps: dict[str, repoparser.App] = {}
        for app in apps:
            if not app.versions or app.is_pal:
                continue
            if app.bundle_identifier in filtered_apps:
                last_updated = filtered_apps[app.bundle_identifier].last_updated
                if app.last_updated > last_updated:
                    filtered_apps[app.bundle_identifier] = app
            else:
                filtered_apps[app.bundle_identifier] = app
        # await write("output/filtered_apps.json", {k: app.to_dict() for k, app in filtered_apps.items()})
        date = datetime.datetime.now().strftime("%-m/%-d/%y")
        aio_source = repoparser.Source(
            name="RagingEnby's AIO Source",
            subtitle=f"[{date}] Every single repo I could find, united.",
            description="I got really sick and tired of typing in a billion different repos. So this one repo scrapes ~100 AltSources and combines them into one, beautiful, united repository.",
            icon_url=constants.ICON_URL,
            header_url=None,
            website="https://ragingenby.dev/",
            fedi_username=None,
            patreon_url="https://patreon.com/RagingEnby",
            tint_color=None,
            featured_apps=None,
            apps=sorted(
                filtered_apps.values(), key=lambda app: app.last_updated, reverse=True
            ),
            news=[],
        )
        await write("repo.json", aio_source.to_dict())  # type: ignore
    finally:
        await asyncreqs.close()


if __name__ == "__main__":
    asyncio.run(main())
