package review

import "testing"

// TestValueQueueGetsB12 는 숫자·경로가 없는 history 가 value 큐에 오는지 본다.
// 예전엔 B12 가 기억 하나씩 보는 검사에서만 나와 review 의 value 큐가 늘 비었다
// (점검·정리 설계 4절).
func TestValueQueueGetsB12(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-aaaa0001", "history", "", "")
	report := runOn(t, opened, Options{Kinds: []string{KindValue}})
	if kinds(report)[KindValue] != 1 {
		t.Fatalf("value 큐가 1건이어야 한다 : %+v", report.Items)
	}
}

// TestValueQueueSkipsObservation 은 모음 기억(observation)을 value 큐에 안 올리는지 본다.
func TestValueQueueSkipsObservation(t *testing.T) {
	opened := newRepo(t)
	put(t, opened, "20260105-aaaa0001", "observation", "", "")
	report := runOn(t, opened, Options{Kinds: []string{KindValue}})
	for _, item := range report.Items {
		if item.Rule == "no-value" {
			t.Fatalf("observation 이 B12 로 올라왔다 : %+v", item)
		}
	}
}
