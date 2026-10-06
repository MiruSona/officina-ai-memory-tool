package i18n

// `mem eval --rule-sample` · `--rule-score` 가 쓰는 문장이다 (B-05 · B08 설계 2절).
// 다른 표를 건드리지 않고 여기서 덧붙인다.

const (
	EvalSampleNeedOut  Key = "eval-sample-need-out"
	EvalSampleBadInt   Key = "eval-sample-bad-int"
	EvalSampleTooFew   Key = "eval-sample-too-few"
	EvalSampleExists   Key = "eval-sample-exists"
	EvalSampleWrote    Key = "eval-sample-wrote"
	EvalScoreBadFile   Key = "eval-score-bad-file"
	EvalScoreLine      Key = "eval-score-line"
	EvalScorePass      Key = "eval-score-pass"
	EvalScoreFail      Key = "eval-score-fail"
	EvalScoreTooFew    Key = "eval-score-too-few"
	EvalScoreNoRate    Key = "eval-score-no-rate"
	EvalControlLine    Key = "eval-control-line"
	EvalControlUnseen  Key = "eval-control-unseen"
	EvalControlNoScore Key = "eval-control-no-score"
)

var evalSampleMessages = map[Key]string{
	EvalSampleNeedOut: "`--rule-sample` 은 `--out <파일>` 이 있어야 한다 — 기본 자리는 두지 않는다",
	EvalSampleBadInt:  "`--%s` 는 정수여야 한다 : %s",
	EvalSampleTooFew:  "표본 %d건 < %d — 못 잼",
	EvalSampleExists:  "%s 가 이미 있어 덮지 않는다 — 판정을 적어 둔 파일일 수 있다",
	EvalSampleWrote:   "표본 %d건을 %s 에 썼다 (울린 것 %d건 · 씨앗 %d). label 칸에 진짜 · 오탐 · 경계를 적는다",
	EvalScoreBadFile:  "표본 파일을 못 읽었다 : %s",
	EvalScoreLine:     "표본 %d건 · 진짜 %d · 오탐 %d · 경계 %d · 다시 판정 %d · 안 판정 %d · 정밀도 %s · pinned 오탐 %d → %s",
	EvalScorePass:     "통과",
	EvalScoreFail:     "미달",
	EvalScoreTooFew:   "못 잼",
	EvalScoreNoRate:   "—",
	EvalControlLine:   "대조군 : %s 거짓 %d",
	EvalControlUnseen: "대조군 : 못 봄 — 판정에서 뺐다 (%s)",
	// 골든셋은 읽었는데 그 규칙 줄이 없을 때다 (울린 적이 없으면 거짓도 0 이다).
	EvalControlNoScore: "대조군 : %s 울림 없음 · 거짓 0",
}

func init() {
	for key, text := range evalSampleMessages {
		messages[key] = text
	}
}
