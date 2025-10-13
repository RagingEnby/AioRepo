import asyncio
import aiofiles
from modules import asyncreqs
import re
import json


async def get_appdb_repos() -> set[str]:
    response = await asyncreqs.get("https://api.dbservices.to/v1.7/get_repos/?lang=en&lt=&st=&is_public=1&brand=appdb")
    data = response.json()
    return {repo["url"] for repo in data["data"]}


async def get_choco_sources() -> set[str]:
    response = await asyncreqs.get("https://choco-ipa-library-v2.onrender.com/")
    m = re.search(r"const\s+defaultRepos\s*=\s*(\[[\s\S]*?\]);", response.text)
    if not m:
        return set()
    blob = m.group(1)
    try:
        items = json.loads(blob)
        return {s.strip() for s in items if isinstance(s, str) and s.strip()}
    except Exception:
        urls = re.findall(r"https?://[^\s\"'\]]+", blob)
        return {u.strip() for u in urls if u.strip()}


async def get_manual_repos() -> set[str]:
    repo_urls: set[str] = set()
    async with aiofiles.open("urls.txt", "r") as f:
        async for line in f:
            line = line.strip()
            if line:
                repo_urls.add(line)
    return repo_urls


async def all() -> set[str]:
    results = await asyncio.gather(
        get_appdb_repos(),
        get_choco_sources(),
        get_manual_repos(),
    )
    return set().union(*results)
