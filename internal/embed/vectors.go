package embed

// Memory/vectors.bin — 문서 벡터 저장소 (결정 12·13).
//
// **`index.db` 와 똑같은 파생물이다.** 지우면 `index` 가 다시 만들고, 머리말의
// 모델 이름·판이 안 맞으면 통째로 다시 만든다. `.gitignore` 에 들어간다.
//
// 꼴은 int8 + 건당 float32 스케일이다. 384차원이면 벡터 하나가 388바이트고,
// 여기에 id 글자가 붙어 2만 건이 약 8MB 다 (G4 상한 20MB).
//
//	"MEMVEC02" 판(4) 차원(4) 건수(4)
//	모델이름 길이(2)+글자 · 모델판 길이(2)+글자
//	건마다 : id 길이(1)+글자 · **열쇠 uint64(8)** · 스케일 float32(4) · int8 × 차원
//
// **열쇠**는 「이 벡터를 무엇으로 만들었나」다 (리뷰 B · V5). 기억 파일 해시와
// 자르는 자(MaxText·maxTokens)를 같이 뜬 값이라, `index --full` 이 색인을
// 통째로 버려도 **열쇠가 같으면 벡터를 그대로 쓴다** (결정 13 「file_hash 가
// 바뀐 건만 다시 계산한다」). v01 판 파일은 못 읽으니 한 번 다시 만든다.

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/mirusona/officina-ai-memory-tool/internal/simhash"
)

// VectorFileName 은 저장소 안 자리다 — `Memory/vectors.bin`.
const VectorFileName = "vectors.bin"

const (
	vectorMagic   = "MEMVEC02"
	vectorVersion = uint32(2)
)

// maxVectors 는 망가진 파일이 메모리를 통째로 먹지 않게 하는 고삐다.
const maxVectors = 1 << 21

// bruteMax 는 짝을 다 재도 되는 저장소 크기다. 3,000건이면 코사인 900만 번,
// 384차원이라 몇 초다. 그 위로는 LSH 밴드로 좁힌다.
const bruteMax = 3000

// VectorPath 는 저장소 폴더 안의 벡터 파일 자리다.
func VectorPath(repoDir string) string {
	if repoDir == "" {
		return ""
	}
	return filepath.Join(repoDir, VectorFileName)
}

// Vectors 는 읽어 둔 벡터 묶음이다. 검색·중복 둘이 같은 것을 나눠 쓴다.
type Vectors struct {
	model   string
	version string
	dim     int
	order   []string
	byID    map[string][]float32
	// keys 는 건마다의 「무엇으로 만들었나」 값이다 (V5).
	keys map[string]uint64
	// bandsOnce 는 LSH 밴드를 **정말 쓸 때** 만들게 한다. 20k 에서 밴드
	// 만들기가 0.3초인데 검색은 밴드를 한 번도 안 본다 (리뷰 B · 검색 p95).
	bandsOnce sync.Once
	// signs 는 LSH 서명이다. 2만 건에서 짝을 다 재면 O(n²)라 못 쓴다.
	signs map[string]uint64
	bands [simhash.BandCount]map[uint16][]string
}

// Model 과 Version 은 머리말이다. `doctor` 가 지금 모델과 견준다.
func (v *Vectors) Model() string   { return v.model }
func (v *Vectors) Version() string { return v.version }
func (v *Vectors) Dim() int        { return v.dim }

// Len 은 담긴 벡터 수다.
func (v *Vectors) Len() int {
	if v == nil {
		return 0
	}
	return len(v.byID)
}

// Get 은 기억 하나의 단위 벡터다. 없으면 nil 이다.
func (v *Vectors) Get(id string) []float32 {
	if v == nil {
		return nil
	}
	return v.byID[id]
}

// Key 는 이 기억 벡터의 열쇠다. 없으면 0 이다.
func (v *Vectors) Key(id string) uint64 {
	if v == nil {
		return 0
	}
	return v.keys[id]
}

// Matches 는 이 파일이 지금 쓰는 모델과 같은 것으로 만들어졌는지다.
// 다르면 통째로 다시 만들어야 한다 (결정 13).
func (v *Vectors) Matches(model, version string, dim int) bool {
	return v != nil && v.model == model && v.version == version && v.dim == dim
}

// Cos 는 기억 둘의 코사인이다. 둘 중 하나라도 벡터가 없으면 0 이다 —
// 「모른다」 를 「안 닮았다」 로 쓰는 자리라, 부르는 쪽이 그 0 을 가산 없음으로
// 다룬다 (quality.Vectors).
func (v *Vectors) Cos(left, right string) float64 {
	if v == nil {
		return 0
	}
	one, other := v.byID[left], v.byID[right]
	if one == nil || other == nil {
		return 0
	}
	return Cos(one, other)
}

// Near 는 벡터가 가까운 기억 id 를 가까운 차례로 준다 (quality.Vectors).
// LSH 밴딩으로 후보를 좁힌 뒤 그 안에서만 코사인을 잰다.
func (v *Vectors) Near(id string, limit int) []string {
	if v == nil || limit <= 0 {
		return nil
	}
	mine := v.byID[id]
	if mine == nil {
		return nil
	}
	v.bandsOnce.Do(v.buildBands)
	scored := []scoredID{}
	if len(v.order) <= bruteMax {
		// 작은 저장소는 다 재는 것이 맞다. LSH 는 16비트 밴드가 통째로 같아야
		// 후보가 되는데, 코사인 0.9 짜리 짝도 밴드 넷 중 하나라도 맞을 확률이
		// 3할뿐이라 **놓치는 것이 잡는 것보다 많다**.
		for _, other := range v.order {
			if other == id {
				continue
			}
			scored = append(scored, scoredID{id: other, value: Cos(mine, v.byID[other])})
		}
	} else {
		seen := map[string]bool{id: true}
		for band, key := range simhash.Bands(v.signs[id]) {
			for _, other := range v.bands[band][key] {
				if seen[other] {
					continue
				}
				seen[other] = true
				scored = append(scored, scoredID{id: other, value: Cos(mine, v.byID[other])})
			}
		}
	}
	sort.Slice(scored, func(a, b int) bool {
		if scored[a].value != scored[b].value {
			return scored[a].value > scored[b].value
		}
		return scored[a].id < scored[b].id
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]string, 0, len(scored))
	for _, one := range scored {
		out = append(out, one.id)
	}
	return out
}

type scoredID struct {
	id    string
	value float64
}

// LoadVectors 는 벡터 파일을 읽는다. 없거나 깨졌으면 오류다 — 부르는 쪽이
// 그걸 보고 조용히 낱말 모드로 간다.
func LoadVectors(path string) (*Vectors, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseVectors(raw)
}

func parseVectors(raw []byte) (*Vectors, error) {
	const headSize = 8 + 4 + 4 + 4
	if len(raw) < headSize || string(raw[:8]) != vectorMagic {
		return nil, errors.New("벡터 파일이 아니다")
	}
	at := 8
	if binary.LittleEndian.Uint32(raw[at:]) != vectorVersion {
		return nil, errors.New("벡터 파일 판이 다르다")
	}
	at += 4
	dim := int(binary.LittleEndian.Uint32(raw[at:]))
	at += 4
	count := int(binary.LittleEndian.Uint32(raw[at:]))
	at += 4
	if dim <= 0 || dim > 4096 || count < 0 || count > maxVectors {
		return nil, fmt.Errorf("벡터 파일 머리말이 이상하다 : 차원 %d · 건수 %d", dim, count)
	}
	model, at, err := readShort(raw, at)
	if err != nil {
		return nil, err
	}
	version, at, err := readShort(raw, at)
	if err != nil {
		return nil, err
	}
	out := &Vectors{model: model, version: version, dim: dim,
		order: make([]string, 0, count), byID: make(map[string][]float32, count),
		keys: make(map[string]uint64, count)}
	for slot := 0; slot < count; slot++ {
		if at >= len(raw) {
			return nil, errors.New("벡터 파일이 잘렸다")
		}
		size := int(raw[at])
		at++
		if at+size+8+4+dim > len(raw) {
			return nil, errors.New("벡터 파일이 잘렸다")
		}
		id := string(raw[at : at+size])
		at += size
		key := binary.LittleEndian.Uint64(raw[at:])
		at += 8
		scale := math.Float32frombits(binary.LittleEndian.Uint32(raw[at:]))
		at += 4
		vector := make([]float32, dim)
		for slot := 0; slot < dim; slot++ {
			vector[slot] = float32(int8(raw[at+slot])) * scale
		}
		at += dim
		out.order = append(out.order, id)
		out.byID[id] = vector
		out.keys[id] = key
	}
	return out, nil
}

func readShort(raw []byte, at int) (string, int, error) {
	if at+2 > len(raw) {
		return "", at, errors.New("벡터 파일 머리말이 잘렸다")
	}
	size := int(binary.LittleEndian.Uint16(raw[at:]))
	at += 2
	if at+size > len(raw) {
		return "", at, errors.New("벡터 파일 머리말이 잘렸다")
	}
	return string(raw[at : at+size]), at + size, nil
}

// WriteVectors 는 벡터 묶음을 원자로 쓴다 (tmp → rename). 반쯤 쓰인 파일이
// 절대 보이지 않는다.
func WriteVectors(path, model, version string, dim int, ids []string,
	rows map[string][]float32, keys map[string]uint64) error {
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if len(rows[id]) == dim && len(id) <= 255 {
			kept = append(kept, id)
		}
	}
	temporary := path + ".tmp"
	file, err := os.Create(temporary)
	if err != nil {
		return err
	}
	out := bufio.NewWriterSize(file, 1<<20)
	head := make([]byte, 0, 64)
	head = append(head, vectorMagic...)
	head = binary.LittleEndian.AppendUint32(head, vectorVersion)
	head = binary.LittleEndian.AppendUint32(head, uint32(dim))
	head = binary.LittleEndian.AppendUint32(head, uint32(len(kept)))
	head = binary.LittleEndian.AppendUint16(head, uint16(len(model)))
	head = append(head, model...)
	head = binary.LittleEndian.AppendUint16(head, uint16(len(version)))
	head = append(head, version...)
	out.Write(head)
	line := make([]byte, 0, 1+255+8+4+dim)
	for _, id := range kept {
		vector := rows[id]
		biggest := 0.0
		for _, value := range vector {
			if math.Abs(float64(value)) > biggest {
				biggest = math.Abs(float64(value))
			}
		}
		scale := biggest / 127
		if scale <= 0 {
			scale = 1
		}
		line = line[:0]
		line = append(line, byte(len(id)))
		line = append(line, id...)
		line = binary.LittleEndian.AppendUint64(line, keys[id])
		line = binary.LittleEndian.AppendUint32(line, math.Float32bits(float32(scale)))
		for _, value := range vector {
			step := math.Round(float64(value) / scale)
			if step > 127 {
				step = 127
			}
			if step < -127 {
				step = -127
			}
			line = append(line, byte(int8(step)))
		}
		out.Write(line)
	}
	if err := out.Flush(); err != nil {
		file.Close()
		os.Remove(temporary)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return err
	}
	return nil
}

// buildBands 는 LSH 서명과 밴드 표를 만든다. 서명은 고정 난수 평면 64장에
// 벡터를 쏘아 부호만 남긴 것이라 **코사인이 가까우면 서명도 가깝다**.
func (v *Vectors) buildBands() {
	planes := hyperplanes(v.dim)
	v.signs = make(map[string]uint64, len(v.byID))
	for band := range v.bands {
		v.bands[band] = map[uint16][]string{}
	}
	for _, id := range v.order {
		sign := signOf(v.byID[id], planes)
		v.signs[id] = sign
		for band, key := range simhash.Bands(sign) {
			v.bands[band][key] = append(v.bands[band][key], id)
		}
	}
}

// hyperplanes 는 고정 씨앗으로 만든 평면 64장이다. 씨앗이 고정이라 같은
// 벡터는 언제 어디서 계산해도 같은 서명을 낸다.
func hyperplanes(dim int) [64][]float32 {
	out := [64][]float32{}
	state := uint64(0x9E3779B97F4A7C15)
	for plane := 0; plane < 64; plane++ {
		row := make([]float32, dim)
		for at := 0; at < dim; at++ {
			state ^= state << 13
			state ^= state >> 7
			state ^= state << 17
			// -1 ~ 1 사이 균등 값이면 평면 방향으로 충분하다.
			row[at] = float32(int64(state>>32)%2001-1000) / 1000
		}
		out[plane] = row
	}
	return out
}

func signOf(vector []float32, planes [64][]float32) uint64 {
	sign := uint64(0)
	for plane := 0; plane < 64; plane++ {
		if Cos(vector, planes[plane]) >= 0 {
			sign |= 1 << uint(plane)
		}
	}
	return sign
}
