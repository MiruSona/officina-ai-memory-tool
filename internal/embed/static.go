// Package embed 는 증류한 정적 임베딩 표를 읽고 문장 벡터를 만든다 (설계 4-8).
//
// **표가 없으면 조용히 없는 대로 돈다.** 이 패키지가 nil 을 돌려주면 검색은
// 낱말만으로 답한다(우아한 퇴화). 그래서 여기서 나는 오류는 검색을 죽이지
// 않는다.
//
// 표는 `tools/embed/distill.py` 가 굽는다. 어휘를 **우리 토크나이저가 낸
// 조각**으로 못박았기 때문에 여기에는 토크나이저 구현이 하나도 없다 —
// 조회하고, 더하고, 정규화하는 것이 전부다.
package embed

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/token"
)

// Magic 은 파일 앞머리 표식이다. 다른 파일을 가리키면 여기서 걸린다.
const Magic = "MEMKOBIN"

// Version 은 이 exe 가 읽을 수 있는 표 판이다.
const Version = 1

// MaxDim 과 MaxVocab 은 망가진 파일이 메모리를 통째로 먹지 않게 하는 고삐다.
const (
	MaxDim   = 1024
	MaxVocab = 1 << 20
)

// Table 은 조각 하나마다 벡터 하나를 가진 표다.
type Table struct {
	dim    int
	scale  float64
	index  map[string]int32
	weight []float32
	// data 는 어휘수 × dim 개의 int8 이다. 실수로 펴 두면 20k × 128 × 4바이트
	// 라 10MB 를 더 쓴다 — 쓸 때만 실수로 바꾼다.
	data []int8
}

// Dim 은 벡터 차원이다.
func (t *Table) Dim() int { return t.dim }

// Size 는 어휘 수다.
func (t *Table) Size() int { return len(t.weight) }

// Load 는 ko.bin 을 읽는다. 꼴이 안 맞으면 오류다 — 부르는 쪽이 이걸 보고
// 조용히 끄면 된다.
func Load(path string) (*Table, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse 는 읽어 둔 바이트에서 표를 만든다. 시험이 파일 없이 부른다.
func Parse(raw []byte) (*Table, error) {
	const headSize = len(Magic) + 16
	if len(raw) < headSize || string(raw[:len(Magic)]) != Magic {
		return nil, errors.New("임베딩 표가 아니다")
	}
	at := len(Magic)
	version := binary.LittleEndian.Uint32(raw[at:])
	dim := int(binary.LittleEndian.Uint32(raw[at+4:]))
	count := int(binary.LittleEndian.Uint32(raw[at+8:]))
	scale := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[at+12:])))
	at += 16
	if version != Version {
		return nil, fmt.Errorf("임베딩 표 판이 %d 다. 이 exe 는 %d 만 읽는다", version, Version)
	}
	if dim <= 0 || dim > MaxDim || count <= 0 || count > MaxVocab {
		return nil, fmt.Errorf("임베딩 표 머리말이 이상하다 : 차원 %d · 어휘 %d", dim, count)
	}
	table := &Table{dim: dim, scale: scale, index: make(map[string]int32, count)}
	for slot := 0; slot < count; slot++ {
		if at >= len(raw) {
			return nil, errors.New("임베딩 표 어휘가 잘렸다")
		}
		length := int(raw[at])
		at++
		if at+length > len(raw) {
			return nil, errors.New("임베딩 표 어휘가 잘렸다")
		}
		table.index[string(raw[at:at+length])] = int32(slot)
		at += length
	}
	if at+4*count > len(raw) {
		return nil, errors.New("임베딩 표 가중치가 잘렸다")
	}
	table.weight = make([]float32, count)
	for slot := 0; slot < count; slot++ {
		table.weight[slot] = math.Float32frombits(binary.LittleEndian.Uint32(raw[at:]))
		at += 4
	}
	if at+count*dim != len(raw) {
		return nil, fmt.Errorf("임베딩 표 행렬 길이가 안 맞는다 : 남은 %d · 있어야 할 %d",
			len(raw)-at, count*dim)
	}
	table.data = make([]int8, count*dim)
	for offset, value := range raw[at:] {
		table.data[offset] = int8(value)
	}
	return table, nil
}

// Zero 는 더할 자리를 만든다.
func (t *Table) Zero() []float64 { return make([]float64, t.dim) }

// Add 는 조각들의 벡터를 가중치를 곱해 더한다. 표에 있는 조각 수를 돌려준다.
// 흔한 조각은 표가 구울 때 이미 눌러 놨다(SIF) — 여기서는 factor 로 어느 칸
// (제목·요약·본문)인지만 더 실어 준다.
func (t *Table) Add(sum []float64, pieces []string, factor float64) int {
	found := 0
	for _, piece := range pieces {
		slot, have := t.index[piece]
		if !have {
			slot, have = t.index[strings.ToLower(piece)]
		}
		if !have {
			continue
		}
		found++
		weight := float64(t.weight[slot]) * factor * t.scale
		row := t.data[int(slot)*t.dim : (int(slot)+1)*t.dim]
		for at, value := range row {
			sum[at] += weight * float64(value)
		}
	}
	return found
}

// Unit 은 길이를 1 로 맞춘다. 길이가 0 이면 nil 이다 — 아는 조각이 하나도
// 없었다는 뜻이라 닮음을 재면 안 된다.
func Unit(sum []float64) []float64 {
	total := 0.0
	for _, value := range sum {
		total += value * value
	}
	if total <= 0 {
		return nil
	}
	length := math.Sqrt(total)
	out := make([]float64, len(sum))
	for at, value := range sum {
		out[at] = value / length
	}
	return out
}

// Dot 은 정규화된 두 벡터의 내적, 곧 코사인 닮음이다.
func Dot(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}
	total := 0.0
	for at, value := range a {
		total += value * b[at]
	}
	return total
}

// Pieces 는 글 한 덩어리를 색인과 똑같이 쪼갠다. 표의 어휘가 이 함수로
// 만들어졌으므로 여기가 어긋나면 표가 통째로 안 맞는다.
func Pieces(text string) []string {
	return strings.Fields(token.ForIndex(text))
}

// Text 는 글 한 덩어리의 단위 벡터다. 아는 조각이 없으면 nil 이다.
func (t *Table) Text(text string) []float64 {
	sum := t.Zero()
	if t.Add(sum, Pieces(text), 1) == 0 {
		return nil
	}
	return Unit(sum)
}
