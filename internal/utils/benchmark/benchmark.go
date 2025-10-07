// Package benchmark sets up and initiated benchmarking.
//
// Includes CPU profiling, memory profiling, and tracing.
package benchmark

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"time"
	"tubarr/internal/utils/logging"
)

// BenchFiles contain benchmarking files written on a benchmark-enabled run.
type BenchFiles struct {
	cpuFile   *os.File
	memFile   *os.File
	traceFile *os.File
}

const (
	cpuProf    = "cpu_"
	memProf    = "mem_"
	traceOut   = "trace_"
	profExt    = ".prof"
	outExt     = ".out"
	timeFormat = "2006-01-02 15:04:05.00"
	timeTag    = "2006-01-02_15:04:05"

	cpuProfBaseLen  = len(cpuProf) + len(profExt)
	memProfBaseLen  = len(memProf) + len(profExt)
	traceOutBaseLen = len(traceOut) + len(outExt)
)

var (
	mainWd,
	cpuProfPath,
	memProfPath,
	traceOutPath string
)

// InjectMainWorkDir injects the main.go path variable into this package.
func InjectMainWorkDir(mainGoPath string) {
	mainWd = filepath.Dir(mainGoPath)
}

// SetupBenchmarking sets up and initiates benchmarking for a program run.
func SetupBenchmarking() (*BenchFiles, error) {
	var err error
	b := new(BenchFiles)

	benchStartTime := time.Now().Format(timeFormat)
	makeBenchFilepaths(mainWd)

	logging.I("(Benchmarking this run. Start time: %s)", benchStartTime)

	// CPU profile
	b.cpuFile, err = os.Create(cpuProfPath)
	if err != nil {
		CloseBenchFiles(b, "", fmt.Errorf("could not create CPU profiling file: %w", err))
		return nil, err
	}

	if err := pprof.StartCPUProfile(b.cpuFile); err != nil {
		CloseBenchFiles(b, "", fmt.Errorf("could not start CPU profiling: %w", err))
		return nil, err
	}

	// Memory profile
	b.memFile, err = os.Create(memProfPath)
	if err != nil {
		CloseBenchFiles(b, "", fmt.Errorf("could not create memory profiling file: %w", err))
		return nil, err
	}

	// Trace
	b.traceFile, err = os.Create(traceOutPath)
	if err != nil {
		CloseBenchFiles(b, "", fmt.Errorf("could not create trace file: %w", err))
		return nil, err
	}
	if err := trace.Start(b.traceFile); err != nil {
		CloseBenchFiles(b, "", fmt.Errorf("could not start trace: %w", err))
		return nil, err
	}

	return b, nil
}

// CloseBenchFiles closes bench files on program termination.
func CloseBenchFiles(b *BenchFiles, noErrExit string, setupErr error) {

	if b.cpuFile != nil {
		pprof.StopCPUProfile()
		if err := b.cpuFile.Close(); err != nil {
			logging.E("Failed to close file %q: %v", b.cpuFile.Name(), err)
		}
	}

	if b.traceFile != nil {
		if err := b.traceFile.Close(); err != nil {
			logging.E("Failed to close file %q: %v", b.traceFile.Name(), err)
		}
	}

	if b.memFile != nil {
		runtime.GC()
		if err := pprof.WriteHeapProfile(b.memFile); err != nil {
			logging.E("Could not write memory profile: %v", err)
		}
		if err := b.memFile.Close(); err != nil {
			logging.E("Failed to close file %q: %v", b.memFile.Name(), err)
		}
	}

	if setupErr != nil {
		logging.E("Benchmarking failure: %v", setupErr)
	}

	logging.I("%s", noErrExit)
}

// makeBenchFilepaths makes paths for benchmarking files.
func makeBenchFilepaths(cwd string) {
	time := time.Now().Format(timeTag)
	var b strings.Builder

	b.Grow(cpuProfBaseLen + len(time))
	b.WriteString(cpuProf)
	b.WriteString(time)
	b.WriteString(profExt)

	// Benchmarking files
	cpuProfPath = filepath.Join(cwd, b.String())
	logging.I("Made CPU profiling file: %q", cpuProfPath)
	b.Reset()

	b.Grow(memProfBaseLen + len(time))
	b.WriteString(memProf)
	b.WriteString(time)
	b.WriteString(profExt)

	memProfPath = filepath.Join(cwd, b.String())
	logging.I("Made mem profiling file: %q", memProfPath)
	b.Reset()

	b.Grow(traceOutBaseLen + len(time))
	b.WriteString(traceOut)
	b.WriteString(time)
	b.WriteString(outExt)

	traceOutPath = filepath.Join(cwd, b.String())
	logging.I("Made trace file: %q", traceOutPath)
}
