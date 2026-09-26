package index

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

const (
	mergeWindowDays = 7
	mergeThreshold  = 0.8
	// mergedMaxLines 는 붙인 뒤 본문이 넘으면 안 되는 줄 수다 (설계 4-3).
	// 한 파일 한 주제를 지키려고 여기서 끊고 새 파일로 간다.
	mergedMaxLines = 120
)

// promoteInbox 는 inbox/new 를 훑어 큐 파일 하나하나를 md 로 만든다. 하나가
// 깨져도 나머지는 계속 간다 (설계 5-2).
func (r *runner) promoteInbox() error {
	names, err := r.store.ListInbox()
	if err != nil {
		return err
	}
	return r.promoteNames(names)
}

// promoteNames 는 이름 준 큐 파일만 차례대로 승격한다. 색인(promoteInbox)은
// inbox/new 전부를, add 즉시 승격(PromoteNow)은 제가 쓴 파일만 넘긴다 — 같은
// 코드·같은 락이다 (설계 2026-09-23 2-4).
func (r *runner) promoteNames(names []string) error {
	// 영수증 표는 승격하는 모든 길(색인 · 훅 · add)이 여기를 지나니 여기서 세운다.
	if err := EnsurePromoted(r.db.sql); err != nil {
		return err
	}
	if err := r.db.prunePromoted(r.now); err != nil {
		return err
	}
	archived := []string{}
	for _, name := range names {
		// 큐가 밀려 있으면 여기서 시간을 다 먹는다. 훅은 마감을 주고 부른다.
		if r.overBudget() {
			// 여기서 바로 돌아가면 이미 치운 이름의 inbox_seen 이 30일 동안
			// 남아 index.db 를 불린다 (리뷰 A7). 아래 지우기까지 하고 끝낸다.
			break
		}
		seen, err := r.db.seenInbox(name)
		if err != nil {
			return err
		}
		if !seen && !r.promoteOne(name) {
			continue
		}
		// 순서 6 : 접수 원본을 한 줄로 붙이고 new 에서 지운다.
		if err := r.store.Archive(name); err != nil {
			r.result.Left++
			r.note(name, err)
			continue
		}
		archived = append(archived, name)
	}
	// 큐 파일이 없어졌으면 「이미 승격했다」 표시도 쓸모가 없다. 두 번 승격을
	// 막는 것은 파일이 아직 new 에 있을 때 뿐이다 (리뷰 A #11). 남겨 두면
	// migrate 를 한 20k 저장소에서 18,804줄이 30일 동안 index.db 에 앉아
	// 1.8MB 를 먹고 자(60MB)를 넘긴다 (v0.4 리뷰 A R2).
	if err := r.db.forgetInbox(archived); err != nil {
		r.note(DBPath(r.store.Dir), err)
	}
	return nil
}

// 이번 회차에 만든 것. 승격이 색인보다 먼저 돌기 때문에 DB 만 봐서는 같은
// 회차에 들어온 두 건이 서로를 못 본다 (리뷰 C #2).
type madeHere struct {
	candidate
	Type     string
	TagField string
	BodyKey  string
	// Held 는 이 항목이 보류(`review: true`)로 만들어졌는지다. 보류 기억은
	// 뒤에 오는 어떤 요청의 합치기 상대도 될 수 없다 (뒷정리-3).
	Held bool
}

// bodyKey 는 완전중복 판정의 열쇠다. 같은 본문이라도 type·scope 가 다르면
// 다른 기억이다 (설계 5-2 순서 1).
func bodyKey(body, memoryType, scope string) string {
	return bodyHash(body) + "\x00" + memoryType + "\x00" + scope
}

// remember 는 방금 만든 기억을 이번 회차 목록에 넣는다.
func (r *runner) remember(id, path string, request *store.AddRequest) {
	r.made = append(r.made, madeHere{
		candidate: candidate{ID: id, Path: path, Title: titleOf(request)},
		Type:      request.Type, TagField: TagField(request.Tags),
		BodyKey: bodyKey(request.Body, request.Type, request.Scope), Held: request.Review})
}

// sameBodyHere 는 이번 회차에 이미 들어온 같은 본문의 id 다.
func (r *runner) sameBodyHere(request *store.AddRequest) string {
	key := bodyKey(request.Body, request.Type, request.Scope)
	for _, item := range r.made {
		if item.BodyKey == key {
			return item.ID
		}
	}
	return ""
}

// retryError 는 요청이 아니라 파일 계층이 실패했다는 표시다. 그런 항목은
// inbox/new 에 그대로 남고 다음 index 가 다시 해 본다 (설계 5-2 순서 5).
type retryError struct {
	err error
}

func (e *retryError) Error() string { return e.err.Error() }
func (e *retryError) Unwrap() error { return e.err }

func retryable(err error) error {
	if err == nil {
		return nil
	}
	return &retryError{err: err}
}

func isRetryable(err error) bool {
	target := &retryError{}
	if errors.As(err, &target) {
		return true
	}
	// 큐 파일 자체를 못 읽었다면 요청을 아예 읽지도 못한 것이다.
	pathError := &fs.PathError{}
	return errors.As(err, &pathError)
}

// 큐 파일 하나가 승격에서 어떻게 끝났는지 (설계 2026-09-23 2-3).
const (
	// OutcomeNew 는 새 md 파일을 만들었다는 뜻이다.
	OutcomeNew = "new"
	// OutcomeDuplicate 는 같은 본문이 이미 있어 그 기억을 가리킨다는 뜻이다.
	OutcomeDuplicate = "duplicate"
	// OutcomeAppended 는 닮은 기억 뒤에 한 절로 붙었다는 뜻이다.
	OutcomeAppended = "appended"
	// OutcomeDone 은 add 가 아닌 항목(patch · body)이 먹혔다는 뜻이다.
	OutcomeDone = "done"
	// OutcomeBad 는 inbox/bad 로 갔다는 뜻이다 (비밀정보 · 규격 위반 · 대상 없음).
	OutcomeBad = "bad"
	// OutcomeLeft 는 파일 계층이 실패해 inbox/new 에 그대로 남았다는 뜻이다.
	OutcomeLeft = "left"
	// OutcomeGone 은 락을 잡고 보니 남이 먼저 승격해 큐 파일이 없고, 영수증도
	// 없어 실제 id 를 모른다는 뜻이다 (색인을 새로 세웠거나 오래 기다렸다).
	OutcomeGone = "gone"
)

// Outcome 은 이름 준 큐 파일 하나의 결과다. ID 는 **실제로 남은** 기억 id 다 —
// 중복이면 쌍둥이, 붙었으면 닮은 기억이다. Reason 은 bad·left 일 때의 까닭으로
// 이미 중화한 한 줄이다 (규칙 이름·칸 이름뿐, 값은 안 실린다).
type Outcome struct {
	State  string
	ID     string
	Reason string
	Secret bool
	// Foreign 은 이 항목을 남(`mem index` · 다른 add)이 먼저 승격했다는 뜻이다.
	// ID 는 그 회차가 남긴 영수증에서 읽은 실제 id 다.
	Foreign bool
}

// record 는 결과를 적는다. id 가 있는 결과는 누가 승격하든 영수증(promoted
// 표)에 남긴다 — 락을 기다리던 add 가 남이 먹은 제 파일의 실제 id 를 거기서
// 읽는다 (리뷰 2026-09-26 #1). 이름별 결과 표는 add 즉시 승격만 들고 있다.
func (r *runner) record(name, state, id string) {
	if id != "" {
		if err := r.db.keepPromoted(name, r.queued, id, state, r.now); err != nil {
			r.note(name, err)
		}
	}
	if r.outcome == nil {
		return
	}
	if _, done := r.outcome[name]; done {
		// 한 항목 안에서 먼저 정해진 결과가 맞다. appendTo 가 본문 넘침으로
		// createNew 로 넘어가는 길처럼 안쪽 함수가 이미 적었다.
		return
	}
	r.outcome[name] = Outcome{State: state, ID: id}
}

// recordFailure 는 bad·left 결과를 까닭과 같이 적는다. 앞서 적힌 성공 결과가
// 있어도 덮는다 — 파일을 쓴 뒤 표시 단계에서 실패하면 그 항목은 끝나지 않았다.
func (r *runner) recordFailure(name, state string, reason error) {
	if err := r.db.dropPromoted(name); err != nil {
		r.note(name, err)
	}
	if r.outcome == nil {
		return
	}
	r.outcome[name] = Outcome{State: state, Reason: safe.Neutralize(safe.OneLine(reason.Error())),
		Secret: IsSecretBlock(reason)}
}

// promoteOne 은 이 항목이 끝났는지 알려준다. 나쁜 항목은 bad 로 옮기고, 파일
// 계층이 실패한 것은 그 자리에 둔다.
func (r *runner) promoteOne(name string) bool {
	item, err := r.store.ReadInbox(name)
	r.queued = ""
	if err == nil && item.Add != nil {
		r.queued = r.queueID(name, item.Add)
	}
	if err == nil {
		err = r.apply(name, item)
	}
	if err != nil && isRetryable(err) {
		r.result.Left++
		r.note(name, err)
		r.recordFailure(name, OutcomeLeft, err)
		return false
	}
	if err != nil {
		// 순서 0·4 : 비밀정보거나 규격을 어긴 것은 bad 로. 조용히 안 지운다.
		r.result.Bad++
		if IsSecretBlock(err) {
			r.result.Secret++
		}
		r.note(name, err)
		r.recordFailure(name, OutcomeBad, err)
		r.forgetAdd(name, item)
		if moveErr := r.store.MoveToBad(name); moveErr != nil {
			// 못 옮기면 파일이 new 에 그대로 남아 매 회차 다시 bad 로 세어진다.
			// 아무도 그걸 모르면 안 된다 (리뷰 A #10).
			r.result.Left++
			r.note(name, errors.New(i18n.T(i18n.IndexBadKept, moveErr.Error())))
			return false
		}
		// 이유를 옆에 남긴다 — 안 그러면 `{"op":"patch",...}` 만 보고는 왜
		// 실패했는지 명령을 다시 돌려야 안다. 오류 글은 규칙 이름·칸 이름뿐이라
		// 비밀정보 값이 실릴 자리가 없지만, 그래도 한 번 더 중화한다.
		reason := safe.Neutralize(safe.OneLine(err.Error()))
		if reasonErr := r.store.WriteBadReason(name, reason); reasonErr != nil {
			r.note(name, reasonErr)
		}
		return false
	}
	if item.Add == nil {
		r.record(name, OutcomeDone, "")
	}
	if err := r.db.markInbox(name); err != nil {
		r.result.Left++
		return false
	}
	return true
}

// forgetAdd 는 bad 로 간 add 의 큐 id 를 적어 둔다. 같은 회차에 뒤따르는 덮임
// 표시(`add --by` 가 같이 넣은 patch)가 없는 id 를 옛 기억에 달지 않게 막는다.
func (r *runner) forgetAdd(name string, item *store.InboxItem) {
	if item == nil || item.Add == nil {
		return
	}
	if r.dropped == nil {
		r.dropped = map[string]bool{}
	}
	r.dropped[r.queueID(name, item.Add)] = true
}

// queueID 는 add 가 큐에 넣을 때 찍은 id 다. 날짜를 안 적은 옛 큐 파일은
// 오늘로 친다 — createNew 와 같은 셈이다.
func (r *runner) queueID(name string, request *store.AddRequest) string {
	date := request.Date
	if date == "" {
		date = r.today()
	}
	return model.QueueID(name, request.Body, date)
}

func (r *runner) apply(name string, item *store.InboxItem) error {
	if item.Add != nil {
		return r.applyAdd(name, item.Add)
	}
	if item.Patch != nil {
		return r.applyPatch(item.Patch)
	}
	if item.Amend != nil {
		return r.applyBody(item.Amend)
	}
	return errors.New(i18n.T(i18n.UnknownOp, item.Op))
}

// applyAdd 는 설계 5-2 의 순서 0~3 이다 — 비밀정보, 완전중복, 근사중복, 새 파일.
func (r *runner) applyAdd(name string, request *store.AddRequest) error {
	if err := r.scanSecret(request); err != nil {
		return err
	}
	if request.Review {
		// 보류(`add --hold`)는 사람이 보기 전엔 안 뜬다는 뜻이다. 닮은 기존
		// 기억에 합쳐지면 검토 없이 그 기억을 통해 뜨는 구멍이 생기니, 완전
		// 중복·근사중복 둘 다 안 보고 늘 새 파일로 둔다 (뒷정리-3).
		return r.createNew(name, request)
	}
	twinID, err := r.db.sameBody(bodyHash(request.Body), request.Type, request.Scope)
	if err != nil {
		return err
	}
	if twinID == "" {
		twinID = r.sameBodyHere(request)
	}
	if twinID != "" {
		if request.Supersedes != "" {
			// 같은 `add --by X` 를 두 번 친 경우다. 두 벌은 안 만들되, add 가
			// 큐에 같이 넣은 덮임 표시(X → 이번 id)는 없는 id 를 가리키게 된다.
			// 살아남은 쌍둥이로 다시 댄다 (리뷰 2026-09-23).
			r.redirectSupersede(name, request, twinID)
		}
		// 이미 있는 기억이니 이번 add 는 그 기억을 한 번 읽은 셈으로 친다.
		store.AppendHit(r.store.Dir, "add", twinID)
		r.result.Duplicated++
		r.record(name, OutcomeDuplicate, twinID)
		return nil
	}
	if request.Supersedes != "" {
		// 덮는 기억(`add --by <옛id>`)은 옛 기억과 제목이 닮은 것이 당연하다.
		// 합치면 add 가 알려 준 새 id 가 사라지고, 옛 기억의 superseded_by 는
		// 없는 id 를 가리킨다 (사용 피드백 2026-09-20). 합침은 건너뛰고 늘 새
		// 파일로 둔다. 글자까지 같은 완전중복만 위에서 걸렀다.
		return r.createNew(name, request)
	}
	twin, err := r.findTwin(request)
	if err != nil {
		return err
	}
	if twin != nil {
		return r.appendTo(name, twin, request)
	}
	return r.createNew(name, request)
}

// redirectSupersede 는 완전중복이라 안 만든 덮는 기억의 덮임 표시를 쌍둥이로
// 옮긴다. 옛 기억에 바로 고치기를 걸고, 같은 회차에 뒤따를 add 의 patch 도
// 표(redirect)로 다시 대게 한다. 고치기가 실패해도 add 는 그대로 중복 처리다 —
// 옛 기억이 없으면 덮을 것도 없다.
func (r *runner) redirectSupersede(name string, request *store.AddRequest, twinID string) {
	if r.redirect == nil {
		r.redirect = map[string]string{}
	}
	r.redirect[r.queueID(name, request)] = twinID
	if twinID == request.Supersedes {
		// 옛 기억과 글자까지 같은 본문으로 「덮었다」. 덮을 것이 없다 —
		// 뒤따를 patch 는 retarget 이 떼어 낸다.
		return
	}
	patch := &store.PatchRequest{Op: store.OpPatch, ID: request.Supersedes,
		Set: map[string]any{"superseded_by": twinID, "invalid_at": r.today()}}
	if err := r.applyPatch(patch); err != nil {
		r.note(name, err)
	}
}

// retarget 은 덮임 표시가 가리키는 id 를 redirect 표로 바꾼다. 바꾼 결과가
// 제 자신을 덮게 되면(쌍둥이가 옛 기억 자체였다) 덮을 것이 없으니 그 두 칸을
// 떼고, 남는 칸이 없으면 true(할 일 없음)를 준다.
func (r *runner) retarget(request *store.PatchRequest) bool {
	value, ok := request.Set["superseded_by"].(string)
	if !ok {
		return false
	}
	target, moved := r.redirect[value]
	if !moved {
		return false
	}
	if target != request.ID {
		request.Set["superseded_by"] = target
		return false
	}
	delete(request.Set, "superseded_by")
	delete(request.Set, "invalid_at")
	return len(request.Set) == 0
}

func (r *runner) findTwin(request *store.AddRequest) (*candidate, error) {
	since := r.now.AddDate(0, 0, -mergeWindowDays).Unix()
	candidates, err := r.db.mergeCandidates(request.Type, request.Tags, since)
	if err != nil {
		return nil, err
	}
	tagField := TagField(request.Tags)
	for _, item := range r.made {
		// 보류로 만든 것은 합치기 상대가 될 수 없다 — 남이 거기 붙으면 그
		// 남의 글도 같이 안 뜬다 (뒷정리-3).
		if item.Type == request.Type && item.TagField == tagField && !item.Held {
			candidates = append(candidates, item.candidate)
		}
	}
	title := titleOf(request)
	for _, item := range candidates {
		if jaccard(item.Title, title) >= mergeThreshold {
			found := item
			return &found, nil
		}
	}
	return nil, nil
}

// scanSecret 은 큐의 글을 쓰기 전에 훑는다. 맞은 값은 절대 메시지에 안 싣는다.
func (r *runner) scanSecret(request *store.AddRequest) error {
	if found := r.scanner.ScanText(request.Body); found != nil {
		return blocked(errors.New(i18n.T(i18n.SecretFound, found.Line, found.Rule)))
	}
	if found := r.scanner.ScanLine(request.Summary); found != nil {
		return blocked(errors.New(i18n.T(i18n.SecretFoundIn, "summary", found.Rule)))
	}
	if found := r.scanner.ScanLine(request.Title); found != nil {
		return blocked(errors.New(i18n.T(i18n.SecretFoundIn, "title", found.Rule)))
	}
	for _, source := range request.Sources {
		if found := r.scanner.ScanLine(source); found != nil {
			return blocked(errors.New(i18n.T(i18n.SecretFoundIn, "sources", found.Rule)))
		}
	}
	return nil
}

// scanPatch 는 고치기가 실어 온 글을 **칸을 가리지 않고** 훑는다. set 은 add 와
// 같은 자리로 흘러가므로 같은 차단선을 지나야 한다 (설계 10절 · 리뷰 A #1).
// 칸 이름을 손으로 적어 두면 `sources`·`author` 처럼 뒤에 늘어난 칸이 검사
// 없이 지나간다 — migrate 가 채우는 것이 바로 그 둘이다 (리뷰 A 2회차).
func (r *runner) scanPatch(set map[string]any) error {
	for _, field := range sortedKeys(set) {
		for _, text := range textsOf(set[field]) {
			if found := r.scanner.ScanLine(text); found != nil {
				return blocked(errors.New(i18n.T(i18n.SecretFoundIn, field, found.Rule)))
			}
		}
	}
	return nil
}

// sortedKeys 는 칸 차례를 고정한다. 맵 차례가 돌 때마다 달라지면 같은 요청이
// 어떤 때는 이 규칙, 어떤 때는 저 규칙으로 걸린다.
func sortedKeys(set map[string]any) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// textsOf 는 값 하나에서 훑을 글월을 다 꺼낸다. 글월과 글월 목록만 본다 —
// 숫자·참거짓에는 열쇠가 안 들어간다.
func textsOf(value any) []string {
	switch item := value.(type) {
	case string:
		return []string{item}
	case []string:
		return item
	case []any:
		return asList(item)
	}
	return nil
}

// appendTo 는 거의 같은 말을 하는 기억 뒤에 한 절을 붙인다. 이미 들어 있는
// 본문은 두 번 안 붙어서, 쓰다 죽은 판을 그냥 다시 돌려도 된다.
func (r *runner) appendTo(name string, twin *candidate, request *store.AddRequest) error {
	file, err := r.store.ReadMemory(twin.Path)
	if err != nil {
		return retryable(err)
	}
	if strings.Contains(file.Memory.Body, strings.TrimSpace(request.Body)) {
		r.result.Duplicated++
		r.record(name, OutcomeDuplicate, twin.ID)
		return nil
	}
	merged := file.Memory.Body + "\n\n" + i18n.T(i18n.AppendHeading, r.today()) + "\n\n" + request.Body
	// 붙이면 한 파일 한 주제가 아니게 된다. 그럴 바에는 새 기억으로 둔다.
	if lineCount(merged) > mergedMaxLines {
		return r.createNew(name, request)
	}
	file.Memory.Body = merged
	if err := r.store.WriteMemory(file.Memory); err != nil {
		return retryable(err)
	}
	r.result.Appended++
	r.record(name, OutcomeAppended, twin.ID)
	return nil
}

// createNew 는 md 파일을 만든다. id 가 큐 파일 이름에서 나오므로 같은 항목을
// 두 번 승격해도 같은 자리에 덮어쓸 뿐 두 벌이 안 생긴다 (설계 4-1).
func (r *runner) createNew(name string, request *store.AddRequest) error {
	date := request.Date
	if date == "" {
		date = r.today()
	}
	memory := model.Memory{
		ID: r.freeID(name, request.Body, date), Type: request.Type, Date: date,
		Summary: request.Summary, Tags: request.Tags, Scope: request.Scope,
		Title: request.Title, Pinned: request.Pinned, Severity: request.Severity,
		Importance: request.Importance, InvalidAt: request.InvalidAt, Links: request.Links,
		Body: request.Body, Sources: request.Sources, StaleAfter: request.StaleAfter,
		TodoStatus: request.Status, Author: request.Author, Review: request.Review,
		Spec: model.SpecV2,
	}
	// 옛 규격으로 들어온 큐 파일(author 없이 source 만)은 그대로 옛 규격으로
	// 쓴다. 도구가 남의 규격을 말없이 올리면 안 된다 (설계 2-5).
	if memory.Author == "" {
		memory.Spec = model.SpecV1
		memory.LegacySource = request.Source
		memory.Author = model.LegacyAuthor(request.Source)
		memory.LegacyStatus = request.Status
	}
	if problems := model.ValidateWith(&memory, memory.Spec, r.types); len(problems) > 0 {
		return problems[0]
	}
	if err := r.store.WriteMemory(&memory); err != nil {
		return retryable(err)
	}
	r.remember(memory.ID, model.StorePath(memory.ID), request)
	r.result.Added++
	r.record(name, OutcomeNew, memory.ID)
	// id 충돌로 소금을 쳤으면 add 가 찍은 큐 id 와 다르다. 같은 회차에 뒤따르는
	// 덮임 표시(patch)가 없는 id 를 달지 않게 새 id 로 다시 댄다.
	if queued := model.QueueID(name, request.Body, date); queued != memory.ID {
		if r.redirect == nil {
			r.redirect = map[string]string{}
		}
		r.redirect[queued] = memory.ID
	}
	return nil
}

// idSalt 는 32비트 해시가 같은 날 두 번 나왔을 때 몇 번까지 다시 뽑는지다.
const idSalt = 8

// freeID 는 이 큐 항목의 id 다. 그 자리에 **다른 내용의** 기억이 이미 있으면
// 소금을 쳐 다시 뽑는다. 같은 항목을 두 번 승격하면 내용이 똑같으니 같은 id 가
// 나오고, 그때는 덮어쓰는 것이 맞다 (설계 4-1 · 리뷰 A #12).
func (r *runner) freeID(name, body, date string) string {
	key := name
	for round := 0; round < idSalt; round++ {
		id := model.QueueID(key, body, date)
		if !r.taken(id, body) {
			return id
		}
		key = name + "\x01" + strconv.Itoa(round)
	}
	return model.QueueID(key, body, date)
}

// taken 은 그 id 자리에 다른 기억이 앉아 있는지 본다.
func (r *runner) taken(id, body string) bool {
	file, err := r.store.ReadMemory(model.StorePath(id))
	if err != nil {
		return false
	}
	return strings.TrimSpace(file.Memory.Body) != strings.TrimSpace(body)
}

// applyPatch 는 고칠 수 있다고 정해 둔 칸만 고친다.
func (r *runner) applyPatch(request *store.PatchRequest) error {
	if value, ok := request.Set["superseded_by"].(string); ok && r.dropped[value] {
		// 덮는 기억이 bad 로 갔다. 표시를 달면 옛 기억이 없는 id 를 가리킨다.
		return errors.New(i18n.T(i18n.SupersedeLost, value))
	}
	if r.retarget(request) {
		return nil
	}
	file, err := r.openTarget(request.ID)
	if err != nil {
		return err
	}
	if err := r.scanPatch(request.Set); err != nil {
		return err
	}
	if err := patchFields(file.Memory, request.Set); err != nil {
		return err
	}
	// 고치기가 멀쩡한 파일을 깨뜨리면 안 되니 전체를 다시 검사한다.
	if problems := model.ValidateWith(file.Memory, file.Memory.Spec, r.types); len(problems) > 0 {
		return problems[0]
	}
	if err := r.store.WriteMemory(file.Memory); err != nil {
		return retryable(err)
	}
	r.result.Patched++
	return nil
}

// openTarget 은 id 가 가리키는 기억 파일을 연다. 색인에 없으면 파일 자리로도
// 찾아본다 — 방금 승격된 기억은 아직 색인에 없다.
func (r *runner) openTarget(id string) (*store.MemoryFile, error) {
	if !model.IsID(id) {
		return nil, errors.New(i18n.T(i18n.BadID, id))
	}
	path, err := r.db.pathByID(id)
	if err != nil {
		return nil, err
	}
	if path == "" {
		path = model.StorePath(id)
	}
	file, err := r.store.ReadMemory(path)
	if err != nil {
		return nil, errors.New(i18n.T(i18n.NoSuchMemory, id))
	}
	return file, nil
}

func patchFields(memory *model.Memory, set map[string]any) error {
	for key, value := range set {
		switch key {
		case "pinned":
			// "true"(문자열)나 1 을 조용히 false 로 읽으면 반대로 적용된다
			// (리뷰 A #13).
			flag, ok := value.(bool)
			if !ok {
				return errors.New(i18n.T(i18n.NotBoolValue, key))
			}
			memory.Pinned = flag
		case "todo_status", "status":
			// v0.2 이름은 todo_status 다. 옛 큐 파일이 status 로 보내는 것도
			// 같이 받는다 — 판을 올리는 중에 큐에 남아 있던 것이 죽으면 안 된다.
			memory.TodoStatus = asText(value)
			if memory.IsLegacy() {
				memory.LegacyStatus = memory.TodoStatus
			}
		case "stale_after":
			memory.StaleAfter = asText(value)
		case "author":
			memory.Author = asText(value)
		case "sources":
			memory.Sources = asList(value)
		case "migrated":
			// migrate 가 옛 기억을 새 규격으로 올리는 자리다. 표시가 켜지면
			// 그때부터 v0.2 자로 검사하고, 옛 칸(source·status)은 안 쓴다.
			flag, ok := value.(bool)
			if !ok {
				return errors.New(i18n.T(i18n.NotBoolValue, key))
			}
			memory.Migrated = flag
			if flag {
				memory.Spec = model.SpecV2
				memory.LegacySource = ""
				memory.LegacyStatus = ""
			}
		case "review":
			// 자동 생성 표시다. `review --promote` 가 false 로 떼고, 그때
			// 머리말에서 줄이 사라진다 (결정 6).
			flag, ok := value.(bool)
			if !ok {
				return errors.New(i18n.T(i18n.NotBoolValue, key))
			}
			memory.Review = flag
		case "invalid_at":
			memory.InvalidAt = asText(value)
		case "superseded_by":
			memory.SupersededBy = asText(value)
		case "links":
			memory.Links = asList(value)
		case "summary":
			memory.Summary = asText(value)
		case "tags":
			memory.Tags = asList(value)
		case "scope":
			memory.Scope = asText(value)
		case "severity":
			memory.Severity = asText(value)
		case "title":
			memory.Title = asText(value)
		case "importance":
			memory.Importance = asInt(value)
		default:
			return errors.New(i18n.T(i18n.NotPatchable, key))
		}
	}
	return nil
}

// applyBody 는 본문을 갈아 끼운다. id 와 날짜는 안 움직여서 링크와 파일 자리가
// 계속 같은 기억을 가리킨다. **아카이브 쓰기가 먼저다** (설계 5-2).
func (r *runner) applyBody(request *store.AmendRequest) error {
	file, err := r.openTarget(request.ID)
	if err != nil {
		return err
	}
	// 검사가 아카이브 쓰기보다 먼저다. 나쁜 본문 때문에 옛 판을 한 줄 더
	// 남길 이유가 없다 (리뷰 A #1).
	if found := r.scanner.ScanText(request.Body); found != nil {
		return blocked(errors.New(i18n.T(i18n.SecretFound, found.Line, found.Rule)))
	}
	if _, err := r.store.AppendAmendRecord(request.ID, file.Path, string(model.Encode(file.Memory)), r.now); err != nil {
		return retryable(err)
	}
	file.Memory.Body = request.Body
	if problems := model.ValidateWith(file.Memory, file.Memory.Spec, r.types); len(problems) > 0 {
		return problems[0]
	}
	if err := r.store.WriteMemory(file.Memory); err != nil {
		return retryable(err)
	}
	r.result.Patched++
	return nil
}

func asText(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

// asInt 는 JSON 이 실어 온 숫자를 읽는다. encoding/json 은 숫자를 float64 로
// 주므로 두 모양을 다 받는다.
func asInt(value any) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	}
	return 0
}

func lineCount(text string) int {
	return strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
}

func asList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func titleOf(request *store.AddRequest) string {
	memory := model.Memory{Title: request.Title, Summary: request.Summary}
	return memory.DisplayTitle()
}

func (r *runner) today() string {
	return r.now.Format(model.DayLayout)
}

func bodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// jaccard 는 제목 둘을 글자 바이그램 집합으로 견준다. 한글은 띄어쓰기가
// 낱말 경계가 아니라 이 방식이 맞다.
func jaccard(left, right string) float64 {
	first := bigramSet(left)
	second := bigramSet(right)
	if len(first) == 0 || len(second) == 0 {
		return 0
	}
	shared := 0
	for gram := range first {
		if second[gram] {
			shared++
		}
	}
	return float64(shared) / float64(len(first)+len(second)-shared)
}

func bigramSet(text string) map[string]bool {
	letters := []rune(text)
	set := map[string]bool{}
	for i := 0; i+1 < len(letters); i++ {
		set[string(letters[i:i+2])] = true
	}
	return set
}
