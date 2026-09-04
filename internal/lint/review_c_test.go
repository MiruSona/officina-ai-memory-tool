package lint

import "testing"

// 리뷰 C #27 — 머리말에 사람이 적은 주석이 있으면 --fix 가 파일을 다시 안 쓴다.
func TestCommentedFrontMatterIsHeld(t *testing.T) {
	withComment := "---\nid: 20260820-ffff8888\n# 메모 : 이 값은 손으로 정했다\ntype: history\n---\n본문\n"
	if !commented(withComment) {
		t.Fatal("머리말 주석을 못 봤다")
	}
	plain := "---\nid: 20260820-ffff8888\ntype: history\n---\n# 본문 제목\n"
	if commented(plain) {
		t.Fatal("본문의 `#` 제목을 머리말 주석으로 봤다")
	}
}
