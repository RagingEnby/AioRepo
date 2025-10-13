import curl_cffi

SESSION: curl_cffi.AsyncSession | None = None


async def get_session() -> curl_cffi.AsyncSession:
    global SESSION
    if SESSION is None:
        SESSION = curl_cffi.AsyncSession(impersonate="chrome136")
    return SESSION


async def close():
    global SESSION
    if SESSION is not None:
        await SESSION.close()


async def get(*args, **kwargs) -> curl_cffi.Response:
    session = await get_session()
    return await session.get(*args, **kwargs)
