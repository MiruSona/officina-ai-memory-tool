// mem 은 AI 기억 저장소의 명령줄 도구다. 이 파일은 인자를 갈라 등록된
// 하위명령을 부르는 일만 한다 (설계 12-1).
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 종료 코드 (설계 6-1). 0~3 은 v0.1 과 뜻이 같고 4~6 만 늘렸다 — 스크립트와
// 훅이 3 을 「저장소 없음」 으로 알고 있어서 그 자리를 안 옮긴다.
const (
	exitOK       = 0
	exitUsage    = 1
	exitCheck    = 2
	exitNoStore  = 3
	exitSecurity = 4
	exitBroken   = 5
	exitLocked   = 6
)

// command 는 하위명령 하나다. bools 는 값을 안 받는 옵션, values 는 값을 받는
// 옵션 이름이다. 둘 다 적어야 모르는 옵션을 알아볼 수 있다.
type command struct {
	name   string
	run    func([]string) int
	bools  []string
	values []string
}

// registry 는 붙어 있는 하위명령이다. 각 명령이 자기 파일의 init 에서 붙인다.
var registry = map[string]command{}

func register(item command) {
	registry[item.name] = item
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	if len(argv) == 0 {
		fmt.Println(i18n.T(i18n.Usage))
		return exitOK
	}
	name, rest := argv[0], argv[1:]
	if name == "help" || name == "--help" || name == "-h" {
		return showHelp(rest)
	}
	if item, found := registry[name]; found {
		// 훅만 예외다. 도움말을 stdout 에 찍으면 그 한글이 그대로 세션
		// 컨텍스트로 들어간다 (설계 7-3 · 리뷰 A #6).
		if name != "hook" && wantsHelp(rest, item.bools) {
			return showHelp([]string{name})
		}
		return item.run(rest)
	}
	return fail(i18n.T(i18n.UnknownCommand, name))
}

// showHelp 는 이름이 없으면 명령 목록을, 있으면 그 명령 하나의 도움말을 찍는다.
func showHelp(argv []string) int {
	name := ""
	for _, word := range argv {
		if !strings.HasPrefix(word, "-") {
			if name == "" {
				name = word
			}
			continue
		}
		// help 는 옵션을 안 받는다. 모르는 것을 조용히 넘기면 오타가 「도움말이
		// 잘 나왔다」로 읽힌다 (보안연동 시험 L-1).
		if word != "-h" && word != "--help" {
			return fail(i18n.T(i18n.UnknownOption, word))
		}
	}
	if name == "" {
		fmt.Println(i18n.T(i18n.Usage))
		return exitOK
	}
	text, found := i18n.CommandHelp(name)
	if !found {
		return fail(i18n.T(i18n.UnknownCommand, name))
	}
	fmt.Println(text)
	return exitOK
}

// wantsHelp 는 `--help` 가 옵션 값이 아니라 진짜 도움말 요청인지 본다. 명령이
// 알려준 bools 로 같이 갈라야 `--summary --help` 같은 것을 안 헷갈린다.
func wantsHelp(argv []string, bools []string) bool {
	known := make([]string, 0, len(bools)+2)
	known = append(known, bools...)
	known = append(known, "help")
	parsed, err := parseOptions(argv, known, anyValue)
	if err != nil {
		return false
	}
	return parsed.flags["help"] || contains(parsed.rest, "-h")
}

func fail(message string) int {
	fmt.Fprintln(os.Stderr, message)
	return exitUsage
}

// options 는 `--이름 값` 과 그 밖의 낱말을 갈라 담는다.
type options struct {
	values map[string]string
	flags  map[string]bool
	rest   []string
}

// anyValue 는 "값 옵션을 안 가린다" 는 뜻이다. 도움말 판정처럼 인자를 대충
// 훑기만 하는 자리에서 쓴다.
var anyValue = []string{"*"}

// parseOptions 는 명령이 알려준 것만 옵션으로 본다. 모르는 `--이름` 은 다음
// 인자를 값으로 삼키지 않고 바로 실패한다 — 조용히 먹으면 사람이 진짜 원인을
// 못 찾고 오래 헤맨다 (실데이터 시험 7절 #8).
func parseOptions(argv []string, bools []string, values []string) (*options, error) {
	parsed := options{values: map[string]string{}, flags: map[string]bool{}}
	for at := 0; at < len(argv); at++ {
		word := argv[at]
		if !strings.HasPrefix(word, "--") {
			parsed.rest = append(parsed.rest, word)
			continue
		}
		name := strings.TrimPrefix(word, "--")
		if key, value, found := strings.Cut(name, "="); found {
			if contains(bools, key) {
				// --pin=true 는 스크립트에서 흔한 꼴이다 (리뷰 C #17).
				flag, err := boolValue(value)
				if err != nil {
					return nil, err
				}
				parsed.flags[key] = flag
				continue
			}
			if !takesValue(values, key) {
				return nil, errors.New(i18n.T(i18n.UnknownOption, "--"+key))
			}
			parsed.values[key] = value
			continue
		}
		if contains(bools, name) {
			parsed.flags[name] = true
			continue
		}
		if !takesValue(values, name) {
			return nil, errors.New(i18n.T(i18n.UnknownOption, word))
		}
		if at+1 >= len(argv) {
			return nil, errors.New(i18n.T(i18n.NeedArgument, word))
		}
		at++
		parsed.values[name] = argv[at]
	}
	return &parsed, nil
}

// boolValue 는 `--pin=true` 의 오른쪽을 읽는다. 모르는 값은 조용히 참으로
// 보지 않는다 — 오타가 켜짐으로 읽히면 사람이 못 찾는다.
func boolValue(text string) (bool, error) {
	switch strings.ToLower(text) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, errors.New(i18n.T(i18n.BadBoolValue, text))
}

// takesValue 는 그 이름이 값을 받는 옵션인지 본다. `*` 는 다 받는다는 뜻이다.
func takesValue(values []string, name string) bool {
	return contains(values, "*") || contains(values, name)
}

func (o *options) text(name string) string { return o.values[name] }

func (o *options) has(name string) bool {
	_, found := o.values[name]
	return found
}

func (o *options) list(name string) []string {
	value := strings.TrimSpace(o.values[name])
	if value == "" {
		return nil
	}
	out := []string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// openStore 는 쓰기용 저장소를 연다.
func openStore(parsed *options) (*config.Repository, *store.Store, error) {
	working, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	project, err := config.Resolve(parsed.text("repo"), working)
	if err != nil {
		return nil, nil, err
	}
	if project == nil {
		return nil, nil, &config.NoRepositoryError{}
	}
	wireNormalize(project)
	return project, store.New(project), nil
}

// wireNormalize 는 index.WireNormalize 로 넘긴다. 훅도 같은 함수를 불러야
// 훅이 만든 행과 `mem index` 가 만든 행이 같아진다 (결정 21·57).
func wireNormalize(project *config.Repository) {
	index.WireNormalize(project)
	// mem.toml `[embed] model` 도 여기서 알린다. 저장소를 여는 길이 둘
	// (openStore·openSources)이라 한 자리에 모아야 값이 안 갈린다.
	wireEmbedModel(project)
}

// exitFor 는 오류에 맞는 종료 코드를 고른다. 저장소가 없으면 3, 색인이 깨졌거나
// exe 가 낡았으면 2 다 (설계 8-1).
func exitFor(err error) int {
	missing := &config.NoRepositoryError{}
	if errors.As(err, &missing) {
		fmt.Fprintln(os.Stderr, err.Error())
		return exitNoStore
	}
	// 색인이 깨진 것은 저장소가 없는 것과 뜻이 정반대다 (설계 6-1 종료 5).
	if index.IsUnusable(err) {
		fmt.Fprintln(os.Stderr, err.Error())
		return exitBroken
	}
	if index.IsCheckFailure(err) {
		fmt.Fprintln(os.Stderr, err.Error())
		return exitCheck
	}
	// 파일·권한 문제는 쓰는 법이 틀린 것이 아니다. 숫자를 갈라야 스크립트가
	// 「다시 해 보면 되는 것」 과 「고쳐야 하는 것」 을 구분한다 (설계 6-1).
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrInvalid) {
		fmt.Fprintln(os.Stderr, err.Error())
		return exitBroken
	}
	return fail(err.Error())
}
