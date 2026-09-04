// perf 는 색인 성능을 재는 임시 잣대다. 저장소 하나를 통째로 다시 색인하고
// 걸린 시간·DB 크기를 찍는다. 쓰는 법 : go run ./testdata/perf -repo <Memory> -cpu out.prof
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/lint"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// options 는 명령줄에서 받은 것이다.
type options struct {
	repo string
	cpu  string
	full bool
	lint bool
}

func main() {
	settings := options{}
	flag.StringVar(&settings.repo, "repo", "", "Memory 저장소 뿌리")
	flag.StringVar(&settings.cpu, "cpu", "", "CPU 프로파일 파일")
	flag.BoolVar(&settings.full, "full", true, "통째로 다시 색인")
	flag.BoolVar(&settings.lint, "lint", false, "색인 대신 lint 를 잰다")
	flag.Parse()
	if settings.repo == "" {
		fmt.Fprintln(os.Stderr, "-repo 가 필요하다")
		os.Exit(2)
	}
	if settings.cpu != "" {
		file, err := os.Create(settings.cpu)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		pprof.StartCPUProfile(file)
		defer func() {
			pprof.StopCPUProfile()
			file.Close()
		}()
	}
	defaults := config.Default("perf")
	started := time.Now()
	if settings.lint {
		report, err := lint.Run(lint.Options{Store: store.Open(settings.repo, false), Config: defaults})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("checked=%d problems=%d elapsed=%.2fs\n", report.Checked, len(report.Problems),
			time.Since(started).Seconds())
		return
	}
	result, err := index.Run(index.Options{Store: store.Open(settings.repo, false),
		GC: defaults.GC, Secret: defaults.Secret, Quiet: true, Full: settings.full})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	elapsed := time.Since(started)
	size := int64(0)
	if info, err := os.Stat(index.DBPath(settings.repo)); err == nil {
		size = info.Size()
	}
	fmt.Printf("indexed=%d skipped=%d total=%d elapsed=%.2fs db=%.1fMB\n",
		result.Indexed, result.Skipped, result.Total, elapsed.Seconds(), float64(size)/1e6)
}
