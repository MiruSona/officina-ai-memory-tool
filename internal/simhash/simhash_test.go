package simhash

import (
	"strings"
	"testing"
)

func TestSameTextSameFingerprint(t *testing.T) {
	text := "색인은 파생물이라 지워도 된다. mem index --full 로 다시 만든다."
	if Of(text) != Of(text) {
		t.Fatal("같은 글인데 지문이 다르다")
	}
	if Of(text) != OfGrams(Grams(text)) {
		t.Fatal("Of 와 OfGrams 가 다른 지문을 냈다")
	}
}

func TestWhitespaceDoesNotChangeFingerprint(t *testing.T) {
	one := "훅은 세션을 막지 않는다 못 하겠으면 아무것도 안 찍는다"
	two := "훅은 세션을   막지 않는다\n못 하겠으면\t아무것도 안 찍는다"
	if Of(one) != Of(two) {
		t.Fatal("공백만 다른데 지문이 달라졌다")
	}
}

func TestSimilarTextIsCloserThanDifferent(t *testing.T) {
	base := "락은 하나뿐이다. index 와 gc 와 lint --fix 만 store 를 고쳐 쓴다."
	near := "락은 하나뿐이다. index 와 gc 와 lint --fix 만 store 를 고쳐 쓴다. 그 외는 inbox 뿐."
	far := "검색은 라우팅과 완화 사다리와 RRF 합치기 셋으로 돌아간다. 임베딩은 안 쓴다."
	if Distance(Of(base), Of(near)) >= Distance(Of(base), Of(far)) {
		t.Fatalf("닮은 글이 더 가까워야 한다 : near %d far %d",
			Distance(Of(base), Of(near)), Distance(Of(base), Of(far)))
	}
}

func TestBandsSplitTheFingerprint(t *testing.T) {
	value := uint64(0x1122334455667788)
	bands := Bands(value)
	restored := uint64(0)
	for at, band := range bands {
		restored |= uint64(band) << uint(at*BandBits)
	}
	if restored != value {
		t.Fatalf("밴드를 도로 붙이면 원래 지문이어야 한다 : %x", restored)
	}
}

func TestNearCopySharesABand(t *testing.T) {
	body := strings.Repeat("색인은 파생물이다. 원본은 md 파일이다. ", 20)
	copied := body + "한 줄 덧붙였다."
	left, right := Bands(Of(body)), Bands(Of(copied))
	shared := false
	for at := range left {
		if left[at] == right[at] {
			shared = true
		}
	}
	if !shared {
		t.Fatal("거의 같은 글인데 밴드가 하나도 안 겹쳤다")
	}
}

func TestJaccard(t *testing.T) {
	left := Grams("가나다라마")
	if score := Jaccard(left, left); score != 1 {
		t.Fatalf("같은 집합은 1 이어야 한다 : %v", score)
	}
	if score := Jaccard(left, Grams("바사아자차")); score != 0 {
		t.Fatalf("겹치는 게 없으면 0 이어야 한다 : %v", score)
	}
	if score := Jaccard(map[string]bool{}, map[string]bool{}); score != 0 {
		t.Fatalf("빈 집합은 0 이어야 한다 : %v", score)
	}
}
