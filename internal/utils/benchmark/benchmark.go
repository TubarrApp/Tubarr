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

type BenchFiles struct {
	cpuFile   *os.File
	memFile   *os.File
	traceFile *os.File
}

const (
	cpuProf  = "cpu_"
	memProf  = "mem_"
	traceOut = "trace_"
	profExt  = ".prof"
	outExt   = ".out"
)

var (
	mainWd,
	cpuProfPath,
	memProfPath,
	traceOutPath string
)

func InjectMainWorkDir(mainGoPath string) {
	mainWd = filepath.Dir(mainGoPath)
}

// SetupBenchmarking sets up and initiates benchmarking for a program run.
func SetupBenchmarking() (*BenchFiles, error) {
	var err error
	b := new(BenchFiles)

	benchStartTime := time.Now().Format("2006-01-02_15:04:05")

	makeFilepaths(benchStartTime, mainWd)

	logging.I("(Benchmarking this run. Start time: %s)", benchStartTime)

	// CPU profile
	b.cpuFile, err = os.Create(cpuProfPath)
	if err != nil {
		CloseBenchFiles(b, "", fmt.Errorf("could not create CPU profiling file: %v", err))
		return nil, err
	}

	if err := pprof.StartCPUProfile(b.cpuFile); err != nil {
		CloseBenchFiles(b, "", fmt.Errorf("could not start CPU profiling: %v", err))
		return nil, err
	}

	// Memory profile
	b.memFile, err = os.Create(memProfPath)
	if err != nil {
		CloseBenchFiles(b, "", fmt.Errorf("could not create memory profiling file: %v", err))
		return nil, err
	}

	// Trace
	b.traceFile, err = os.Create(traceOutPath)
	if err != nil {
		CloseBenchFiles(b, "", fmt.Errorf("could not create trace file: %v", err))
		return nil, err
	}
	if err := trace.Start(b.traceFile); err != nil {
		CloseBenchFiles(b, "", fmt.Errorf("could not start trace: %v", err))
		return nil, err
	}

	return b, nil
}

// CloseBenchFiles closes bench files on program termination.
func CloseBenchFiles(b *BenchFiles, noErrExit string, setupErr error) {

	if b.cpuFile != nil {
		pprof.StopCPUProfile()
		if err := b.cpuFile.Close(); err != nil {
			logging.E(0, "Failed to close file %q: %v", b.cpuFile.Name(), err)
		}
	}

	if b.traceFile != nil {
		if err := b.traceFile.Close(); err != nil {
			logging.E(0, "Failed to close file %q: %v", b.traceFile.Name(), err)
		}
	}

	if b.memFile != nil {
		runtime.GC()
		if err := pprof.WriteHeapProfile(b.memFile); err != nil {
			logging.E(0, "Could not write memory profile: %v", err)
		}
		if err := b.memFile.Close(); err != nil {
			logging.E(0, "Failed to close file %q: %v", b.memFile.Name(), err)
		}
	}

	if setupErr != nil {
		logging.E(0, "Benchmarking failure: %v", setupErr)
	}

	logging.I("%s", noErrExit)
}

func makeFilepaths(time, cwd string) {
	var b strings.Builder

	b.Grow(len(cpuProf) + len(time) + len(profExt))
	b.WriteString(cpuProf)
	b.WriteString(time)
	b.WriteString(profExt)

	// Benchmarking files
	cpuProfPath = filepath.Join(cwd, b.String())
	logging.I("Made CPU profiling file: %q", cpuProfPath)
	b.Reset()

	b.Grow(len(memProf) + len(time) + len(profExt))
	b.WriteString(memProf)
	b.WriteString(time)
	b.WriteString(profExt)

	memProfPath = filepath.Join(cwd, b.String())
	logging.I("Made mem profiling file: %q", memProfPath)
	b.Reset()

	b.Grow(len(traceOut) + len(time) + len(outExt))
	b.WriteString(traceOut)
	b.WriteString(time)
	b.WriteString(outExt)

	traceOutPath = filepath.Join(cwd, b.String())
	logging.I("Made trace file: %q", traceOutPath)
}
