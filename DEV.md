# Tubarr developer guide

Function names are given as `Type.Method` or `package.Function`. Links go to the file that holds them. The queues have their own package doc with more detail on their rules: `go doc ./internal/queue`.

- [The pieces](#the-pieces)
- [Startup and shutdown](#startup-and-shutdown)
- [Channels, channel URLs and settings](#channels-channel-urls-and-settings)
- [Crawling: finding new videos](#crawling-finding-new-videos)
- [Available: videos waiting for the user](#available-videos-waiting-for-the-user)
- [Downloading a video](#downloading-a-video)
- [Post-processing with Metarr](#post-processing-with-metarr)
- [Stopping and canceling](#stopping-and-canceling)
- [Concurrency and limits](#concurrency-and-limits)
- [Bot detection and blocked domains](#bot-detection-and-blocked-domains)
- [Where state lives](#where-state-lives)
- [The web server](#the-web-server)
- [Terminal commands](#terminal-commands)
- [The database](#the-database)
- [Adding a setting](#adding-a-setting)
- [Tests and linting](#tests-and-linting)

---

## The pieces

Tubarr is one Go binary that schedules work and keeps records. The heavy lifting is done by other programs it runs:

- **yt-dlp** lists a channel's videos, fetches each video's metadata JSON, and downloads the video.
- **Metarr** (a separate program) renames, tags and transcodes each downloaded video, and moves it to its output directory.
- **SQLite**, at `~/.tubarr/tubarr.db`, holds channels, videos, download status, settings and logins.
- The **web UI** is the HTML pages in [web/](web/), served by the binary. There is no build step: [web/style.css](web/style.css) is prebuilt Tailwind (only classes already in it work), and [web/bundle.js](web/bundle.js) is edited by hand.

Code shared with Metarr (the operation and filter grammar, logging, constants) is in the `github.com/TubarrApp/gocommon` module, imported as `sharedparsing`, `sharedfilters`, `sharedconsts` and so on.

| Package | Job |
|---|---|
| [cmd/tubarr](cmd/tubarr/) | `main`: startup, the single-instance lock, the heartbeat, shutdown |
| [internal/cfg](internal/cfg/) | The terminal commands (`tubarr channel …`, `profile …`, `auth …`), and loading saved global settings before any command runs |
| [internal/cmd](internal/cmd/) | Flag registration shared by the commands |
| [internal/app](internal/app/) | Crawling channels, and turning user choices into download requests |
| [internal/scraper](internal/scraper/) | Finding a channel's video URLs (yt-dlp or site rules), cookies, logins, FlareSolverr, scraping metadata from sites yt-dlp can't read |
| [internal/queue](internal/queue/) | The download queue and the post-processing queue, and the per-channel lanes |
| [internal/downloads](internal/downloads/) | Building and running the yt-dlp commands for metadata and video, retries, bot-detection checks |
| [internal/downloads/downloaders](internal/downloads/downloaders/) | Reading progress from yt-dlp and external downloaders |
| [internal/metadata](internal/metadata/) | Reading a video's metadata JSON, download filters, filtered ops, move ops |
| [internal/metarr](internal/metarr/) | Building and running the Metarr command, Download As ops, transcode progress |
| [internal/repo](internal/repo/) | All SQL, as stores behind the interfaces in [internal/contracts](internal/contracts/) |
| [internal/database](internal/database/) | Opening the database, creating tables, adding columns |
| [internal/models](internal/models/) | The data types: `Channel`, `ChannelURL`, `Video`, `Settings`, `MetarrArgs`, `Profile` |
| [internal/parsing](internal/parsing/) | Turning op strings, filters and config files into models, filling directory templates |
| [internal/validation](internal/validation/) | Checking settings when they're saved and when they're loaded |
| [internal/limits](internal/limits/) | Global and per-domain concurrency limits |
| [internal/blocking](internal/blocking/) | Domains blocked after bot detection |
| [internal/server](internal/server/) | The HTTP API and the web pages |
| [internal/server/serverauth](internal/server/serverauth/) | Web logins, sessions, the API key, trusted proxies |
| [internal/siterules](internal/siterules/) | Built-in and user scrape rules, matched by the URL's host (the most specific domain wins) |
| [internal/state](internal/state/) | Which channels are crawling, and crawl jitter |
| [internal/notify](internal/notify/) | Calling notification URLs |
| [internal/file](internal/file/) | Channel config files, ops files, thumbnails |
| [internal/times](internal/times/) | Bot-avoidance waits |
| [internal/auth](internal/auth/) | Encrypting stored passwords, parsing login details |
| [internal/domain](internal/domain/) | Constants: flag names (`keys`), database columns and statuses (`consts`), web form keys (`jsonkeys`), Metarr flags (`metkeys`), yt-dlp flags (`command`), file locations (`paths`), the shared logger |

---

## Startup and shutdown

Everything starts in [cmd/tubarr/main.go](cmd/tubarr/main.go):

1. `init` calls `paths.InitProgFilesDirs`, which creates `~/.tubarr` and the files Tubarr keeps there.
2. `main` sets up logging (`logging.SetupLogging`, stored in `logger.Pl`).
3. `initializeApplication` ([setup.go](cmd/tubarr/setup.go)):
   1. `database.InitDB` opens SQLite and creates or updates the tables.
   2. `repo.InitStores` builds the stores.
   3. `ProgControl.StartTubarr` takes the single-instance lock, a row in the `program` table kept fresh by the heartbeat. It's skipped for `help`, `-h` and `auth reset` (`skipsInstanceLock`), which may run beside a running Tubarr.
   4. With the lock held, the previous run's leftovers are repaired: `VideoStore.RecoverInterrupted` (see [Restart recovery](#restart-recovery)), `VideoStore.ReconnectOrphanedVideos`, and unused thumbnails are removed (`file.RemoveUnusedThumbnails`, `file.RemoveUnusedCachedThumbnails`).
4. `file.LoadMetarrLogs` and `blocking.LoadBlockedDomains` load what the web pages show.
5. The two queues are made, once for the life of the process: `queue.NewPostProcessingQueue`, then `queue.NewDownloadQueue`, which hands its finished downloads to the first.
6. `startHeartbeat` ([control.go](cmd/tubarr/control.go)) runs in the background.
7. `cfg.InitCommands` registers the flags and commands, and `cfg.Execute` runs Cobra. Before any command runs, `rootCmd.PersistentPreRunE` ([internal/cfg/cfg.go](internal/cfg/cfg.go)) loads the saved global settings from the `settings` table, unless a flag sets them for that run: log level, crawl, download and post-processing concurrency, domain limits, the scrape rules file and the FlareSolverr URL. It applies the global limits (`limits.SetGlobal*Limit`) and loads the scrape rules into `siterules`. A rules file that can't be read is logged, and the built-in rules are used until it's fixed.
8. Then one of:
   - `--web`: `server.StartServer` runs until the process is told to stop.
   - bare `tubarr`: `times.StartupWait` (a random wait of up to 15 minutes, skipped with `-s`), then `app.CheckChannels` crawls what's due.
   - any other command runs and returns.
9. Terminal runs wait for both queues to empty (`DownloadQueue.Wait`, `PostProcessingQueue.Wait`), so queued downloads finish before Tubarr exits.
10. Shutdown cancels the context, waits up to 5 seconds for the queues to store their videos' final status, and `cleanup` calls `ProgControl.QuitTubarr` to release the lock.

---

## Channels, channel URLs and settings

### The models

- A **channel** ([`models.Channel`](internal/models/channels.go)) has a name, its own `ChanSettings` (`models.Settings`) and `ChanMetarrArgs` (`models.MetarrArgs`), an optional profile, an optional config file, and its URLs in `URLModels`.
- A **channel URL** ([`models.ChannelURL`](internal/models/channel_urls.go)) is one address the channel crawls. It can have its own settings, Metarr args, profile and login (`Login`). `Session` holds what a crawl or download fetched for it, such as cookies.
- Every channel also has a hidden **manual downloads** channel URL (`IsManual` true, URL `consts.ManualDownloadsCol`). Videos sent in by URL go through it, and so do videos whose channel URL was deleted (they have channel URL ID 0). `queue.ManualChannelURL` returns it, creating it the first time.
- A **profile** ([`models.Profile`](internal/models/profiles.go)) is a named `Settings` and `MetarrArgs` pair that channels and channel URLs link to by ID. Nothing is copied from it, it's merged in when the channel loads.

### Loading a channel

`ChannelStore.GetChannelModel` and `ChannelStore.GetAllChannels` ([internal/repo/channel.go](internal/repo/channel.go)) both end in `loadChannelDetails`:

1. The channel's saved JSON settings are decoded, and `validateLoadedSettings` checks them. Invalid settings don't stop the load: they set `Channel.LoadErr`, so the channel still shows on the web pages, but crawls, downloads and Metarr runs refuse it until it's fixed.
2. With `mergeURLsWithParent` true (what every job uses), the channel's profile fills in the settings the channel leaves blank (`mergeSettings`, `mergeMetarrArgs`), the list settings are stacked (`stackLists`), and output directory tags are filled (`parsing.FillChannelOutputDirectory`).
3. `GetChannelURLModels` ([internal/repo/channel_urls.go](internal/repo/channel_urls.go)) loads the URLs. Merged, each URL's blank settings are filled first from its own profile, then from its channel (`inheritChannelSettings`). A URL without a JSON directory saves metadata beside its own video directory if it has one, else in the channel's JSON directory. A channel without one saves metadata in its video directory.

Single values come from the nearest level that sets them: **URL → URL's profile → channel → channel's profile**. Lists (download filters, move ops, meta ops, filename ops and both filtered op lists) are added together across the levels instead, unless a level's `*ReplaceInherited` flag drops its parent's list (`withoutReplacedSettings`, `withoutReplacedMetarrArgs`). Each level's lists are kept in `Channel.Levels` and `ChannelURL.Levels`, so run-time code can add each level's ops file in its place.

Load with `mergeURLsWithParent` false when you want only what's saved for that level, such as on an edit page.

Each loaded channel records when it was read (`LoadedAt`). The queues only take limits from a channel read later than the last one they took limits from. This stops a long crawl, which loaded the channel before you saved new limits, from putting the old limits back when it queues its videos.

### Ops and filter files

A level can name an ops or filter file as well as typed ops. Files are read when they're used, not when the channel loads: `metarr.loadAndMergeMetaOps` and `loadAndMergeFilenameOps` for Metarr, `metadata.ChannelURLFilters` and friends for filters. A file that can't be read holds things back rather than running without it:

- a broken ops file leaves the URL's videos with Metarr pending (`metarr.SkipReason`).
- a broken download filter file stops crawled videos queuing at all (`app.crawlRequests`), so nothing downloads unfiltered.

### Channel config files

A channel can point at a YAML, TOML or JSON file holding its settings. `file.UpdateFromConfigFile` calls `ChannelStore.UpdateChannelFromConfig`, which re-reads the file (`parsing.LoadChannelInputFile`), writes any changes to the database, and reloads the model. It runs at most once per loaded model (`Channel.UpdatedFromConfig`), and is called before a crawl, in `DownloadQueue.Add`, before Run Metarr, and when the Download As dialog opens. A file that can't be used sets `Channel.LoadErr`, so the channel is held back as for invalid saved settings, rather than run with what was saved before.

### Saving a channel

- **Web**: the add and edit forms post to `handleCreateChannel` and `handleUpdateChannel` ([internal/server/handlers.go](internal/server/handlers.go)). `parseChannelForm` ([internal/server/helpers.go](internal/server/helpers.go)) reads the channel's settings from form fields (`settingsFromForm`, `metarrArgsFromForm`) and each URL's from the `url_settings` JSON (`parseURLSettings`). Both go through `parsing.ApplySettingsInput` and `parsing.ApplyMetarrArgsInput`, which validate. An update runs in `ChannelStore.WithTx`, so a failure changes nothing, then `setQueueLimits` applies the new limits to queued videos.
- **Terminal**: `tubarr channel add` (`addChannelCmd`) builds a `models.ChannelInput` from the flags and calls `app.AddChannelFromInput`, which calls `parsing.BuildChannelFromInput` and `ChannelStore.AddChannelWithDetails`.
- **Config files**: `tubarr channel add-batch`, and the web's add-from-file and add-from-directory, load `ChannelInput` from the files and add them the same way.

---

## Crawling: finding new videos

### What starts a crawl

| Trigger | Path |
|---|---|
| Web, on schedule | `serverStore.startCrawlWatchdog` ([internal/server/methods.go](internal/server/methods.go)) ticks every 10 seconds and calls `crawlDueChannels`. That reloads every channel, applies their limits to the queues, and for each channel works out which URLs are due (`Channel.DueURLs`, with `times.CalculateJitter` added: a random few minutes picked once per crawl, cleared when the crawl marks its URLs scanned). It reloads the config file, skips paused channels, takes the channel's crawl lock (`state.TryLockCrawl`) and a global crawl slot (`limits.GlobalCrawls`), then calls `app.CrawlChannel` with only the due URLs. |
| Web, Crawl button | `handleCrawlChannel`. An optional `url_ids` body picks the URLs, paused ones included. |
| Terminal, bare `tubarr` | `app.CheckChannels` ([internal/app/crawler.go](internal/app/crawler.go)) does the same as the watchdog, once, with its own semaphore sized by the crawl concurrency setting and no jitter. |
| Terminal, `tubarr channel crawl` | `crawlChannelCmd` takes the crawl lock and calls `app.CrawlChannel`. |

A URL is due when its crawl frequency (its own, else the channel's, `Channel.URLCrawlFreq`) has passed since its `LastScan`.

### `app.CrawlChannel`

1. `filterAllowedValidURLs` drops manual URLs, URLs whose domain is blocked (`queue.DomainBlocked`), and URLs whose download destination isn't there (`queue.CheckDestinations`, which catches an unmounted drive). If none are left, the URLs are marked as scanned and the crawl ends.
2. `times.WaitTime` waits a random few seconds, against bot detection.
3. `Scraper.GetNewReleases` ([internal/scraper/scrape.go](internal/scraper/scrape.go)) finds the new videos:
   1. `ChannelStore.GetDownloadedOrIgnoredVideoURLs` returns the URLs the channel has already **seen** (finished, ignored, Available, Canceled, queued, downloading, or Failed with no crawl retries left), and the failed ones a crawl can still retry.
   2. For each URL, after a wait between URLs: a per-domain crawl slot (`limits.DomainCrawls`), then cookies and any login (`CookieManager.GetChannelURLCookies`, kept in `cu.Session`). Each URL's cookies are its own: the crawl only shares the browser cookies it read for a domain, and FlareSolverr solves for a host.
   3. `newEpisodeURLs` lists the URL's videos. Site crawl rules come first (`crawlChannelURLs`, matched with `siterules.MatchCrawl`), else yt-dlp (`ytDlpURLFetch`, running `yt-dlp --flat-playlist -J`). Seen URLs are dropped (`ignoreDownloadedURLs`).
   4. Crawl retries: on a URL that downloads automatically, each failed video found again uses one retry (`ChannelStore.AddCrawlDLRetries`), up to `consts.MaxCrawlDLRetries`. On a URL that doesn't, failed videos are left to the user (`ChannelStore.UseUpCrawlDLRetries`).
   5. Each new URL becomes a `models.Video`, in the channel's listing order, with the title, thumbnail, date and duration the listing showed.
4. For each channel URL with new videos:
   - If it doesn't download automatically (`Channel.AutoDownloads` is false), the videos are made Available (see below) and the channel's notification dot is set (`ChannelStore.AddNewVideoURLs`).
   - Otherwise `crawlRequests` makes a `queue.Request` per video (refusing them all if a filter file is broken) and passes them to `DownloadQueue.Add`.
5. However the crawl ended, its URLs are marked as scanned (`UpdateChannelURLsLastScan`, `UpdateLastScan`), so each waits its crawl frequency again.

### Ignore crawls

An ignore crawl marks everything a channel lists now as done, so only videos after it are new. `app.CrawlChannelIgnore` calls `GetNewReleases` with `ignoreCrawl` true, marks each video ignored (`Video.MarkVideoAsIgnored`) and stores them with `VideoStore.AddVideos`. It runs from the web's Ignore Crawl button (`handleIgnoreCrawlChannel`), when a channel is added with "ignore run" set, and from `tubarr channel ignore-crawl`. `tubarr channel ignore-video-url` stores one URL the same way (`ChannelStore.AddURLToIgnore`).

---

## Available: videos waiting for the user

A video is **Available** when its `downloads` row has status `Available` and its `videos` row isn't finished.

### How a video becomes Available

- **Found by a crawl** on a URL that doesn't download automatically: `VideoStore.AddAvailableVideos` ([internal/repo/video.go](internal/repo/video.go)) inserts the `videos` and `downloads` rows. It uses `ON CONFLICT DO NOTHING`, so a video the channel already has (downloading, failed, anything) isn't reset.
- **Sent in by URL** to Available rather than downloaded: `handleAddAvailableURLs`, or `tubarr channel download-video-urls --send-to-available`, both call `app.AddManualAvailable`.
- **A download that failed or was canceled**: `DownloadQueue.finish` stores Failed or Canceled, and the Available page lists those too (`app.AvailableStatuses`).

### Listing and acting on them

The Available page calls `handleGetAvailableVideos`, which reads `VideoStore.GetAvailableVideos` (with Ignored added for `?ignored=1`). A failed video on a URL that downloads automatically is shown with the crawl retries it has left.

Choices post to `/api/v1/channels/{id}/available`, handled by `handleAvailableAction`:

| Action | What happens |
|---|---|
| `download` | Any Download As options are parsed and checked (`parseOverrideLines`, `checkOverridesFit`), dropped if they match the channel URL's anyway (`matchesChannelURL`). Then `DownloadQueue.Add(c, app.AvailableRequests(...)...)`, as chosen by the user. |
| `dismiss` | `VideoStore.ClaimVideos` moves them to Ignored, and `app.RemoveDismissedFromNew` clears them from the notification dot. |
| `restore` | `VideoStore.ClaimVideos` moves Ignored videos back to Available. |

The terminal has the same: `tubarr channel list-available`, `download-available` and `dismiss-available` ([internal/cfg/channel.go](internal/cfg/channel.go)) call the same functions.

---

## Downloading a video

Read [internal/queue/doc.go](internal/queue/doc.go) for the queue's rules. This section is the call path.

### Into the queue: `DownloadQueue.Add`

[internal/queue/downloads.go](internal/queue/downloads.go). This is the only way a video enters the download queue, from crawls, Available, manual URLs and the terminal alike.

1. `file.UpdateFromConfigFile`, refusing the channel if that sets `Channel.LoadErr`, then `SetLimits` on both queues with this copy of the channel.
2. For each `queue.Request`:
   1. A video with no ID yet gets the channel URL it downloads through (`DownloadQueue.channelURL`).
   2. `VideoStore.QueueVideo` claims it in the database, setting it to Queued. A crawled video can only be claimed from Failed (a crawl retry), whereas one the user chose can be claimed from anything except Queued or Downloading. A URL the channel doesn't have yet is inserted. A video that isn't claimed is skipped. Claiming clears the video's ignored and finished flags, so it shows on the Available page if this download fails.
   3. `newDownload` stores Download As options if the request replaces them, and finds the `session` it shares with the request's other videos through the same channel URL and login on the same host, so they share cookies and the check on whether Metarr can run. A session for a host other than the channel URL's leaves out the crawl's cookies and fetches its own for the video's host.
   4. `insert` puts it in the queue and `go DownloadQueue.run` starts it. A URL another channel is downloading fails, while a video claimed again between its own download's attempts is skipped.

### Through the queue: `DownloadQueue.run`

[internal/queue/download_steps.go](internal/queue/download_steps.go).

1. `PostProcessingQueue.hold` reserves the video for this download, canceling any Metarr run on it first.
2. `DownloadQueue.process`:
   1. `acquireLane` waits for a slot in the channel's lane, or its URL's own lane (see [Concurrency](#concurrency-and-limits)).
   2. `session.ready` runs once per session: it fetches cookies for the session's host (`Scraper.ScraperURLCookies`) unless a crawl already did, and decides whether Metarr can run (`metarr.CheckInstalled`, `metarr.SkipReason`), and whether it should (`MetarrArgs.AutoPostProcess`, Post-Process After Download). The manual Post-Process path only calls `SkipReason`, so turning the setting off never blocks the buttons.
   3. Blocked domains fail fast: `downloads.CheckIfAvoidURL` and `queue.DomainBlocked`.
   4. A random bot-avoidance wait.
   5. `prepare` works out where the video goes: a directory parser for the templates, the video's Download As options if the user chose it (`VideoStore.GetVideoOverrides`, `withYtdlpOverrides`), `CheckDestinations`, the filled video and JSON directories, whether Metarr is held back (`Video.SkipMetarr`), and the channel URL's current lines in Download As ops (`metarr.FollowChannelURLOps`).
   6. A per-domain download slot, then a global one (`limits.DomainDownloads`, `limits.GlobalDownloads`), held until the download ends.
   7. `executeCustomScraper`: a site with metadata scrape rules has its JSON written by `Scraper.ScrapeCustomSite` instead of yt-dlp.
   8. `processJSON`:
      - `downloads.NewJSONDownload(...).Execute()` ([internal/downloads/json_dl_manager.go](internal/downloads/json_dl_manager.go)) runs `yt-dlp --skip-download --write-info-json`, built by `JSONDownload.buildJSONCommand`, retrying on failure.
      - `metadata.ValidateAndFilter` ([internal/metadata/validator.go](internal/metadata/validator.go)) reads the JSON (`parseAndStoreJSON`: title, date, description, duration, thumbnail), applies the download filters and date range (`handleFilters`, skipped for videos the user chose and manual URLs), and picks the filtered meta and filename ops that match.
      - A video that fails the filters is marked ignored (`MarkVideoAsIgnored`), stored with `VideoStore.AddVideo`, has its JSON deleted, and leaves the queue as Ignored. One that passes is stored with its metadata. A JSON file or filter that can't be used fails the download instead.
   9. `metadata.HandleMoveOps` picks a move op's output directory if one matches (`Video.Files.MoveOpOutputDir`).
   10. `startDownloading` marks it downloading. From here, Stop Queue lets it finish.
   11. `processVideo` calls `downloads.NewVideoDownload(...).Execute()` ([internal/downloads/video_dl_manager.go](internal/downloads/video_dl_manager.go)). It tries up to `downloadAttempts` times (1 plus the Download Retries setting). Each attempt builds the command (`VideoDownload.buildVideoCommand` in [video.go](internal/downloads/video.go): output named after the JSON file, Tubarr's progress template, cookies, codecs, external downloader, extra args) and runs it (`executeVideoDownload`), reading every output line:
       - progress lines go through `downloaders.ProgressParser.Parse` to `download.SendUpdate`.
       - the line starting `command.FinalPathPrefix` gives the finished file's path.
       - bot-detection text makes `checkBotDetection` block the domain and stop without retrying.
3. `DownloadQueue.finish` works out the final status (`finalStatus`: Finished, Ignored, Canceled or Failed with why), stores it (`storeStatus` → `DownloadStore.UpdateDownloadStatus`), and takes the video out of the queue. Ignored ones stay on the Downloads page for 5 seconds so it can show they were filtered out.
4. A finished download goes on to `DownloadQueue.postProcess` (next section).
5. `notifyIfLast` counts the video's run as over, then the hold is released. Once none of the channel's videos are downloading or post-processing, `notify.Services` calls the notification URLs for the channel URLs that got new videos. So they're called after Metarr has placed the files, or when the downloads end if Metarr is held back.

### What the Downloads page shows

`download.SetStatus` and `download.SendUpdate` keep each video's latest status in memory (`download.status`), and store it whenever the status changes. The Downloads page polls `/api/v1/downloads`, served by `handleGetDownloads` from `DownloadQueue.Entries`, with an ETA from `downloads.TrackETA` and `downloads.ETA`.

---

## Post-processing with Metarr

### After a download

`DownloadQueue.postProcess` ([download_steps.go](internal/queue/download_steps.go)):

1. If Metarr is held back (`Video.SkipMetarr.Skip`), the video is finished with Metarr pending.
2. Otherwise `VideoStore.UpdateVideo` stores the file's path first, so a restart mid-Metarr can recover the video. Then `videoHold.postProcess` ([internal/queue/postprocessing.go](internal/queue/postprocessing.go)):
   - If the channel was stopped since this download took the video, Metarr isn't started and the video stays pending.
   - Otherwise it waits for a slot in the channel's Metarr lane and a global post-processing slot (`PostProcessingQueue.acquire`), then calls `execMetarr`, which calls `metarr.InitMetarr` ([internal/metarr/execute.go](internal/metarr/execute.go)):
     1. `makeMetarrCommand` ([build.go](internal/metarr/build.go)) builds the arguments: the video's Download As ops, or else the channel URL's ops (files and typed ops, plus the filtered ops that matched). The output directory (`parseMetarrOutputDir`: Download As, then a matching move op, then the channel URL's). The extension, rename style, transcode settings, and so on.
     2. `runMetarr` runs it through `runWithProgress`, which keeps FFmpeg's progress for the Post-processing page (`metarr.VideoProgress`). `Video.StoreFilenamesFromMetarr` reads where Metarr left the files from its "final video path:" and "final json path:" lines.
     3. `finishMetafile` re-reads the JSON Metarr left (`metadata.RefreshFromMetarr`), so titles changed by meta ops are stored, then deletes it if the purge setting says so.
   - The video's `MetarrStatus` becomes `done`, `failed` (with `MetarrError`), or `pending` if the run was canceled.
3. `completeAndStoreVideo` marks it finished (`Video.MarkVideoAsComplete`), stores it (`VideoStore.UpdateVideo`) and sets the notification dot. `addNewVideo` then records its channel URL for the notification URLs (step 5 above).

### Running Metarr later

The Post-Process buttons on a channel's videos page (Post-Process pending, Post-Process Selected, Edit & Post-Process Selected) post to `handleRunMetarr`, which can save new Download As options for the videos first (`VideoStore.SetVideoOverrides`), then calls `PostProcessingQueue.RunMetarr(ctx, channel, videoIDs, rerun)`. That loads the videos (`VideoStore.GetVideosAwaitingMetarr`) and, for each, `runVideoMetarr`: it re-reads the JSON for the filtered ops and move ops, then calls `execMetarr` as above. The terminal's `tubarr channel run-metarr` calls the same. Once the run ends, `notify.Services` calls the notification URLs again for the channel URLs whose videos Metarr finished, as it may have moved their files.

A video is in at most one queue: `hold` cancels a Metarr run when the video is downloaded again, and `RunMetarr` skips videos a download holds.

### Restart recovery

`VideoStore.RecoverInterrupted` runs at startup with the lock held. A downloaded video with its file stored but not yet finished becomes finished with Metarr pending. Anything still Queued or Downloading becomes Failed ("Tubarr stopped before this video finished"), so it shows in Available again.

---

## Stopping and canceling

| Button | API | Calls |
|---|---|---|
| Cancel (Downloads page) | `POST /api/v1/downloads/cancel` | `DownloadQueue.Cancel` for each video |
| Stop Queue | `POST /api/v1/channels/{id}/stop-queue` | `state.StopCrawl`, `DownloadQueue.Stop`, `PostProcessingQueue.Stop` |
| Abort All | `POST /api/v1/channels/{id}/abort` | `state.StopCrawl`, `DownloadQueue.Abort`, `PostProcessingQueue.Abort` |
| Stop Post-Processing | `POST /api/v1/channels/{id}/stop-metarr` | `PostProcessingQueue.Cancel` |

Each download has two contexts. `ctx` is canceled by Cancel, Abort and shutdown. `preDownloadCtx` is also canceled by Stop, which only reaches videos not yet downloading. The cancel cause (`downloads.ErrCanceledByUser`, `errStopped`, `errAborted`) is what `finalStatus` reads to choose the stored status.

yt-dlp runs in its own process group (`useProcessGroup` in [proc_unix.go](internal/downloads/proc_unix.go)), so canceling it also stops ffmpeg or aria2c under it. A user cancel deletes the partial files yt-dlp reported (`removePartialFiles`).

---

## Concurrency and limits

| Limit | Set by | Enforced in |
|---|---|---|
| A channel's (or URL's) download concurrency | the channel's settings | the download queue's `lanes` (`downloadLimits`) |
| A channel's (or URL's) Metarr concurrency | the channel's Metarr settings | the post-processing queue's `lanes` (`metarrLimits`) |
| Global downloads | Settings page, `--global-download-concurrency` | `limits.GlobalDownloads` |
| Per-domain downloads | Settings page, Domain Limits | `limits.DomainDownloads` |
| Global crawls | Settings page, `--crawl-concurrency` | `limits.GlobalCrawls` (web), a local semaphore in `app.CheckChannels` (terminal) |
| Per-domain crawls | Settings page, Domain Limits | `limits.DomainCrawls`, in `GetNewReleases` |
| Global post-processing | Settings page, `--post-process-concurrency` | `limits.GlobalPostProcessing` |

The global and per-domain limits are `limits.Semaphore`s. Their setting handlers change the limit in place, and it applies straight away.

Lanes ([internal/queue/queue.go](internal/queue/queue.go)) give each channel a slot count, and a channel URL with its own limit a lane of its own. Videos are given slots in the order they asked, and a changed limit applies to videos already running or waiting. After saving a channel's or profile's settings, reload the channels and pass them to `DownloadQueue.SetLimits` and `PostProcessingQueue.SetLimits`. The web server does this in `setQueueLimits` after each save, and the watchdog does it for every channel every 10 seconds.

---

## Bot detection and blocked domains

- `downloads.checkBotDetection` ([dl_helpers.go](internal/downloads/dl_helpers.go)) looks for "not a bot" or "not a robot" in a failed yt-dlp run's error. If found, `blocking.BlockDomain` ([internal/blocking/blocking.go](internal/blocking/blocking.go)) blocks the domain, and the download stops without retrying.
- Blocks are by registered domain (`network.RegisteredDomain`, so `www.youtube.com` and `youtube.com` are one) and by **block context**: unauthenticated, cookies, or login (`blocking.GetBlockContext`). A block on unauthenticated requests doesn't stop a channel URL that logs in.
- `downloads.CheckIfAvoidURL` checks before each download attempt, and `queue.DomainBlocked` before crawling and downloading.
- Blocks expire (`blocking.GetTimeoutForDomain`). `blocking.CleanExpiredBlocks` runs before crawls. The Blocked Domains page lifts one with `handleUnblockDomain`, and the terminal with `tubarr channel unblock`.

---

## Where state lives

| What | Where |
|---|---|
| Download status, percentage, last error, crawl retries used | `downloads` table: `status`, `percentage`, `error`, `crawl_download_retries` |
| Finished and ignored flags, file paths, title and other metadata, Metarr status and error, Download As options | `videos` table |
| Channel, channel URL and profile settings | JSON columns `settings` and `metarr` on `channels`, `channel_urls` and `profiles` |
| The notification dot | `channels.new_video_notification` and `channels.new_video_urls` |
| Global settings, the web login, the API key | `settings` table, key and value (`TubarrSettingsStore`) |
| Blocked domains | `blocked_domains` and `blocked_domain_videos` tables, mirrored in `vars.BlockedDomains` |
| Videos queued, downloading or in Metarr | memory only: the two queues |
| Which channels are crawling | memory only: [internal/state](internal/state/) |

The download statuses are the `consts.DLStatus*` constants and the Metarr statuses the `consts.MetarrStatus*` constants, both in [internal/domain/consts/db.go](internal/domain/consts/db.go).

---

## The web server

- `server.StartServer` ([internal/server/router.go](internal/server/router.go)) sets up logins (`serverauth.New`), builds the router (`NewRouter`), serves HTTP or HTTPS ([tls.go](internal/server/tls.go)), and starts the crawl watchdog.
- Middleware, in order: request ID, compression, request logging (except `/health` and the activity poll), panic recovery, cross-origin protection, a 10 MB body limit, then `serverauth.Manager.Middleware` ([serverauth.go](internal/server/serverauth/serverauth.go)), which accepts a session cookie or an `X-Api-Key` header and lets `isPublicPath` paths through.
- Every API route is listed in `NewRouter`. Handlers are in [handlers.go](internal/server/handlers.go) and [profiles.go](internal/server/profiles.go), and form and JSON parsing is in [helpers.go](internal/server/helpers.go).
- Pages: `StaticHandler` serves `web/` from beside the binary. Each `.html` page goes through `fillIncludes`, which replaces `<!-- include:name key="value" -->` with `web/partials/name.html`, filling its `{{key}}` placeholders. The settings fields shared by the add channel, edit channel and profile pages are [web/partials/settings-fields.html](web/partials/settings-fields.html).
- The pages poll `/api/v1/channels/activity` (which channels are crawling, downloading, stopping or in Metarr), `/api/v1/downloads` and `/api/v1/post-processing`.
- Metarr's logs: `metarr.InitMetarr` starts Metarr with `TUBARR_LOG_URL` and `TUBARR_LOG_TOKEN` set, and Metarr posts its log lines to `/metarr-logs` (`handlePostMetarrLogs`) every 2 seconds, which the Logs page shows. The reply is Tubarr's debug level, which Metarr switches to, so a level change reaches Metarr runs already going.
- Channel logs: work done for a channel logs through `logger.Pl.Channel(name)`, which adds a `channel` field to the JSON line. Metarr's lines get the same field from the `channel` query parameter `InitMetarr` puts on `TUBARR_LOG_URL`: Metarr reads it at startup and tags its own lines with `logging.SetChannel`, so its log file keeps the channel too, and `withLogChannel` tags any line that arrives without one. The Logs page filters on the field in the browser, and `logs.html?channel=Name` opens it filtered. Log through `Channel` in new crawl, download and post-processing code.
- Debug levels, in Tubarr, Metarr and gocommon. Each level also shows the levels below it, and 0 shows no debug lines.
  1. Why did it skip or change something? A video failing a filter, a crawl that didn't start, a blocked URL, a file left unrenamed, a video being transcoded.
  2. What is it doing? Each stage of a crawl, download or Metarr run: a file loaded, cookies saved, an op applied, a rename, a notification sent.
  3. What did it go through? Every video entry, cookie, metadata field and database row a stage handles. A big crawl writes hundreds.
  4. What came back? The whole output from yt-dlp, a website or FFprobe, scraped metadata, headers and form data. Lines are long and can hold cookies and tokens.
  5. Everything, nonstop: a line on every watchdog check (every 10 seconds), every line of download output, and the full metadata after each Metarr edit.

  Validation and parsing that run whenever a channel loads don't log, since the crawl watchdog loads every channel every 10 seconds. Don't log that a function was entered or that nothing is configured.

---

## Terminal commands

The commands in [internal/cfg](internal/cfg/) call the same functions as the web handlers, and their downloads go through the same queues. `main` waits for the queues to empty before it exits.

| Command | Calls |
|---|---|
| `tubarr` | `app.CheckChannels` |
| `tubarr channel crawl` | `app.CrawlChannel` |
| `tubarr channel ignore-crawl` | `app.CrawlChannelIgnore` |
| `tubarr channel add`, `add-batch` | `app.AddChannelFromInput` |
| `tubarr channel download-video-urls` | `DownloadQueue.Add(c, app.ManualRequests(...)...)`, or `app.AddManualAvailable` with `--send-to-available` |
| `tubarr channel download-available` | `DownloadQueue.Add(c, app.AvailableRequests(...)...)` |
| `tubarr channel dismiss-available` | `VideoStore.ClaimVideos` |
| `tubarr channel run-metarr` | `PostProcessingQueue.RunMetarr` |
| `tubarr channel delete-videos` | `ChannelStore.DeleteVideosByURLs` |
| `tubarr profile add`, `edit`, `delete`, `list` | `ChannelStore.AddProfile`, `UpdateProfile`, `DeleteProfile`, `GetAllProfiles` |
| `tubarr auth reset` | `serverauth.ResetLogin` |

---

## The database

- `database.InitDB` ([internal/database/database.go](internal/database/database.go)) opens SQLite and calls `initTables`. Each table is created from its file in [internal/database/sql](internal/database/sql/) with `CREATE TABLE IF NOT EXISTS`, then `addColumnIfMissing` adds columns introduced since.
- To add a column: add it to the table's `.sql` file **and** add an `addColumnIfMissing` call, since existing databases never re-run `CREATE TABLE`. Add its name as a `consts.Q*` constant. Saved rows from older versions must still load.
- The stores are `ChannelStore` (channels, channel URLs, profiles, notifications, logins), `VideoStore`, `DownloadStore` and `TubarrSettingsStore`, built by `repo.InitStores` and used through the interfaces in [internal/contracts/stores.go](internal/contracts/stores.go). Use `ChannelStore.WithTx` for changes that must all happen or none.
- SQL is built with `fmt.Sprintf` from the `consts` column names, with every value passed as a `?` argument. Column names that come from callers are checked with `validKey`.

---

## Adding a setting

These are the places a channel setting is wired, using `ExtraYTDLPCrawlArgs` as the example. Search for it to see each one.

1. **Model**: the field and its JSON tag on `models.Settings` ([internal/models/settings.go](internal/models/settings.go)), plus `ChildSettingsMatchParent` and `SettingsAllZero` in the same file.
2. **Inheritance**: `mergeSettings` ([internal/repo/channel_urls.go](internal/repo/channel_urls.go)), unless it's a filter or op list (download filters, move ops, meta ops, filename ops, filtered ops), which is stacked instead (`models.Stack*` in [levels.go](internal/models/levels.go)). Other lists, such as codecs, are inherited whole like single values (`inheritSlice`).
3. **Input and validation**: `models.SettingsInput` ([internal/models/vars.go](internal/models/vars.go), its `viper` tag is the config file key) and `parsing.ApplySettingsInput` ([internal/parsing/viper.go](internal/parsing/viper.go)). Add a check to `validation.ValidateSettingsModel` if a saved value can be wrong.
4. **Terminal**: the flag name in `keys` ([internal/domain/keys/terminal.go](internal/domain/keys/terminal.go)), the flag in [internal/cmd/cobra.go](internal/cmd/cobra.go), `models.ChannelFlagValues` ([flags.go](internal/models/flags.go)), `mergeFlagsIntoInput` ([internal/cfg/helpers.go](internal/cfg/helpers.go)), `clearURLSettings` ([internal/cfg/channel_urls.go](internal/cfg/channel_urls.go)), and `displaySettingsStruct` ([internal/repo/channel.go](internal/repo/channel.go)).
5. **Web**: the form key in `jsonkeys` ([internal/domain/jsonkeys/json_keys.go](internal/domain/jsonkeys/json_keys.go)), `settingsFromForm` (channel and profile forms), `settingsInput` and its `input` method (each URL's settings), and `settingsResponse` with `newSettingsResponse` (sent to the pages), all in [internal/server/helpers.go](internal/server/helpers.go), then the field in [web/partials/settings-fields.html](web/partials/settings-fields.html), [url-settings-fields.html](web/partials/url-settings-fields.html) for channel URLs, and wherever add-channel, edit-channel, list-channels and profiles read it.
6. **Use**: wherever it takes effect, such as `ytDlpURLFetch` for this one.

A `MetarrArgs` setting follows the same pattern with `mergeMetarrArgs`, `ChildMetarrArgsMatchParent`, `MetarrArgsAllZero`, `models.MetarrArgsInput`, `parsing.ApplyMetarrArgsInput`, `validation.ValidateMetarrArgsModel`, `metarrArgsFromForm`, `metarrArgsInput`, `newMetarrArgsResponse`, and finally `makeMetarrCommand`, which passes it to Metarr.

---

## Tests and linting

- Each package keeps its tests in one file, `zz_<package>_test.go`, so it sorts after the code. Sections inside it start with a `// ******** Name ****` divider (instead of having a tonne of different test files sprayed everywhere) add new tests to the matching section, or a new one. A package with tests in an external `_test` package (`parsing`, `validation`) has a second file for those, `zz_<package>_external_test.go`.
- `go test ./...` takes about 11 seconds. Tests that really download need yt-dlp, FFmpeg and Metarr, and only run when asked: `TUBARR_LIVE_TESTS=1 go test -timeout 20m ./internal/downloads ./internal/server`.
- Lint with `custom-gcl run ./...` from the repo root. It's golangci-lint built with the nilaway plugin that [.golangci.yml](.golangci.yml) needs. Plain `golangci-lint` fails on that config.
