package main

import (
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

// rulesAt 은 판정에서 한 등급의 규칙 이름만 Catalog 차례로 뽑는다. Verdict.Rules 는
// 경고까지 섞어 내서, 거절 줄에 쓰면 막지 않은 규칙(E02 같은)이 거절 사유처럼 읽힌다
// (사용 피드백 2026-10-11).
func rulesAt(verdict quality.Verdict, level quality.Grade) []string {
	seen := map[string]bool{}
	for _, one := range verdict.Findings {
		if one.Level == level {
			seen[one.Rule] = true
		}
	}
	names := []string{}
	for _, rule := range quality.Catalog {
		if seen[rule.Name] {
			names = append(names, rule.Name)
		}
	}
	return names
}

// autoRejectRules 는 자동 관문 거절 규칙 뒤에 기존 관문의 거절 등급 규칙을 잇는다.
// 같은 이름은 두 목록을 통틀어 한 번만 적는다.
func autoRejectRules(verdict quality.Verdict, auto autoAdd) []string {
	names := []string{}
	for _, one := range auto.Result.Rejects {
		names = append(names, one.Rule)
	}
	rules := []string{}
	seen := map[string]bool{}
	for _, name := range append(names, rulesAt(verdict, quality.GradeReject)...) {
		if !seen[name] {
			seen[name] = true
			rules = append(rules, name)
		}
	}
	return rules
}
