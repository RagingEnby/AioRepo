from typing import Literal, TypedDict

Category = Literal["developer", "entertainment", "games", "lifestyle", "other", "photo-video", "social", "utilities"]


class Source(TypedDict):
    name: str
    subtitle: str | None
    description: str | None
    iconURL: str | None
    headerURL: str | None
    website: str | None
    fediUsername: str | None
    patreonURL: str | None
    tintColor: str | None
    featuredApps: list[str] | None
    apps: list[App]
    news: list[News]
    
    
class App(TypedDict):
    name: str
    bundleIdentifier: str # completely unique identifier
    marketplaceID: str # only required for altstorepal
    developerName: str
    subtitle: str | None
    localizedDescription: str
    iconURL: str
    tintColor: str | None
    category: Category | None
    screenshots: list[Screenshot] | None
    versions: list[Version]
    appPermissions: list[Permissions]
    patreon: Patreon | None
    
    
class Screenshot(TypedDict):
    imageURL: str
    width: int | None # all iPad screenshtos must provide an explicit `width`
    height: int | None # all iPad screenshtos must provide an explicit `height`
    

class Version(TypedDict):
    version: str
    buildVersion: str
    marketingVersion: str | None # If not provided, this will default to combining `version` and `buildVersion`
    date: str # This should be in **ISO 8601** format (e.g. `2023-2-17` or `2023-02-17T12:00:00-06:00`)
    localizedDescription: str
    downloadURL: str # If you're using Patreon with your app, you must attach your ADP or IPA to a Patreon post. For Altstore classic this is a url to a .ipa file. For Altstore PAL this is a url to the manifest.json in your uploaded ADP.
    size: int
    assetURLs: dict[str, str] | None
    minOSVersion: str | None
    maxOSVersion: str | None
    
    
class Permissions(TypedDict):
    entitlements: list[str]
    privacy: dict[str, str]
    
    
class News(TypedDict):
    title: str
    identifier: str # A source cannot have multiple News items with the same identifier
    caption: str
    date: str # This should be in **ISO 8601** format (e.g. `2023-2-17` or `2023-02-17T12:00:00-06:00`)
    tintColor: str | None
    imageURL: str | None
    notify: bool | None
    url: str | None
    appID: str | None
    
    
class Patreon(TypedDict):
    pledge: int | None
    currency: str | None
    benefit: str | None
    tiers: list[str] | None
    