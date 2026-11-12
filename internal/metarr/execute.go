package metarr

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/file"
	"tubarr/internal/metadata"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// runningFiles holds a map of files already being processed by Metarr.
var runningFiles sync.Map

// tryJobMap checks if the filepath is currently being ran.
func tryJobMap(filepath string) (alreadyRunning bool) {
	_, alreadyRunning = runningFiles.LoadOrStore(filepath, true)
	return alreadyRunning
}

// clearJobMapEntry clears the job from the map on completion.
func clearJobMapEntry(filepath string) {
	runningFiles.Delete(filepath)
}

// InitMetarr begins processing with Metarr.
func InitMetarr(metarrCtx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) error {
	// Metarr may move the file, changing v.Files.VideoFilePath, so clear the path that was taken.
	path := v.Files.VideoFilePath
	if tryJobMap(path) {
		return fmt.Errorf("metarr is already processing video %q", path)
	}
	defer clearJobMapEntry(path)

	args := makeMetarrCommand(v, cu, c, dirParser)
	if len(args) == 0 {
		c.Log().I("No Metarr arguments built, returning...")
		return nil
	}

	//nolint:gosec // Metarr with the args Tubarr builds.
	cmd := exec.CommandContext(metarrCtx, sharedconsts.ProgramMetarr, args...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	if vars.MetarrLogToken != "" {
		// The channel goes in the log URL, so the Logs page can show this run's lines with the channel's.
		logURL := vars.MetarrLogURL + "?" + url.Values{consts.WebParamLogChannel: {c.Name}}.Encode()
		cmd.Env = append(cmd.Environ(),
			sharedconsts.EnvTubarrLogToken+"="+vars.MetarrLogToken,
			sharedconsts.EnvTubarrLogURL+"="+logURL)
	}

	if err := runMetarr(cmd, v); err != nil {
		return err
	}
	return finishMetafile(v, cu)
}

// finishMetafile takes the title, description and date from the metadata file Metarr finished with, then deletes the
// file if the purge setting says to. Tubarr purges rather than Metarr, so it can read the file first.
func finishMetafile(v *models.Video, cu *models.ChannelURL) error {
	if v.Files.JSONFilePath == "" {
		return nil
	}
	if err := metadata.RefreshFromMetarr(v); err != nil {
		cu.Log().E("Failed to read the metadata Metarr wrote for %q, keeping the stored title and description: %v", v.URL, err)
	}

	purge, err := metaPurge(v, cu)
	if err != nil {
		return err
	}
	if purge != "all" && purge != "json" { // Tubarr's metadata files are JSON.
		return nil
	}
	if err := os.Remove(v.Files.JSONFilePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to purge metadata file %q: %w", v.Files.JSONFilePath, err)
	}
	cu.Log().I("Purged metadata file %q", v.Files.JSONFilePath)
	v.Files.JSONFilePath = ""
	return nil
}

// metaPurge returns which metadata files to delete once Metarr finishes with v: Download As's setting, else the channel
// URL's.
func metaPurge(v *models.Video, cu *models.ChannelURL) (string, error) {
	var o string
	if v.Overrides != nil {
		o = v.Overrides.MetaPurge
	}
	return validation.ValidatePurgeMetafiles(cmp.Or(o, cu.ChanURLMetarrArgs.MetaPurge))
}

// runWithProgress runs a Metarr command, recording its progress lines for the video and returning the rest of stdout.
func runWithProgress(cmd *exec.Cmd, videoID int64) (string, error) {
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	defer progressByVideo.Delete(videoID)

	var out strings.Builder
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if p, ok := parseProgress(sc.Text()); ok {
			progressByVideo.Store(videoID, p)
			continue
		}
		out.WriteString(sc.Text())
		out.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		logger.Pl.E("Metarr output scanner for video with ID %d encountered error: %v", videoID, err)
	}
	_, _ = io.Copy(io.Discard, pipe) // Lets Metarr finish if a line was too long to scan.
	return out.String(), cmd.Wait()
}

// runMetarr runs a Metarr command with a built argument list.
func runMetarr(cmd *exec.Cmd, v *models.Video) error {
	v.Log().I("Running Metarr command:\n\n%s\n", cmd.String())

	cmd.Stderr = os.Stderr

	// Run command and grab stdout.
	metarrStdout, err := runWithProgress(cmd, v.ID)
	if err != nil {
		// Metarr still reports where the files are after a failed rename or move.
		if storeErr := v.StoreFilenamesFromMetarr(metarrStdout); storeErr != nil {
			v.Log().D(1, "No file paths stored from failed Metarr run for %q: %v", v.URL, storeErr)
		}
		// Metarr's own account of why it failed, rather than just its exit status.
		if msgs := metarrErrors(metarrStdout); len(msgs) > 0 {
			return errors.New(strings.Join(msgs, "; "))
		}
		return err
	}

	// Retrieve filenames.
	if err := v.StoreFilenamesFromMetarr(metarrStdout); err != nil {
		return err
	}

	// Get file size.
	if size := file.Size(v.Files.VideoFilePath); size > 0 {
		v.Metadata.Filesize = size
	}

	// Success.
	v.Log().S("Metarr successfully processed video %q (file path: %q)", v.URL, v.Files.VideoFilePath)
	return nil
}

// metarrErrors returns the errors Metarr reported on stdout, from its "metarr error: " lines.
func metarrErrors(stdout string) []string {
	var msgs []string
	for l := range strings.SplitSeq(stdout, "\n") {
		if msg, ok := strings.CutPrefix(l, "metarr error: "); ok && msg != "" && !slices.Contains(msgs, msg) {
			msgs = append(msgs, msg)
		}
	}
	return msgs
}
