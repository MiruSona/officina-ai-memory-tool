package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// W4 — 한 파일이라도 해시가 안 맞으면 **모델 폴더를 통째로** 지운다.
//
// 예전에는 깨진 `config.json` 하나만 지우고 모델·토크나이저 129MiB 를 홈에
// 남겼다. 반쪽짜리 모델은 안 도는데 죽은 무게만 남고 아무도 안 알려 줬다.
func TestBrokenBundleWipesWholeModelDir(t *testing.T) {
	newHome(t)
	t.Setenv("MEM_INSTALL_NO_PATH", "1")
	dir := embed.ModelDir(embed.DefaultModel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"model_int8.onnx", "tokenizer.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("깨진 내용"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 빈 폴더를 꾸러미로 주면 어느 파일도 못 놓는다 = 해시 불일치와 같은 자리다.
	report, err := Install(Options{Apply: true, Bundle: t.TempDir()})
	if err != nil {
		t.Fatalf("install 실패 : %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		left, _ := os.ReadDir(dir)
		t.Fatalf("모델 폴더가 남았다 : %v", left)
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, i18n.T(i18n.EmbedModelWiped, dir)) {
		t.Fatalf("지웠다고 안 알려 준다 : %s", notes)
	}
	if !strings.Contains(notes, "낱말 검색만") {
		t.Fatalf("낱말 모드로 끝냈다고 안 알려 준다 : %s", notes)
	}
}
