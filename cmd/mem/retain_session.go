package main

// mem retain 의 세션 하나 처리 — 설계 1절 ④-1~④-6 (읽기 → 가리기 → 토막 → 생성 → 관문 → 표시).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/hook"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/llm"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
)

// retainPerChunk 는 토막 하나에서 받을 후보 상한이다 (설계 1절 ④-4).
const retainPerChunk = 3

// retainChildFailed 는 자식 add 가 거절도 저장도 아닌 오류로 끝난 후보를 세는 이름이다.
const retainChildFailed = "child-error"

// retainWorker 는 한 판 동안 바뀌지 않는 것을 모은다.
type retainWorker struct {
	repository *config.Repository
	generator  *llm.Generator
	dry        bool
	system     string
	scanner    *secret.Scanner
	// nliTimeoutMS 는 판정 한 번의 상한이다. 자식 add 를 기다릴 상한을 셈한다.
	nliTimeoutMS int
	vocab        config.Vocab
	scopes       []string
	tags         []string
	// root 는 저장소 뿌리다. scope 추정(guessScope)의 마지막 후보다.
	root string
}

func newRetainWorker(repository *config.Repository, generator *llm.Generator, dry bool, nliTimeoutMS int) *retainWorker {
	vocab := vocabOf(repository)
	// 가리기는 add 의 비밀 차단과 같은 규칙(builtin · guard 포함)을 쓴다.
	worker := &retainWorker{repository: repository, generator: generator, dry: dry, nliTimeoutMS: nliTimeoutMS,
		scanner: secret.ForConfig(repository.Config.Secret), vocab: vocab, scopes: vocab.StandardScopes(), tags: vocab.StandardTags()}
	worker.root = filepath.Dir(repository.Dir)
	project := worker.guessScope(worker.root)
	if project == "" {
		project = "목록에서 프로젝트 이름에 맞는 것"
	}
	worker.system = i18n.T(i18n.RetainSystemPrompt, strings.Join(worker.scopes, ", "), project, strings.Join(worker.tags, ", "))
	return worker
}

// guessScope 는 생성이 scope 를 못 맞췄을 때 쓸 scope 다. A1 훅과 같은 hook.ScopeFor(mem.toml [scope]
// 표 → 폴더 이름 소문자)로 dirs 를 차례로 보고 vocab 안인 첫 값을 준다. 못 정하면 빈 글이다 —
// 그 후보는 버린다. 틀린 scope 로 저장하는 것보다 낫다.
func (w *retainWorker) guessScope(dirs ...string) string {
	for _, dir := range dirs {
		if name, known, _ := w.vocab.NormalizeScope(hook.ScopeFor(w.repository.Config.Scope, dir)); known {
			return name
		}
	}
	return ""
}

// session 은 큐 항목 하나를 처리한다. stopped 가 참이면 서버가 끊긴 것이라 표시 없이 판을 멈춘다.
func (w *retainWorker) session(item retain.QueueItem) (retainLogLine, bool) {
	started := time.Now()
	entry := item.Entry
	short := retain.Short(entry.SessionID)
	line := newRetainLogLine(short, w.dry, started)
	utterances, err := retain.ReadUtterances(entry.TranscriptPath)
	if err != nil {
		line.Error = retain.ResultNoTranscript
		w.mark(item, retain.ResultNoTranscript, nil)
		return w.finish(line, started, retain.ResultNoTranscript), false
	}
	if info, err := os.Stat(entry.TranscriptPath); err == nil {
		line.Bytes = info.Size()
	}
	for at := range utterances {
		utterances[at].Text, _ = w.scanner.Redact(utterances[at].Text)
		line.TalkChars += utf8.RuneCountInString(utterances[at].Text)
	}
	settings := w.repository.Config.Retain
	chunks, dropped := retain.Chunks(utterances, settings.ChunkChars, settings.MaxChunks)
	line.Chunks, line.DroppedChunks = len(chunks), dropped
	caps := loadRetainCaps(w.repository, entry.SessionID)
	fallbackScope := w.guessScope(entry.Cwd, w.root)
	for at, chunk := range chunks {
		candidates, err := w.generate(chunk, fallbackScope, &line)
		if err != nil {
			line.Error = "generate: " + err.Error()
			if llm.ServerDown(err) {
				return w.finish(line, started, ""), true
			}
			// 400 류(문맥 초과 등)는 이 세션만의 문제다 — 오류로 표시하고 다음 세션으로 간다.
			fmt.Fprintln(os.Stderr, i18n.T(i18n.RetainGenerateError, short, err.Error()))
			w.mark(item, retain.ResultError, line.SavedIDs)
			return w.finish(line, started, retain.ResultError), false
		}
		for _, candidate := range candidates {
			if caps.full() {
				line.Rejected[retain.RuleCap]++
				continue
			}
			// 관문 R8 과 같은 뿌리(cmd_add_auto 의 Context.Root)로 거른다.
			sources, dropped := childSources(filepath.Dir(w.repository.Dir), short, at+1, candidate.Files)
			line.DroppedFiles += dropped
			result, err := runChild(childArgs(w.repository.Dir, short, candidate, sources, w.dry), candidate.Body,
				childTimeout(len(candidate.Quotes), w.nliTimeoutMS))
			if err != nil {
				line.Rejected[retainChildFailed]++
				line.Error = err.Error()
				continue
			}
			if down := w.note(&line, candidate, sources, result, &caps); down != "" {
				line.Error = down
				return w.finish(line, started, ""), true
			}
		}
	}
	outcome := retain.ResultRejected
	if line.Saved > 0 {
		outcome = retain.ResultSaved
	} else if line.Candidates == 0 {
		outcome = retain.ResultNothing
	}
	w.mark(item, outcome, line.SavedIDs)
	return w.finish(line, started, outcome), false
}

// generate 는 토막 하나를 생성 서버에 묻는다. JSON 이 깨지면 「JSON 하나만」 한 줄을 붙여 한 번 더
// 묻고, 또 깨지면 parse_fail 로 세고 빈 목록을 준다. 서버 오류는 err 로 돌려 판을 멈추게 한다.
func (w *retainWorker) generate(chunk, fallbackScope string, line *retainLogLine) ([]retain.GenCandidate, error) {
	user := "[대화]\n" + chunk
	for attempt := 0; attempt < 2; attempt++ {
		answer, err := w.generator.Generate(context.Background(), w.system, user)
		line.GenMS = append(line.GenMS, answer.MS)
		line.PromptTokens = append(line.PromptTokens, answer.PromptTokens)
		if err != nil && !errors.Is(err, llm.ErrNoContent) {
			return nil, err
		}
		if err == nil {
			candidates, _, dropped, parseErr := retain.ParseCandidates(answer.Content, w.vocab, fallbackScope, retainPerChunk)
			if parseErr == nil {
				line.Candidates += len(candidates)
				for why, count := range dropped {
					line.Rejected[why] += count
					line.Candidates += count
				}
				return candidates, nil
			}
		}
		user += "\n\n" + i18n.T(i18n.RetainJSONOnly)
	}
	line.ParseFail++
	return nil, nil
}

// note 는 자식 결과 하나를 기록에 더한다. R3 가 「서버 끊김」으로 거절했으면 그 까닭을 돌려준다.
func (w *retainWorker) note(line *retainLogLine, candidate retain.GenCandidate, sources []string, result childResult, caps *retainCaps) string {
	verdict := retainVerdictPass
	if result.Passed {
		caps.count()
		if !w.dry {
			line.Saved++
			line.SavedIDs = append(line.SavedIDs, result.ID)
		}
	} else {
		rules := []string{}
		for _, one := range result.Rejects {
			line.Rejected[one.Rule]++
			rules = append(rules, one.Rule)
		}
		verdict = strings.Join(rules, ",")
	}
	if w.dry {
		reasons := []string{}
		for _, one := range result.Rejects {
			reasons = append(reasons, one.Rule+": "+one.Reason)
		}
		line.Items = append(line.Items, retainLogItem{Title: candidate.Title, Type: candidate.Type,
			Scope: candidate.Scope, ScopeFrom: candidate.ScopeFrom,
			Summary: candidate.Summary, Quotes: candidate.Quotes, Sources: sources, Verdict: verdict, Reasons: reasons})
	}
	for _, one := range result.Rejects {
		if one.Rule == retain.RuleSupport && retain.SupportDown(one.Reason) {
			return one.Reason
		}
	}
	return ""
}

// mark 는 큐 항목에 처리 표시를 단다. dry-run 은 안 단다.
func (w *retainWorker) mark(item retain.QueueItem, outcome string, saved []string) {
	if w.dry {
		return
	}
	// 처리 도중 훅이 표를 새로 썼으면 표시하지 않는다 — 다음 판에 다시 돈다.
	if _, err := retain.MarkDone(item.Path, item.Entry.At, outcome, saved, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.RetainMarkFailed, err.Error()))
	}
}

// finish 는 걸린 시간을 적고 세션 한 줄을 stderr 에 찍는다 (--json 의 stdout 을 안 더럽힌다).
func (w *retainWorker) finish(line retainLogLine, started time.Time, outcome string) retainLogLine {
	line.TotalMS = time.Since(started).Milliseconds()
	rejected := 0
	for _, count := range line.Rejected {
		rejected += count
	}
	fmt.Fprintln(os.Stderr, i18n.T(i18n.RetainSessionLine, line.Session, line.Chunks, line.DroppedChunks,
		line.Candidates, line.Saved, rejected, outcome))
	return line
}
