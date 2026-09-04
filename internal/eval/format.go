package eval

import (
	"strconv"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/search"
)

// variantHead 는 질의 꼴 표의 머리다.
const variantHead = "| 질의 꼴 | 건수 | recall@5 | MRR |\n| --- | --- | --- | --- |"

// missCut 은 못 맞힌 질문을 몇 개까지 적는지다.
const missCut = 12

// Markdown 은 사람이 읽는 표다. --json 이 같은 숫자를 준다.
func Markdown(report *Report) string {
	out := strings.Builder{}
	out.WriteString(i18n.T(i18n.EvalHeader, report.Cases, verdict(report.Pass)) + "\n\n")
	out.WriteString(i18n.T(i18n.EvalRelaxedNote, search.RungOr) + "\n")
	if report.Hard > 0 {
		out.WriteString(i18n.T(i18n.EvalHardLine, report.Hard, report.HardHit) + "\n")
	}
	out.WriteString(i18n.T(i18n.EvalWideLine, report.WideRecall5, report.WideHit) + "\n")
	if report.AbstainWide > 0 {
		out.WriteString(i18n.T(i18n.EvalAbstainWide, report.Abstain, report.AbstainWide) + "\n")
	}
	if report.ExMultiCases > 0 {
		out.WriteString(exMultiLine(report) + "\n")
	}
	if report.Unreachable > 0 {
		out.WriteString(i18n.T(i18n.EvalUnreachable, report.Unreachable) + "\n")
	}
	out.WriteString("\n" + i18n.T(i18n.EvalTableHead) + "\n")
	for _, metric := range report.Metrics {
		out.WriteString(metricRow(metric) + "\n")
	}
	out.WriteString(groupTable(i18n.T(i18n.EvalKindHead), report.ByKind))
	out.WriteString(groupTable(i18n.T(i18n.EvalLangHead), report.ByLang))
	out.WriteString(groupTable(variantHead, report.ByVariant))
	if len(report.Warnings) > 0 {
		out.WriteString("\n" + i18n.T(i18n.EvalWarnHead) + "\n")
		for _, line := range report.Warnings {
			out.WriteString("- " + line + "\n")
		}
	}
	if len(report.Unknown) > 0 {
		out.WriteString("\n" + i18n.T(i18n.EvalUnknownID, strings.Join(report.Unknown, " ")) + "\n")
	}
	misses := missLines(report.Results)
	if len(misses) > 0 {
		out.WriteString("\n" + i18n.T(i18n.EvalMissHead) + "\n")
		out.WriteString(strings.Join(misses, "\n") + "\n")
	}
	out.WriteString("\n" + i18n.T(i18n.EvalFooter) + "\n")
	return out.String()
}

// exMultiLine 은 multisession 을 뺀 자를 나란히 보여주는 줄이다. 설계 4-6 이
// 그 유형은 검색으로 안 푼다고 했으므로 **어디가 구멍인지** 를 밝히되,
// 합격선 판정은 골든셋 전부로 한다는 것도 같이 말한다.
func exMultiLine(report *Report) string {
	return "multisession 을 빼면 recall@5 는 " + three(report.ExMultiRecall5) +
		" (" + strconv.Itoa(report.ExMultiCases) + "건)다. " +
		"설계 4-6 은 multisession 을 검색이 아니라 links 로 푼다고 했지만, " +
		"합격선 판정은 골든셋 전부로 한다."
}

func groupTable(head string, rows []GroupScore) string {
	if len(rows) == 0 {
		return ""
	}
	out := strings.Builder{}
	out.WriteString("\n" + head + "\n")
	for _, item := range rows {
		out.WriteString("| " + item.Name + " | " + strconv.Itoa(item.Cases) + " | " +
			three(item.Recall5) + " | " + three(item.MRR) + " |\n")
	}
	return out.String()
}

func metricRow(metric Metric) string {
	line := i18n.T(i18n.EvalThresholdLow, numberOf(metric.Key, metric.Threshold))
	if metric.Higher {
		line = i18n.T(i18n.EvalThresholdUp, numberOf(metric.Key, metric.Threshold))
	}
	return "| " + labelOf(metric.Key) + " | " + cellOf(metric) + " | " +
		line + " | " + verdict(metric.Pass) + " |"
}

func labelOf(key string) string {
	names := map[string]i18n.Key{
		MetricRecall5: i18n.MetricRecall5, MetricRecall10: i18n.MetricRecall10,
		MetricMRR: i18n.MetricMRR, MetricAbstain: i18n.MetricAbstain,
		MetricMisexclusion: i18n.MetricMisexclusion, MetricP95: i18n.MetricP95,
		MetricTokens: i18n.MetricTokens,
	}
	found, ok := names[key]
	if !ok {
		return localLabel(key)
	}
	return i18n.T(found)
}

// localLabel 은 v0.2 가 새로 더한 지표 이름이다. i18n 표를 안 건드리려고
// 여기 둔다 (파도 E 가 그 파일을 쥐고 있다).
func localLabel(key string) string {
	switch key {
	case MetricRecall5V01:
		return "recall@5 (v0.1 자)"
	case MetricLangGap:
		return "ko/en 격차"
	}
	return key
}

// cellOf 는 지표 한 칸이다. 안 잰 지표는 숫자 대신 그렇다고 적는다.
func cellOf(metric Metric) string {
	if !metric.Measured {
		return i18n.T(i18n.EvalNotMeasured)
	}
	return numberOf(metric.Key, metric.Value)
}

func numberOf(key string, value float64) string {
	if key == MetricTokens {
		return strconv.Itoa(int(value + 0.5))
	}
	if key == MetricP95 {
		return strconv.FormatFloat(value, 'f', 2, 64) + "ms"
	}
	return three(value)
}

func three(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

func verdict(pass bool) string {
	if pass {
		return i18n.T(i18n.EvalPass)
	}
	return i18n.T(i18n.EvalFail)
}

func missLines(results []CaseResult) []string {
	lines := []string{}
	for _, item := range results {
		line := missLine(item)
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) == missCut {
			return lines
		}
	}
	return lines
}

func missLine(item CaseResult) string {
	if item.Kind == KindAbstain {
		if len(item.Got) == 0 && !item.WrongAnswer {
			return ""
		}
		return i18n.T(i18n.EvalAbstainMiss, item.Q, len(item.Got))
	}
	if item.Rank > 0 && item.Rank <= 5 {
		return ""
	}
	tail := ""
	if item.PushedOut {
		tail = i18n.T(i18n.EvalMissPushed)
	}
	if item.Unreachable {
		tail += i18n.T(i18n.EvalMissUnreach)
	}
	return i18n.T(i18n.EvalMissLine, item.Q, item.Kind, rankText(item.Rank), tail)
}

func rankText(rank int) string {
	if rank == 0 {
		return i18n.T(i18n.EvalNoRank)
	}
	return strconv.Itoa(rank)
}
