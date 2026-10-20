package metarr

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
)

// InitMetarr begins processing with Metarr.
func InitMetarr(metarrCtx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) error {
	args := makeMetarrCommand(v, cu, c, dirParser)
	if len(args) == 0 {
		logger.Pl.I("No Metarr arguments built, returning...")
		return nil
	}

	cmd := exec.CommandContext(metarrCtx, "metarr", args...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	if vars.MetarrLogToken != "" {
		cmd.Env = append(cmd.Environ(),
			consts.EnvLogToken+"="+vars.MetarrLogToken,
			consts.EnvLogURL+"="+vars.MetarrLogURL)
	}

	if err := runMetarr(cmd, v); err != nil {
		return err
	}
	return nil
}

// runMetarr runs a Metarr command with a built argument list.
func runMetarr(cmd *exec.Cmd, v *models.Video) error {
	logger.Pl.I("Running Metarr command:\n\n%s\n", cmd.String())

	cmd.Stderr = os.Stderr

	// Run command and grab stdout.
	metarrStdout, err := cmd.Output()
	if err != nil {
		// Metarr still reports where the files are after a failed rename or move.
		if storeErr := v.StoreFilenamesFromMetarr(string(metarrStdout)); storeErr != nil {
			logger.Pl.D(1, "No file paths stored from failed Metarr run for %q: %v", v.URL, storeErr)
		}
		// Metarr's own account of why it failed, rather than just its exit status.
		if msgs := metarrErrors(string(metarrStdout)); len(msgs) > 0 {
			return errors.New(strings.Join(msgs, "; "))
		}
		return err
	}

	// Retrieve filenames.
	if err := v.StoreFilenamesFromMetarr(string(metarrStdout)); err != nil {
		return err
	}

	// Success.
	logger.Pl.S("Metarr successfully processed video %q (file path: %q)", v.URL, v.VideoFilePath)
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
