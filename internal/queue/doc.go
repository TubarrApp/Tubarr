// Package queue holds Tubarr's two queues: videos waiting to be downloaded or post-processed, and those running.
//
// Both queues are created once at startup and passed to whatever needs them:
//
//	postProcessing := queue.NewPostProcessingQueue(store)
//	downloads := queue.NewDownloadQueue(ctx, store, postProcessing)
//
// # Downloading videos
//
// To download videos, call [DownloadQueue.Add] with the channel and one [Request] per video. Add is the only way a
// video enters the download queue, and it handles everything after that:
//
//   - It claims each video in the database, so nothing else downloads it at the same time. A URL the channel doesn't
//     have yet is stored first. Videos already queued or downloading are left out of what Add returns.
//   - It shows each video on the Downloads page as queued, then starts it in the background.
//   - Each video waits for a slot in its channel's lane (see Concurrency below), fetches its metadata and checks it
//     against the channel's filters, waits for a download slot, then downloads.
//   - A downloaded video is handed to the post-processing queue to run Metarr, then stored as finished.
//   - Every video leaves the queue with a final status: Finished, Ignored (filtered out), Canceled, or Failed with why.
//     Canceled and Failed videos go back to the Available list.
//
// Add returns as soon as the videos are queued, without waiting for them to download. To wait, as terminal commands
// do before exiting, call [DownloadQueue.Wait].
//
// A Request says who asked for the video, which changes how it's treated:
//
//   - Crawled videos (ByUser false) go through the channel's download filters, and a failed one is only queued
//     again as a crawl retry.
//   - Videos the user chose from Available or entered as a URL (ByUser true) skip the filters, can be downloaded again
//     whatever their status (unless already queued or downloading), and use their Download As options.
//   - Auth is login details for this download only, never stored. ReplaceOverrides stores Overrides as the video's
//     Download As options, nil clearing them.
//
// # Stopping downloads
//
//   - [DownloadQueue.Cancel] cancels one video, waiting or downloading. It returns to the Available list as Canceled.
//   - [DownloadQueue.Stop] cancels a channel's videos that haven't started downloading, and lets those downloading
//     finish. Stopped videos the user chose return as Canceled, crawled ones as Failed so a later crawl retries them.
//   - [DownloadQueue.Abort] cancels all of a channel's videos, downloading ones too.
//
// The Stop Queue and Abort All buttons call these on both queues, as well as stopping the channel's crawl.
//
// # Post-processing
//
// The download queue hands videos to the post-processing queue itself, so only Run Metarr calls into it directly,
// with [PostProcessingQueue.RunMetarr]. Its Cancel, Stop and Abort work as the download queue's do, and a downloaded
// video it doesn't run (stopped, or Metarr can't run) is left with Metarr pending for Run Metarr to process later.
//
// A video is only ever in one queue. Adding a video to the download queue cancels any Metarr run on it first, and Run
// Metarr skips videos in the download queue.
//
// # Concurrency
//
// Each channel has a lane: how many of its videos download at once (its download concurrency setting). A channel URL
// with its own setting gets a lane of its own. Videos in a lane start in the order they were added. On top of the
// lanes, every download needs a slot under the global and per-domain download limits, and every Metarr run a slot
// under the channel's Metarr concurrency and the global post-processing limit.
//
// # Showing the queues
//
//   - Entries returns each queue's videos for the Downloads and Post-processing pages.
//   - [DownloadQueue.Channels] returns the channels with videos in the download queue, and those being stopped.
//   - Len and Wait report whether a queue is empty, for the idle endpoint and for terminal commands.
package queue
