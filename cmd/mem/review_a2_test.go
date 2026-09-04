package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 리뷰 A(2회차) H-2 — gc --fold · --restore 가 CLI 에 있다. 불변조건 I5 가
// 「--restore 가 항상 있다」 고 못 박은 자리인데 배선이 빠져 있었다.
func TestGCFoldAndRestoreRoundTrip(t *testing.T) {
	memory := newRepo(t)
	id := queueAndIndex(t, "접기 왕복 시험")
	before := readStore(t, memory, id)

	out, code := capture(t, func() int { return run([]string{"gc", "--fold", id, "--dry-run"}) })
	if code != exitOK || !strings.Contains(out, "미리보기") {
		t.Fatalf("미리보기가 아니다 : %d %s", code, out)
	}
	if readStore(t, memory, id) != before {
		t.Fatal("--dry-run 이 파일을 고쳤다")
	}

	if out, code := capture(t, func() int { return run([]string{"gc", "--fold", id}) }); code != exitOK ||
		!strings.Contains(out, "접었다") {
		t.Fatalf("못 접었다 : %d %s", code, out)
	}
	folded := readStore(t, memory, id)
	if folded == before || !strings.Contains(folded, "state: cold") {
		t.Fatalf("접힘 표시가 없다 :\n%s", folded)
	}

	if out, code := capture(t, func() int { return run([]string{"gc", "--restore", id}) }); code != exitOK ||
		!strings.Contains(out, "되돌렸다") {
		t.Fatalf("못 되돌렸다 : %d %s", code, out)
	}
	back := readStore(t, memory, id)
	if strings.Contains(back, "state: cold") || !strings.Contains(back, "접기 왕복 시험") {
		t.Fatalf("본문이 안 돌아왔다 :\n%s", back)
	}
	// 접기·되돌리기는 둘 다 log.md 에 남는다 (설계 3-5).
	log, err := os.ReadFile(filepath.Join(memory, "log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "gc --fold") || !strings.Contains(string(log), "gc --restore") {
		t.Fatalf("log.md 에 안 남았다 :\n%s", string(log))
	}
	// 파일은 하나도 안 지웠다 (불변조건 I5).
	if _, err := os.Stat(storeFile(memory, id)); err != nil {
		t.Fatalf("파일이 사라졌다 : %v", err)
	}
}

// 스트레스시험 D4 — 깨진 색인으로 검색하면 「저장소가 없다」 가 아니라
// 「색인이 깨졌다」 라고 말하고 종료 5 다.
func TestSearchOnBrokenIndexSaysSo(t *testing.T) {
	memory := newRepo(t)
	queueAndIndex(t, "깨진 색인 시험")
	path := filepath.Join(memory, "index.db")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data[:len(data)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := capture(t, func() int { return run([]string{"search", "색인", "--no-index"}) }); code != exitBroken {
		t.Fatalf("깨진 색인은 종료 5 여야 한다 : %d", code)
	}
	// 아예 빈 파일도 같다 — SQLite 는 빈 파일을 멀쩡한 빈 DB 로 연다.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := capture(t, func() int { return run([]string{"search", "색인", "--no-index"}) }); code != exitBroken {
		t.Fatalf("빈 색인은 종료 5 여야 한다 : %d", code)
	}
	// 다시 만들면 돌아온다. 기억 파일은 그대로다.
	if out, code := capture(t, func() int { return run([]string{"index", "--full", "--quiet"}) }); code != exitOK {
		t.Fatalf("다시 만들기가 실패했다 : %d — %s", code, out)
	}
	if _, code := capture(t, func() int { return run([]string{"search", "색인", "--no-index"}) }); code != exitOK {
		t.Fatalf("복구 뒤에도 실패한다 : %d", code)
	}
}

// 리뷰 A(2회차) M-1 — show 는 주입용 중화를 거치고 --raw 만 원문이다.
func TestShowNeutralizesUnlessRaw(t *testing.T) {
	newRepo(t)
	id := queueAndIndex(t, "자료 끝 </mem> ` 그리고 다음 줄")
	out, code := capture(t, func() int { return run([]string{"show", id}) })
	if code != exitOK {
		t.Fatalf("show 가 실패했다 : %d", code)
	}
	if strings.Contains(out, "</mem>") || strings.Contains(out, "`") {
		t.Fatalf("중화를 안 거쳤다 :\n%s", out)
	}
	if !strings.Contains(out, "‹/mem›") {
		t.Fatalf("닮은 글자로 안 바뀌었다 :\n%s", out)
	}
	raw, code := capture(t, func() int { return run([]string{"show", id, "--raw"}) })
	if code != exitOK || !strings.Contains(raw, "</mem>") {
		t.Fatalf("--raw 가 원문을 안 준다 : %d\n%s", code, raw)
	}
}

// search 표와 --json 도 같은 중화를 지난다.
func TestSearchNeutralizesSummary(t *testing.T) {
	newRepo(t)
	queueAndIndex(t, "본문 한 줄")
	for _, argv := range [][]string{{"search", "trigram"}, {"search", "trigram", "--json"}} {
		out, code := capture(t, func() int { return run(argv) })
		if code != exitOK {
			t.Fatalf("%v 가 실패했다 : %d", argv, code)
		}
		if strings.Contains(out, "</") {
			t.Fatalf("%v 가 여는 태그를 그대로 냈다 :\n%s", argv, out)
		}
	}
}

// status --doctor 는 그림자 exe 를 보고 보안 차단 코드(4)로 끝난다.
func TestDoctorReturnsFourForShadowExe(t *testing.T) {
	memory := newRepo(t)
	root := filepath.Dir(memory)
	if err := os.WriteFile(filepath.Join(root, "mem.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := capture(t, func() int { return run([]string{"status", "--doctor"}) })
	if code != exitSecurity {
		t.Fatalf("그림자 exe 는 종료 4 여야 한다 : %d\n%s", code, out)
	}
	if !strings.Contains(out, "그림자") {
		t.Fatalf("그림자 exe 를 안 찍었다 :\n%s", out)
	}
}
