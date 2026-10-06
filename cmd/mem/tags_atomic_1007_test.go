package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/fileio"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// tags --add 가 vocab.toml 을 덮을 때 옛 바이트를 local/backup 에 남긴다.
func TestTagsAddLeavesBackup(t *testing.T) {
	memory := newRepo(t)
	path := filepath.Join(memory, config.VocabFileName)
	old, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("시험 저장소에 vocab.toml 이 없다 : %v", err)
	}
	if _, code := capture(t, func() int { return run([]string{"tags", "--add", "shader=design"}) }); code != exitOK {
		t.Fatal("tags --add 실패")
	}
	backup := filepath.Join(store.LocalDir(memory), "backup", config.VocabFileName+fileio.BackupSuffix)
	saved, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("백업이 없다 : %v", err)
	}
	if string(saved) != string(old) {
		t.Fatal("백업이 옛 바이트와 다르다")
	}
}

// 메모장이 붙인 BOM 으로 시작하는 vocab.toml 도 tags --add 가 고친다.
// 원래 있던 어휘 항목은 다시 쓴 파일에 남는다 (BOM 은 EncodeVocab 대로 안 붙는다).
func TestTagsAddReadsVocabWithBOM(t *testing.T) {
	memory := newRepo(t)
	path := filepath.Join(memory, config.VocabFileName)
	text := "\xEF\xBB\xBF[tag]\n\"zetaparent\" = [\"zetachild\"]\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := captureBoth(t, func() int { return run([]string{"tags", "--add", "shader=design"}) })
	if code != exitOK {
		t.Fatalf("BOM 있는 vocab.toml 에서 tags --add 실패 (%d) :\n%s", code, out)
	}
	now, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"zetaparent", "zetachild", "shader"} {
		if !strings.Contains(string(now), want) {
			t.Fatalf("다시 쓴 vocab.toml 에 %q 가 없다 :\n%s", want, now)
		}
	}
}

// 읽은 뒤 남이 vocab.toml 을 바꿨으면 덮지 않고 실패로 끝난다.
func TestSaveVocabRefusesRaced(t *testing.T) {
	memory := newRepo(t)
	repository, err := config.Open(memory)
	if err != nil {
		t.Skipf("저장소를 못 연다 : %v", err)
	}
	before, err := readVocabBytes(repository)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(memory, config.VocabFileName)
	raced := append(append([]byte{}, before...), []byte("\n# 남이 더한 줄\n")...)
	os.WriteFile(path, raced, 0o644)
	out, code := captureBoth(t, func() int { return saveVocab(repository, vocabOf(repository), before) })
	if code == exitOK || !strings.Contains(out, "다른 프로그램이 파일을 바꿨다") {
		t.Fatalf("경합을 못 잡았다 (%d) :\n%s", code, out)
	}
	now, _ := os.ReadFile(path)
	if string(now) != string(raced) {
		t.Fatal("경합인데 파일을 덮었다")
	}
}

// 별칭 저장이 경합으로 실패하면 태그 패치도 큐에 안 들어간다 (별칭 먼저 · 패치 나중).
func TestApplyRenameSkipsPatchWhenVocabRaced(t *testing.T) {
	memory := newRepo(t)
	repository, err := config.Open(memory)
	if err != nil {
		t.Skipf("저장소를 못 연다 : %v", err)
	}
	before, err := readVocabBytes(repository)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(memory, config.VocabFileName)
	os.WriteFile(path, append(append([]byte{}, before...), []byte("\n# 남이 더한 줄\n")...), 0o644)
	opened := store.Open(memory, false)
	if err := opened.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	plans := [][3]string{{"20260101-deadbeef", "old", "new"}}
	_, code := captureBoth(t, func() int { return applyRenameLocked(repository, opened, plans, "old", "new", before) })
	if code == exitOK {
		t.Fatal("경합인데 성공으로 끝났다")
	}
	entries, _ := os.ReadDir(opened.InboxNewDir())
	if len(entries) != 0 {
		t.Fatalf("별칭 저장이 실패했는데 패치 %d 개가 큐에 들어갔다", len(entries))
	}
}
