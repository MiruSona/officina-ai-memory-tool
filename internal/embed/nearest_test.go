package embed

import "testing"

// Nearest 는 전부를 재서 코사인 내림으로 limit 건을 준다 (C2).
func TestNearestOrdersByCosine(t *testing.T) {
	loaded, err := LoadVectors(writeThree(t))
	if err != nil {
		t.Fatal(err)
	}
	near := loaded.Nearest(unit(1, 0, 0), 2)
	if len(near) != 2 || near[0].ID != "a" || near[1].ID != "b" {
		t.Fatalf("차례가 틀렸다 : %+v", near)
	}
	if near[0].Cos < near[1].Cos {
		t.Fatalf("코사인 내림이 아니다 : %+v", near)
	}
	if all := loaded.Nearest(unit(0, 0, 1), 10); len(all) != 3 || all[0].ID != "c" {
		t.Fatalf("limit 이 건수보다 크면 전부여야 한다 : %+v", all)
	}
}

// 빈 질의·0 건 요청·nil 표는 아무것도 안 준다 — 섞기가 조용히 꺼진다.
func TestNearestEmpty(t *testing.T) {
	loaded, err := LoadVectors(writeThree(t))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Nearest(nil, 5) != nil || loaded.Nearest(unit(1, 0, 0), 0) != nil {
		t.Fatal("빈 질의나 0건 요청에 답을 줬다")
	}
	var none *Vectors
	if none.Nearest(unit(1, 0, 0), 5) != nil {
		t.Fatal("nil 표가 답을 줬다")
	}
	var reranker *Reranker
	if reranker.Nearest(unit(1, 0, 0), 5) != nil {
		t.Fatal("nil Reranker 가 답을 줬다")
	}
}
