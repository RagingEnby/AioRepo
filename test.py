import asyncio


from main import get_source


async def main():
    source = await get_source("https://appstore.nabzclan.vip/repos/altstore.php")
    print(source)
    for app in source.apps:
        print(app.to_dict())


if __name__ == "__main__":
    asyncio.run(main())
