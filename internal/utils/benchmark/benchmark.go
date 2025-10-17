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
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/setup"
	"tubarr/internal/utils/logging"
)

var BenchmarkFiles *BenchFiles

// BenchFiles contain benchmarking files written on a benchmark-enabled run.
type BenchFiles struct {
	cpuFile   *os.File
	memFile   *os.File
	traceFile *os.File
}

var (
	cpuProfPath,
	memProfPath,
	traceOutPath string
)

// CloseBenchmarking closes benchmark files if they exist
func CloseBenchmarking() {
	if BenchmarkFiles != nil {
		CloseBenchFiles(BenchmarkFiles, fmt.Sprintf("Benchmark ended at %v", time.Now().Format(time.RFC1123Z)), nil)
		BenchmarkFiles = nil
	}
}

// SetupBenchmarking sets up and initiates benchmarking for a program run.
func SetupBenchmarking() (*BenchFiles, error) {
	var err error
	b := new(BenchFiles)

	startTime := time.Now().Format("2006-01-02_15-04-05")
	makeBenchFilepaths(setup.BenchmarkDir, startTime)

	logging.I("(Benchmarking this run. Start time: %s)", startTime)

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
	if b == nil {
		return
	}

	if b.cpuFile != nil {
		logging.I("Stopping CPU profile...")
		pprof.StopCPUProfile()
		if err := b.cpuFile.Close(); err != nil {
			logging.E("Failed to close file %q: %v", b.cpuFile.Name(), err)
		}
		b.cpuFile = nil // Prevent double-close
	}

	if b.traceFile != nil {
		logging.I("Stopping trace...")
		trace.Stop()
		if err := b.traceFile.Close(); err != nil {
			logging.E("Failed to close file %q: %v", b.traceFile.Name(), err)
		}
		b.traceFile = nil // Prevent double-close
	}

	if b.memFile != nil {
		logging.I("Writing memory profile...")
		runtime.GC()
		if err := pprof.WriteHeapProfile(b.memFile); err != nil {
			logging.E("Could not write memory profile: %v", err)
		}
		if err := b.memFile.Close(); err != nil {
			logging.E("Failed to close file %q: %v", b.memFile.Name(), err)
		}
		b.memFile = nil // Prevent double-close
	}

	if setupErr != nil {
		logging.E("Benchmarking failure: %v", setupErr)
	}
	logging.I("%s", noErrExit)
}

// makeBenchFilepaths makes paths for benchmarking files in a timestamped subdirectory.
func makeBenchFilepaths(baseDir, timestamp string) {
	// Create timestamped subdirectory for this run
	runDir := filepath.Join(baseDir, timestamp)

	if err := os.MkdirAll(runDir, consts.PermsGenericDir); err != nil {
		logging.E("Failed to create benchmark run directory: %v", err)
		return
	}

	// Simple filenames (no timestamp needed since they're in timestamped folder)
	cpuProfPath = filepath.Join(runDir, "cpu.prof")
	memProfPath = filepath.Join(runDir, "mem.prof")
	traceOutPath = filepath.Join(runDir, "trace.out")

	logging.I("Created benchmark directory: %q", runDir)
}
