# ⚠️WARNING⚠️
Inclusion of an app in this repository does NOT indicate endorsement. This project simply scrapes together a large repo and likely contains at least a few malicious apps. Download them at your own discretion. Every app's description ends with the name of the source it came from.
# AioAltSource
[AltStore](https://altstore.io/) is a sideloading platform allowing you to install third party applications on Apple devices. There are many AltSource's made by third parties. These are rest API's/static json files that return a list of apps and their download links. These are awesome but there are a ton of repos with 1-2 apps and getting them all into your AltStore just to have a large library to browse is exhausting and damn near impossible. That's where collection repo's come in. I'm not too familiar with the history of AltSource's, but I know the idea of scraping a ton of AltSources to create one mega source is not new. I'm not the first one to do it and I won't be the last. However all public ones I could find had many issues (missing a few small apps I want, missing some larger repos, etc.). That's why I decided to make AioRepo
## Sources
I scrape the following lists of AltSources:
- `urls.txt`, a list of repositories I added by hand or that passed a security audit
- https://choco-ipa-library-v2.onrender.com/
- https://appdb.to/

Both AltStore sources and Scarlet repos are read. All of these are scraped and their apps exported into one json file, `repo.json`.

New sources are only scraped after a security audit. Candidates wait in `candidates.json`, ranked by how likely they are to be trustworthy (GitHub stars, with forks, archived repos, and repos idle for a year ranked lower). They come from:
- "add source" links (`altstore://source?url=...`) and `sourceURL` fields inside the sources each run scrapes
- a weekly job that searches GitHub code for AltStore and Scarlet source files
- manual web searches

Sources in `candidates.json` are reviewed with `go run . candidates` and `go run . check`. Trusted sources move to `urls.txt`, and rejected ones are recorded in `rejected.txt`.

Sources that fail every run for 7 days move to `dead.txt` and are skipped. Delete a line from `dead.txt` to try it again.

When two sources have the same app (same bundle ID), `urls.txt` wins over Choco, and Choco wins over appdb. A less trusted source can only replace an app when its version is newer and the developer name matches. Copies by the same developer have their versions merged, up to 10 per app.
# Installation
The spam repo contains only apps made by spam authors such as Nabzclan. The main repo includes none of these.
## Main Repo
[AltSource (raw)](https://raw.githubusercontent.com/RagingEnby/AioRepo/refs/heads/master/repo.json) 
([Add to AltStore](https://intradeus.github.io/http-protocol-redirector?r=altstore://source?url=https://raw.githubusercontent.com/RagingEnby/AioRepo/refs/heads/master/repo.json) | 
[Add to SideStore](https://intradeus.github.io/http-protocol-redirector?r=sidestore://source?url=https://raw.githubusercontent.com/RagingEnby/AioRepo/refs/heads/master/repo.json) | [Online View](https://altsource.by.lao.sb/browse/?source=https%3A%2F%2Fraw.githubusercontent.com%2FRagingEnby%2FAioRepo%2Frefs%2Fheads%2Fmaster%2Frepo.json))
## Spam Repo
[AltSource (raw)](https://raw.githubusercontent.com/RagingEnby/AioRepo/refs/heads/master/repo++.json) 
([Add to AltStore](https://intradeus.github.io/http-protocol-redirector?r=altstore://source?url=https://raw.githubusercontent.com/RagingEnby/AioRepo/refs/heads/master/repo++.json) | 
[Add to SideStore](https://intradeus.github.io/http-protocol-redirector?r=sidestore://source?url=https://raw.githubusercontent.com/RagingEnby/AioRepo/refs/heads/master/repo++.json) | [Online View](https://altsource.by.lao.sb/browse/?source=https%3A%2F%2Fraw.githubusercontent.com%2FRagingEnby%2FAioRepo%2Frefs%2Fheads%2Fmaster%2Frepo%2B%2B.json))
## Smaller sources and the browse page
The site at https://ragingenby.github.io/AioRepo/ lets you search every app and shows which sources worked in the last run. It also hosts smaller sources:
- `recent.json`: apps updated in the last 90 days
- `category/<name>.json`: one source per AltStore category (games, utilities, photo-video, and so on)
- `repo.json` and `repo++.json` without whitespace, which download faster

For example, `https://ragingenby.github.io/AioRepo/recent.json`.
# Running
The scraper is written in Go. It writes `repo.json` and `repo++.json` in the AltStore classic source format (the sideload build, not AltStore PAL).
```sh
go run .            # build the sources
go run . discover   # queue GitHub search results for audit (needs GITHUB_TOKEN)
go run . candidates -n 25   # print the top of the audit queue
go run . check URL  # print facts about candidate sources as JSON
go test ./...
go test -run TestGolden -update   # rewrite testdata/golden after an intended parser change
```
## Config files
- `spam.txt`: rules that send apps to `repo++.json`
- `block.txt`: rules that drop apps from every output
- `featured.txt`: bundle IDs featured at the top of `repo.json`

A rule is `<field> <op> <value>`, for example `developer is Holloway` or `bundle contains happymod`. Fields are name, developer, subtitle, bundle, icon, download, source, and description. Ops are is, contains, prefix, suffix, and regex. Matching ignores case.
## Files the scraper writes
- `state/seen.json`: when each app first appeared, used for the "new apps" news items, and first-seen dates for Scarlet apps, which have none
- `.cache/`: the last good copy of each source and its failure count. CI keeps it with actions/cache. If a source fails, its copy from the last 3 days is used, so its apps do not drop out for a run.
- `_site/`: the GitHub Pages build

A run fails instead of writing when `repo.json` would lose more than 30% of its apps. Set `AIOREPO_FORCE=1`, or tick "force" when starting the workflow by hand, to write anyway.
## Workflows
- `Update repo` runs every 6 hours and on manual dispatch. It commits `repo.json` and `repo++.json` only when an app changed, then publishes the Pages site.
- `Discover sources` runs weekly and queues new sources in `candidates.json`. Nothing it finds is scraped before the audit. Set a `DISCOVERY_TOKEN` secret (a fine-grained token with public repository read access) if the default token cannot use code search.
- `CI` runs gofmt, go vet, staticcheck, and the tests on pushes and pull requests.
