package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/eval"
)

var evalBools = []string{"json", "hard-only", "no-index", "quality", "tune-dup", "search"}
var evalValues = []string{"golden", "repo"}

func init() {
	register(command{name: "eval", run: runEval, bools: evalBools, values: evalValues})
}

// runEval 은 골든셋으로 검색 품질을 잰다. 합격선에 못 미치면 종료 코드 2 다.
func runEval(argv []string) int {
	parsed, err := parseOptions(argv, evalBools, evalValues)
	if err != nil {
		return fail(err.Error())
	}
	if parsed.flags["quality"] || parsed.flags["tune-dup"] {
		return runEvalQuality(parsed)
	}
	repos, err := openSources(parsed)
	if err != nil {
		return exitFor(err)
	}
	defer repos.close()
	report, err := eval.Run(eval.Options{
		Sources: repos.sources, Golden: goldenPath(parsed, repos),
		Stopwords: repos.settings.Stopword.Words, Synonym: repos.settings.Synonym,
		HardOnly: parsed.flags["hard-only"],
		RRFK:     repos.settings.Search.RRFK, BonusCap: repos.settings.Search.BonusCap,
		// mem.toml [search] 를 통째로 넘긴다. `mem search` 와 같은 값이라야
		// 자가 실제 검색을 잰다 (갈래 A 가 넘긴 결함).
		Search: repos.settings.Search,
		Embed:  repos.settings.Embed, RepoDir: repos.dirOf(),
		Vectors: evalVectors(repos),
	})
	if err != nil {
		return evalExit(err)
	}
	if parsed.flags["json"] {
		printJSON(report)
	}
	if !parsed.flags["json"] {
		fmt.Print(eval.Markdown(report))
	}
	if report.Pass {
		return exitOK
	}
	return exitCheck
}

// goldenPath 는 --golden 이 있으면 그것, 없으면 저장소의 golden/ 이다.
func goldenPath(parsed *options, repos *opened) string {
	if given := parsed.text("golden"); given != "" {
		return given
	}
	return eval.Path(repos.dirOf())
}

func evalExit(err error) int {
	fmt.Fprintln(os.Stderr, err.Error())
	failure := &eval.Error{}
	if errors.As(err, &failure) {
		// 골든셋이 없는 것은 「미달」 이 아니라 「잴 것이 없다」 다 (리뷰C #13).
		return failure.ExitCode()
	}
	return exitUsage
}

// runEvalQuality 는 규칙 42개가 실제로 듣는지 잰다 (설계 3-6). --tune-dup 은
// 중복 문턱 곡선이다. 둘 다 파도 G 의 internal/eval 이 셈을 한다.
func runEvalQuality(parsed *options) int {
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	// 기억은 `Memory/` 바로 아래가 아니라 `store/` 아래에 있다. Memory/ 를 주면
	// log.md·사용법.md 를 기억으로 읽다가 죽는다 (리뷰 B).
	options := eval.QualityOptions{Golden: parsed.text("golden"), Store: opened.StoreDir(),
		Config: repository.Config, Vocab: vocabOf(repository), Now: time.Now(),
		Near: nearOf(vectorsFor(opened.Dir))}
	if options.Golden == "" {
		options.Golden = eval.QualityPath(opened.Dir)
	}
	if parsed.flags["tune-dup"] {
		report, err := eval.RunTune(options)
		if err != nil {
			return evalExit(err)
		}
		if parsed.flags["json"] {
			printJSON(report)
			return exitOK
		}
		fmt.Print(eval.TuneMarkdown(report))
		return exitOK
	}
	report, err := eval.RunQuality(options)
	if err != nil {
		return evalExit(err)
	}
	if parsed.flags["json"] {
		printJSON(report)
	} else {
		fmt.Print(eval.QualityMarkdown(report))
	}
	if report.Pass {
		return exitOK
	}
	return exitCheck
}
