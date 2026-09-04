package embed

// 문장 임베딩 모델 (설계 결정 7·9·10·12·16).
//
// **없으면 nil 이다. 오류가 아니다.** 모델·DLL 이 없거나 해시가 안 맞으면
// `OpenModel` 이 nil 을 주고, 부르는 쪽은 낱말 검색만 한다 (우아한 퇴화).
//
// 규약은 e5 그대로다 : 질의는 `query: `, 문서는 `passage: ` 를 앞에 붙이고,
// 마지막 은닉층을 attention mask 로 평균 낸 뒤 L2 정규화한다.
//
// **DLL 은 절대경로로만 로드한다.** 이름만 넘기면 System32 의 옛
// onnxruntime.dll(API v10)이 잡혀 죽는다 — 0실측에서 실제로 났다.

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	ort "github.com/getcharzp/onnxruntime_purego"
	"github.com/sugarme/tokenizer"
)

// BatchSize 는 한 번에 묶어 넣을 문장 수다. 0실측에서 32 가 한 건씩보다
// 1.4배 빨랐고 8 → 32 이득은 작았다.
const BatchSize = 32

// maxTokens 는 한 문장을 자를 토큰 수다. e5-small 은 512 까지 받지만 긴 글은
// 시간이 그만큼 든다. 기억 하나의 뜻은 앞머리에 다 있다.
//
// **(리뷰 B · V4) 256 → 128.** 20k 벡터가 13.2분이라 자(10분)를 넘겼는데,
// 0단계 실측 환산(3.3분)과 4배 어긋난 원인이 이 값이었다 — 실측은 50~150자
// 문장이었다. 실기억 골든셋 v2 80건으로 128·96·64 를 다 재 봤고 **셋 다 r@5
// 0.761 로 256 과 같았다**(MRR 은 0.543 → 0.567 로 오히려 올랐다). 셋 중 가장
// 안전한 128 을 골랐다. 근거 표는 History 코드리뷰-B 문서 V4 절.
// **재기용 뒷문**은 `MEM_EMBED_MAXTOKENS` 다.
var maxTokens = envInt("MEM_EMBED_MAXTOKENS", 128)

// QueryPrefix 와 PassagePrefix 는 e5 규약이다. 안 붙이면 품질이 떨어진다.
const (
	QueryPrefix   = "query: "
	PassagePrefix = "passage: "
)

// Model 은 열린 세션 한 벌이다. 프로세스 하나에 하나만 연다.
type Model struct {
	name    string
	version string
	dim     int
	engine  *ort.Engine
	session *ort.Session
	tok     *tokenizer.Tokenizer
	// wantsTypes 는 이 그래프가 token_type_ids 를 받는지다. optimum 이 뽑은
	// ko-v2 는 안 받고, HF 에 올라온 원본 판은 받는다 — 안 받는 그래프에 넣으면
	// 「Invalid input name」 으로 죽는다.
	wantsTypes bool
	// out 은 문장 벡터를 담은 출력 이름이다.
	out string
}

// Name 은 모델 이름이다 (vectors.bin 머리말에 적힌다).
func (m *Model) Name() string { return m.name }

// Version 은 모델 파일 해시 앞 12자다. 파일이 바뀌면 이 값이 바뀌고,
// vectors.bin 이 통째로 다시 만들어진다.
func (m *Model) Version() string { return m.version }

// Dim 은 벡터 차원이다.
func (m *Model) Dim() int { return m.dim }

// Close 는 세션을 닫는다. 안 닫아도 프로세스가 끝나면 같이 사라진다.
func (m *Model) Close() {
	if m == nil {
		return
	}
	if m.session != nil {
		m.session.Destroy()
	}
	if m.engine != nil {
		m.engine.Destroy()
	}
}

// Ready 는 모델을 열 수 있는 상태인지다 — 파일이 다 있고 크기가 맞는지.
func Ready(model string) bool {
	_, ok := check(model)
	return ok
}

// check 는 **싼 검사**다 — 파일이 있고 크기가 박아 둔 값과 같은지만 본다.
// 113MB 를 해시하는 것은 `install` 때 한 번이면 된다. 검색마다 하면 그것이
// 곧 0.2초다. 못 갖춘 것 목록을 준다.
func check(model string) ([]string, bool) {
	missing := []string{}
	dir := ModelDir(model)
	if dir == "" {
		return []string{"집 폴더"}, false
	}
	for _, asset := range ModelAssets(model) {
		if !SizeOK(filepath.Join(dir, asset.Name), asset.Bytes) {
			missing = append(missing, asset.Name)
		}
	}
	runtimeAsset := RuntimeAsset()
	if !SizeOK(DLLPath(), runtimeAsset.Bytes) {
		missing = append(missing, runtimeAsset.Name)
	}
	return missing, len(missing) == 0
}

// CheckDeep 은 **박아 둔 SHA-256 까지 맞춰 보는 검사**다. `install` 과
// `status --embed` 만 이 길로 온다.
func CheckDeep(model string) ([]string, bool) {
	missing := []string{}
	dir := ModelDir(model)
	if dir == "" {
		return []string{"집 폴더"}, false
	}
	for _, asset := range ModelAssets(model) {
		if !SHAOK(filepath.Join(dir, asset.Name), asset.SHA) {
			missing = append(missing, asset.Name)
		}
	}
	runtimeAsset := RuntimeAsset()
	if !SHAOK(DLLPath(), runtimeAsset.SHA) {
		missing = append(missing, runtimeAsset.Name)
	}
	return missing, len(missing) == 0
}

// OpenModel 은 모델을 연다. **못 열면 nil 이다** — 부르는 쪽은 nil 이면
// 임베딩 갈래를 통째로 건너뛴다 (결정 16).
func OpenModel(model string) *Model {
	made, err := openModel(model)
	if err != nil {
		return nil
	}
	return made
}

// openModel 은 왜 못 열었는지까지 말한다. `status --embed` 가 이 길로 온다.
func openModel(model string) (*Model, error) {
	if model == "" {
		model = DefaultModel
	}
	if runtime.GOOS != "windows" {
		// DLL 이름과 자리가 윈도우 기준이다. 다른 OS 는 낱말 모드다.
		return nil, errors.New("이 OS 에서는 의미 검색을 아직 안 켠다")
	}
	if missing, ok := check(model); !ok {
		return nil, errors.New("모델 파일이 없거나 해시가 안 맞는다 : " + strings.Join(missing, ", "))
	}
	dir := ModelDir(model)
	dll, err := filepath.Abs(DLLPath())
	if err != nil {
		return nil, err
	}
	modelPath, err := filepath.Abs(filepath.Join(dir, ModelFile))
	if err != nil {
		return nil, err
	}
	tok, err := loadTokenizer(dir)
	if err != nil {
		return nil, err
	}
	engine, err := ort.NewEngine(dll)
	if err != nil {
		return nil, err
	}
	// 스레드 제한은 하지 않는다 — 준비 시간은 그대로인데 색인만 2배 느려진다
	// (0실측 스레드 표).
	session, err := engine.NewSession(modelPath, nil)
	if err != nil {
		engine.Destroy()
		return nil, err
	}
	made := &Model{name: model, version: ModelVersion(model), dim: Dim,
		engine: engine, session: session, tok: tok, out: hiddenName(session)}
	for _, name := range session.InputNames {
		if name == "token_type_ids" {
			made.wantsTypes = true
		}
	}
	return made, nil
}

// hiddenName 은 마지막 은닉층 출력 이름이다. 이름이 다른 export 도 있어서
// 못 찾으면 첫 출력을 쓴다.
func hiddenName(session *ort.Session) string {
	for _, name := range session.OutputNames {
		if name == "last_hidden_state" {
			return name
		}
	}
	if len(session.OutputNames) > 0 {
		return session.OutputNames[0]
	}
	return "last_hidden_state"
}

// Query 는 질의 한 줄의 단위 벡터다. 못 만들면 nil 이다.
func (m *Model) Query(text string) []float32 {
	if m == nil {
		return nil
	}
	clean := Clean(text)
	if clean == "" {
		return nil
	}
	made, err := m.encode([]string{QueryPrefix + clean})
	if err != nil || len(made) == 0 {
		return nil
	}
	return made[0]
}

// Passages 는 문서 여러 개의 단위 벡터다. 배치로 묶어 넣는다.
//
// **길이 차례로 정렬해서 묶는다.** 한 배치는 그 안에서 가장 긴 문장에 맞춰
// 패딩하므로, 짧은 것과 긴 것을 섞으면 짧은 쪽 자리를 통째로 낭비한다.
// 실측에서 이 정렬 하나가 색인 시간을 절반 아래로 줄였다.
func (m *Model) Passages(texts []string) ([][]float32, error) {
	if m == nil {
		return nil, errors.New("모델이 없다")
	}
	// 빈 글은 아예 안 태운다 — 규약 앞머리(`passage: `)만 남은 벡터는 뜻이
	// 없는데 서로 닮았다고 나온다 (리뷰 B · H-1 곁가지).
	clean := make([]string, len(texts))
	order := make([]int, 0, len(texts))
	for at := range texts {
		clean[at] = Clean(texts[at])
		if clean[at] == "" {
			continue
		}
		order = append(order, at)
	}
	sort.SliceStable(order, func(a, b int) bool {
		return len(clean[order[a]]) < len(clean[order[b]])
	})
	out := make([][]float32, len(texts))
	for at := 0; at < len(order); at += BatchSize {
		end := at + BatchSize
		if end > len(order) {
			end = len(order)
		}
		batch := make([]string, 0, end-at)
		for _, slot := range order[at:end] {
			batch = append(batch, PassagePrefix+clean[slot])
		}
		made, err := m.encode(batch)
		if err != nil {
			return nil, err
		}
		for one, slot := range order[at:end] {
			if one < len(made) {
				out[slot] = made[one]
			}
		}
	}
	return out, nil
}

// encode 는 한 배치를 실제로 돌린다. 짧은 문장 여러 개를 한 판에 넣으려고
// 가장 긴 것에 맞춰 패딩한다 — 패딩 토큰 id 는 XLM-R 규약대로 1 이다.
func (m *Model) encode(texts []string) ([][]float32, error) {
	count := len(texts)
	if count == 0 {
		return nil, nil
	}
	rows, fail := m.tokenize(texts)
	for _, why := range fail {
		if why != "" {
			Warn(fmt.Sprintf("벡터를 못 만든 기억이 있다 — %s", why))
		}
	}
	longest := 0
	for _, row := range rows {
		if len(row) > longest {
			longest = len(row)
		}
	}
	if longest == 0 {
		// 배치가 통째로 비었다. 오류가 아니라 「이 배치는 벡터가 없다」다.
		return make([][]float32, count), nil
	}
	inputIDs := make([]int64, count*longest)
	mask := make([]int64, count*longest)
	types := make([]int64, count*longest)
	for at, row := range rows {
		base := at * longest
		for slot, id := range row {
			inputIDs[base+slot] = int64(id)
			mask[base+slot] = 1
		}
		for slot := len(row); slot < longest; slot++ {
			inputIDs[base+slot] = 1
		}
	}
	shape := []int64{int64(count), int64(longest)}
	first, err := ort.NewTensor(shape, inputIDs)
	if err != nil {
		return nil, err
	}
	defer first.Destroy()
	second, err := ort.NewTensor(shape, mask)
	if err != nil {
		return nil, err
	}
	defer second.Destroy()
	inputs := map[string]*ort.Value{"input_ids": first, "attention_mask": second}
	if m.wantsTypes {
		third, err := ort.NewTensor(shape, types)
		if err != nil {
			return nil, err
		}
		defer third.Destroy()
		inputs["token_type_ids"] = third
	}
	outputs, err := m.session.Run(inputs)
	if err != nil {
		return nil, err
	}
	for name, value := range outputs {
		if name != m.out {
			value.Destroy()
		}
	}
	hidden := outputs[m.out]
	if hidden == nil {
		return nil, errors.New("모델이 last_hidden_state 를 안 낸다")
	}
	defer hidden.Destroy()
	data, err := ort.GetTensorData[float32](hidden)
	if err != nil {
		return nil, err
	}
	return meanPool(data, mask, count, longest, m.dim), nil
}

// tokenize 는 배치 하나를 토큰 id 로 바꾼다. 토큰화가 색인 시간의 3분의 1이라
// (실측 건당 28.6ms 중 10.2ms) 코어를 다 쓴다. 순수 Go 라 안전하다.
//
// **한 건이 죽어도 프로세스는 안 죽는다** (리뷰 B · H-1). 3자 토크나이저가
// 어떤 글자 조합에서 슬라이스 범위를 벗어나 panic 을 낸다. 고루틴 안에서
// 터지므로 부르는 쪽 recover 로는 못 잡는다 — **일꾼 안에서 건마다** 잡고,
// 그 한 건은 토큰 없이 두고 나머지를 계속 간다. 토큰이 빈 건은 encode 가
// 벡터 없음(nil)으로 돌려주고 Build 가 그 기억만 벡터 없이 지나간다.
func (m *Model) tokenize(texts []string) ([][]int, []string) {
	rows := make([][]int, len(texts))
	fail := make([]string, len(texts))
	workers := runtime.NumCPU()
	if workers > len(texts) {
		workers = len(texts)
	}
	group := sync.WaitGroup{}
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func(start int) {
			defer group.Done()
			for at := start; at < len(texts); at += workers {
				ids, why := m.encodeOne(texts[at])
				rows[at], fail[at] = ids, why
			}
		}(worker)
	}
	group.Wait()
	return rows, fail
}

// encodeOne 은 글 한 줄을 토큰으로 바꾼다. 죽으면 까닭 한 줄을 돌려준다.
func (m *Model) encodeOne(text string) (ids []int, why string) {
	defer func() {
		if problem := recover(); problem != nil {
			ids, why = nil, fmt.Sprintf("토크나이저가 이 글에서 죽었다 : %v", problem)
		}
	}()
	if strings.TrimSpace(text) == "" {
		return nil, "빈 글이라 벡터를 안 만든다"
	}
	encoded, err := m.tok.EncodeSingle(text, true)
	if err != nil {
		return nil, err.Error()
	}
	ids = encoded.Ids
	if len(ids) > maxTokens {
		// 끝의 </s> 는 살려 둔다. 끊긴 것을 모델이 알아야 한다.
		ids = append(append([]int{}, ids[:maxTokens-1]...), ids[len(ids)-1])
	}
	return ids, ""
}

// meanPool 은 패딩을 뺀 평균 풀링 + L2 정규화다.
func meanPool(data []float32, mask []int64, count, length, dim int) [][]float32 {
	out := make([][]float32, count)
	for row := 0; row < count; row++ {
		vector := make([]float32, dim)
		live := float32(0)
		for slot := 0; slot < length; slot++ {
			if mask[row*length+slot] == 0 {
				continue
			}
			live++
			base := (row*length + slot) * dim
			if base+dim > len(data) {
				break
			}
			for at := 0; at < dim; at++ {
				vector[at] += data[base+at]
			}
		}
		if live <= 0 {
			// 토큰이 하나도 없는 줄이다 (건너뛴 건). 벡터를 안 만든다.
			out[row] = nil
			continue
		}
		for at := range vector {
			vector[at] /= live
		}
		out[row] = normalize32(vector)
	}
	return out
}

// normalize32 는 길이를 1 로 맞춘다. 길이가 0 이면 그대로 둔다.
func normalize32(vector []float32) []float32 {
	total := 0.0
	for _, value := range vector {
		total += float64(value) * float64(value)
	}
	if total <= 0 {
		return vector
	}
	length := float32(math.Sqrt(total))
	for at := range vector {
		vector[at] /= length
	}
	return vector
}

// Cos 는 단위 벡터 둘의 코사인이다. 길이가 다르면 짧은 쪽까지만 센다.
func Cos(left, right []float32) float64 {
	size := len(left)
	if len(right) < size {
		size = len(right)
	}
	total := 0.0
	for at := 0; at < size; at++ {
		total += float64(left[at]) * float64(right[at])
	}
	return total
}
