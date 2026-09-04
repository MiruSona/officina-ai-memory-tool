package lint

import (
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// knownFields 는 model.Encode 가 다시 쓰는 머리말 칸이다. 이 밖의 칸이 든
// 파일은 다시 쓰면 그 칸이 사라지므로 건너뛴다 (설계 9-2).
var knownFields = []string{"id", "type", "date", "summary", "tags", "source", "scope",
	"title", "pinned", "importance", "status", "severity", "invalid_at", "superseded_by",
	"links", "state", "archived",
	// v0.2 가 늘린 칸 (설계 2-2).
	"author", "sources", "stale_after", "todo_status", "migrated"}

// byteOrderMarkText 는 BOM 을 글자로 본 것이다. 편집기가 옮기지 못하게
// 리터럴로 안 적는다.
var byteOrderMarkText = string(byteOrderMark)

// applyFixes 는 하나뿐인 쓰기 락을 잡고 되돌릴 수 있는 것만 고친다. 못 잡으면
// 아무것도 안 고치고 검사만 한 것으로 둔다 (불변조건 2 · 설계 9-2).
func applyFixes(scans []*scanned, options *Options, report *Report) error {
	release, taken, err := index.TryLock(options.Store.Dir)
	if err != nil {
		return err
	}
	if !taken {
		report.FixLocked = true
		return nil
	}
	defer release()
	for _, item := range scans {
		what := fixesFor(item, vocabOf(options))
		if len(what) == 0 {
			continue
		}
		if model.StorePath(item.Memory.ID) != item.Path {
			continue
		}
		if extra := unknownFields(item.Text); len(extra) > 0 {
			report.Held = append(report.Held, Fix{Path: item.Path, What: strings.Join(extra, ", ")})
			continue
		}
		// 머리말에 사람이 적은 `#` 주석은 다시 쓰기가 지워 버린다. 되돌릴 수
		// 없는 것은 안 건드린다 (리뷰 C #27).
		if commented(item.Text) {
			report.Held = append(report.Held, Fix{Path: item.Path, What: i18n.T(i18n.LintFixComment), Note: true})
			continue
		}
		lowerTags(item.Memory)
		swapAliases(item.Memory, vocabOf(options))
		if err := options.Store.WriteMemory(item.Memory); err != nil {
			return err
		}
		report.Fixes = append(report.Fixes, Fix{Path: item.Path, What: strings.Join(what, " · ")})
	}
	return nil
}

// commented 는 머리말에 `#` 로 시작하는 줄이 있는지 본다. model.Encode 는 칸만
// 다시 쓰므로 그런 줄은 살아남지 못한다.
func commented(text string) bool {
	inside := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if trimmed == "---" {
			if inside {
				return false
			}
			inside = true
			continue
		}
		if inside && strings.HasPrefix(trimmed, "#") {
			return true
		}
	}
	return false
}

// fixesFor 는 이 파일에 필요한 되돌릴 수 있는 손질이다. 다시 쓰기 한 번이
// 인코딩·BOM·줄끝·태그를 한꺼번에 정리한다. **거절 규칙은 여기서 안 고친다** —
// 요약이 짧거나 근거가 없는 것은 사람이 써야 할 내용이다.
func fixesFor(item *scanned, vocab config.Vocab) []string {
	what := []string{}
	if item.CP949 {
		what = append(what, i18n.T(i18n.LintFixEncoding))
	}
	if item.BOM {
		what = append(what, i18n.T(i18n.LintFixBOM))
	}
	if item.CRLF {
		what = append(what, i18n.T(i18n.LintFixLineEnd))
	}
	if hasUpperTag(item.Memory.Tags) {
		what = append(what, i18n.T(i18n.LintFixTagCase))
	}
	if from, to := aliasSwaps(item.Memory.Tags, vocab); len(from) > 0 {
		what = append(what, "별칭 태그 "+strings.Join(from, "·")+" → "+strings.Join(to, "·"))
	}
	return what
}

// aliasSwaps 는 표준 목록의 별칭을 제 이름으로 바꿀 자리다 (규칙 F09).
// 되돌릴 수 있는 손질이라 --fix 가 한다 (품질규칙표 1-1 F09).
func aliasSwaps(tags []string, vocab config.Vocab) ([]string, []string) {
	from, to := []string{}, []string{}
	for _, tag := range tags {
		want, found := vocab.TagAlias[strings.ToLower(tag)]
		if !found || want == strings.ToLower(tag) {
			continue
		}
		from, to = append(from, tag), append(to, want)
	}
	return from, to
}

// swapAliases 는 별칭을 제 이름으로 갈아 끼운다. 같은 태그가 둘이 되면 하나만 둔다.
func swapAliases(memory *model.Memory, vocab config.Vocab) {
	seen, kept := map[string]bool{}, []string{}
	for _, tag := range memory.Tags {
		name := strings.ToLower(tag)
		if want, found := vocab.TagAlias[name]; found {
			name = want
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		kept = append(kept, name)
	}
	memory.Tags = kept
}

func hasUpperTag(tags []string) bool {
	for _, tag := range tags {
		if tag != strings.ToLower(tag) {
			return true
		}
	}
	return false
}

func lowerTags(memory *model.Memory) {
	for at, tag := range memory.Tags {
		memory.Tags[at] = strings.ToLower(tag)
	}
}

// unknownFields 는 규격이 모르는 머리말 칸 이름이다.
func unknownFields(text string) []string {
	head, ok := frontMatterOf(text)
	if !ok {
		return nil
	}
	extra := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(head, "\n") {
		key, ok := topLevelKey(line)
		if !ok || seen[key] || knownField(key) {
			continue
		}
		seen[key] = true
		extra = append(extra, key)
	}
	return extra
}

func frontMatterOf(text string) (string, bool) {
	body := strings.ReplaceAll(strings.TrimPrefix(text, byteOrderMarkText), "\r\n", "\n")
	if !strings.HasPrefix(body, "---\n") {
		return "", false
	}
	rest := body[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// topLevelKey 는 첫 칸에서 시작하는 `키:` 만 읽는다. 안으로 들여쓴 줄·목록
// 항목·주석은 칸이 아니다.
func topLevelKey(line string) (string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '-' {
		return "", false
	}
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return "", false
	}
	key := line[:colon]
	if strings.ContainsAny(key, " \t\"'") {
		return "", false
	}
	return key, true
}

func knownField(key string) bool {
	for _, known := range knownFields {
		if known == key {
			return true
		}
	}
	return false
}

// vocabOf 는 태그 표준 목록이다. 안 주면 기본 목록을 쓴다.
func vocabOf(options *Options) config.Vocab {
	if len(options.Vocab.Tags) == 0 {
		return config.DefaultVocab()
	}
	return options.Vocab
}
