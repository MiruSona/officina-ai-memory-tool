package i18n

// `mem auto report` 가 쓰는 말과 R3 건너뜀 경고 기록 줄이다 (자동쌓기A1후속 2절 · 7절).
// 통과선(선 …)은 표시만 한다 — 판정은 사람이 한다.

const (
	AutoLogWarned       Key = "auto-log-warned"
	AutoReportPeriod    Key = "auto-report-period"
	AutoReportNudges    Key = "auto-report-nudges"
	AutoReportRejects   Key = "auto-report-rejects"
	AutoReportUndo      Key = "auto-report-undo"
	AutoReportMissed    Key = "auto-report-missed"
	AutoReportR3        Key = "auto-report-r3"
	AutoReportHook      Key = "auto-report-hook"
	AutoReportSearch    Key = "auto-report-search"
	AutoReportSample    Key = "auto-report-sample"
	AutoReportSampleRow Key = "auto-report-sample-row"
	AutoReportNone      Key = "auto-report-none"
)

var autoReportMessages = map[Key]string{
	AutoLogWarned:       "자동 · %s · %s",
	AutoReportPeriod:    "기간 %s ~ %s · 저장소 %s",
	AutoReportNudges:    "알림 %d회 (세션 %d) · 알림 뒤 자동 add 있는 세션 %d/%d = %s (선 ≥30%%)",
	AutoReportRejects:   "관문 거절 : %s",
	AutoReportUndo:      "되돌림률 : 되돌림 %d ÷ 자동 기억 %d = %s (선 ≤10%%)",
	AutoReportMissed:    "놓친 세션 : 큐 %d 중 covered 아님 %d (%s)",
	AutoReportR3:        "R3 : 거절 %d · 건너뜀 경고 %d (경고는 %s 부터 셈)",
	AutoReportHook:      "훅 비용 : 못 셈 — `mem hook stop --json` 의 ms 를 따로 20번",
	AutoReportSearch:    "검색 해 : 범위 밖 — 측정절차.md 대로 골든셋 v3 로 잰다",
	AutoReportSample:    "잡음률 표본 %d건 (씨앗 %d · 같은 씨앗이면 같은 목록):",
	AutoReportSampleRow: "  %d. %s %s%s %s %s — 판정: 쓸모/잡음/중복/틀림",
	AutoReportNone:      "없음",
}

func init() {
	for key, text := range autoReportMessages {
		messages[key] = text
	}
}
