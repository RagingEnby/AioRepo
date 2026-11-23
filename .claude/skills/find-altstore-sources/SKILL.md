---
name: find-altstore-sources
description: Take the top of the AioRepo audit queue (candidates.json) and search the web for more AltStore and SideStore sources, check them with the repo's checker, and hand the results to a High-tier model for a security audit. The auditor adds only sources it is fully confident in to urls.txt, commits, and pushes. Use when the user says "find altstore sources", "find new repos", "search for altsources", "audit the candidate queue", or asks to grow urls.txt.
---

# Find AltStore sources

You are the searcher. Your job is to collect candidate source URLs and run
one command on them. You do not judge whether a source is safe, and you never
edit `urls.txt`, commit, or push. A High-tier auditor does that.

Work from the AioRepo root (the folder with `main.go` and `urls.txt`).

## 1. Get ready

1. Run `git status --short`. If files other than `repo.json`, `repo++.json`,
   and `state/seen.json` are modified, stop and tell the user. The auditor
   commits to this checkout and must not pick up unrelated work.
2. Run `git pull --ff-only`. If it fails, stop and tell the user.
3. Make a scratch folder for this run, for example
   `mkdir -p "$TMPDIR/altsources"` (use your session scratch directory if you
   have one). Below it is called `SCRATCH`.

The repo already knows the URLs in `urls.txt`, `dead.txt`, and
`rejected.txt`. The checker marks those, so you do not need to read them.

## 2. Take the top of the queue

`candidates.json` holds sources found automatically, ranked with the most
likely to be trustworthy first. Start with those:

```sh
go run . candidates -n 25 > SCRATCH/candidates.txt
```

For each line, add a row `URL<TAB>candidates.json queue` to
`SCRATCH/found-at.tsv`.

If the user asked only to work through the queue, skip step 3.

## 3. Search the web

Spend about 15 to 25 searches. Collect every URL that looks like a source
file: it usually ends in `.json`, or is a bare domain such as
`https://alt.example.dev`. Write each one on its own line in
`SCRATCH/candidates.txt` (append, do not overwrite the queue URLs).
Duplicates are fine.

Run these web searches:

- `altstore source json`
- `altstore trusted sources`
- `sidestore source url`
- `"altstore://source?url="`
- `"add to altstore" source github`
- `awesome altstore sources list`
- `altstore repo list 2026`
- `sidestore sources list reddit`
- `site:reddit.com/r/sideloaded altstore source`
- `site:reddit.com/r/AltStore source`
- `site:github.com altstore source json`
- `<app name> altstore source` for well-known sideloaded apps, for example
  emulators (Delta, PPSSPP, Dolphin, RetroArch, MeloNX, Manic EMU, Folium),
  UTM, PojavLauncher, Aidoku, Paperback, Feather, Winston, uYouEnhanced.

Open these pages and copy every source URL from them:

- https://faq.altstore.io/altstore-classic/trusted-sources
- https://github.com/victordedomenico/awesome-altstore
- https://github.com/luh-99/sidestoreandaltstore-sources
- https://senumy.com/altstore-and-sidestore-sources
- Any other list, wiki, or gist your searches find.

How to read a page:

- A link like `altstore://source?url=X` or `sidestore://source?url=X` means
  the source is X. URL-decode X if it has `%3A%2F%2F` in it.
- A link through `intradeus.github.io/http-protocol-redirector?r=...` wraps
  one of those links. Take the URL after `url=`.
- A GitHub page for a file (`github.com/OWNER/REPO/blob/BRANCH/PATH`) becomes
  `https://raw.githubusercontent.com/OWNER/REPO/BRANCH/PATH`.
- Ignore IPA download links, App Store links, and TestFlight links. They are
  not sources.

Also save, for each URL, where you found it. Write lines like
`URL<TAB>where you found it` to `SCRATCH/found-at.tsv`. The auditor uses this.

## 4. Check the candidates

Run:

```sh
go run . check -f SCRATCH/candidates.txt > SCRATCH/report.json
```

The command fetches each URL and prints facts as JSON. It does not change any
file. Then keep only the useful rows:

```sh
jq '[.[] | select(.ok and .known == null and .installable > 0)]' SCRATCH/report.json > SCRATCH/new.json
jq 'length' SCRATCH/new.json
```

If the count is 0, tell the user that no new working sources were found,
list how many URLs you checked, and stop. Do not start the auditor.

## 5. Start the auditor

Load the `model-tiers` skill. Use the High tier: GPT 6.2 Sol on the `codex`
provider with `{"reasoningEffort":"high"}`. If Sol is not available, use
Claude Opus 5.5 on `claudeAgent` with `{"effort":"medium"}`. They are the same
tier. If neither is available, stop and tell the user. Do not use a lower tier.

In T3, call `orchestrator_capabilities` once, then `delegate_task` with
`mode: "wait"`, `role: "review"`, the target above, and the working
directory set to this checkout. Outside T3, use a native subagent that runs
one of those models. Send the brief below, with the paths filled in.

Do not touch the repo while the auditor runs.

### Auditor brief

> You are auditing candidate AltStore sources for AioRepo, a public
> aggregator that thousands of people install apps from. The checkout is
> `<repo path>`. Candidates with facts from the repo's checker are in
> `<SCRATCH>/new.json`. Where each URL was found is in
> `<SCRATCH>/found-at.tsv`. Read `.claude/skills/find-altstore-sources/AUDIT.md`
> in the checkout and follow it exactly. It says how to audit, what to
> write, and when you may commit and push. The user has authorized you to
> commit and push to the current branch under the rules in that file.
> Report back with the table it asks for.

## 6. Report

Pass the auditor's table to the user as it is. Add how many URLs you checked
and how many reached the auditor. If the auditor pushed, give the commit hash
it reported. Do not add or remove any verdicts yourself.
