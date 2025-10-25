# ⚠️WARNING⚠️
Inclusion of an app in this repository does NOT indicate endorsement. This project simply scrapes together a large repo and likely contains at least a few malicious apps. Download them at your own discretion.
# AioAltSource
[AltStore](https://altstore.io/) is a sideloading platform allowing you to install third party applications on Apple devices. There are many AltSource's made by third parties. These are rest API's/static json files that return a list of apps and their download links. These are awesome but there are a ton of repos with 1-2 apps and getting them all into your AltStore just to have a large library to browse is exhausting and damn near impossible. That's where collection repo's come in. I'm not too familiar with the history of AltSource's, but I know the idea of scraping a ton of AltSources to create one mega source is not new. I'm not the first one to do it and I won't be the last. However all public ones I could find had many issues (missing a few small apps I want, missing some larger repos, etc.). That's why I decided to make AioRepo
## Sources
I scrape the following sources to get a list of AltSources:
- `urls.txt` which is a static file I have in this repo of manually added repositories
- https://choco-ipa-library-v2.onrender.com/
- https://appdb.to/
All of these are used to gather a collection of AltSources which are then scraped to export their apps into one json file, `repo.json`.
# Installation
[AltSource (raw)](https://raw.githubusercontent.com/RagingEnby/AioRepo/refs/heads/master/repo.json) 
([Add to AltStore](https://intradeus.github.io/http-protocol-redirector?r=altstore://source?url=https://raw.githubusercontent.com/RagingEnby/AioRepo/refs/heads/master/repo.json), 
[Add to SideStore](https://intradeus.github.io/http-protocol-redirector?r=sidestore://source?url=https://raw.githubusercontent.com/RagingEnby/AioRepo/refs/heads/master/repo.json))