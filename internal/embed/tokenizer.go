package embed

// 토크나이저와 그 바이너리 캐시 (결정 10).
//
// `tokenizer.json` 은 16.3MB 다. 이걸 JSON 으로 푸는 데만 **147ms** 가 들고,
// 그것이 준비 시간(약 440ms)의 3분의 1이다. 어휘 25만 줄은 한 번 풀면 두 번
// 풀 이유가 없으므로 우리 꼴로 구워 둔다 : `tokenizer.bin`.
//
// 구운 캐시는 **파생물**이다. 없으면 만들고, 원본이 바뀌었거나 판이 다르면
// 버리고 다시 만든다. 지워도 아무 일도 안 난다.

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/model/unigram"
	"github.com/sugarme/tokenizer/pretrained"
	"github.com/sugarme/tokenizer/util"
)

// cacheMagic 과 cacheVersion 은 구운 캐시 표식이다. 판이 다르면 다시 굽는다.
const (
	cacheMagic   = "MEMTOKB1"
	cacheVersion = uint32(1)
)

// maxVocab 은 망가진 캐시가 메모리를 통째로 먹지 않게 하는 고삐다.
const maxVocab = 1 << 21

// cachedTokenizer 는 구운 캐시가 담는 것이다 — 어휘를 뺀 설정 + 어휘.
type cachedTokenizer struct {
	shape []byte
	unkID int
	fuse  bool
	byteF bool
	vocab []unigram.TokenScore
}

// loadTokenizer 는 토크나이저를 만든다. 구운 캐시가 쓸 만하면 그것으로,
// 아니면 원본 json 을 풀고 **구워 둔다**.
func loadTokenizer(dir string) (*tokenizer.Tokenizer, error) {
	jsonPath := filepath.Join(dir, TokenizerFile)
	cachePath := filepath.Join(dir, CacheFile)
	if baked, err := readCache(cachePath, jsonPath); err == nil {
		if made, err := buildTokenizer(baked); err == nil {
			return made, nil
		}
		// 캐시가 있는데 조립이 안 되면 그 캐시는 못 쓰는 것이다. 지우고
		// 원본으로 간다 — 다음 번에 다시 굽는다.
		os.Remove(cachePath)
	}
	baked, err := parseTokenizerJSON(jsonPath)
	if err != nil {
		return nil, err
	}
	made, err := buildTokenizer(baked)
	if err != nil {
		return nil, err
	}
	// 굽다가 실패해도 토크나이저는 이미 있다. 다음 번이 조금 느릴 뿐이다.
	writeCache(cachePath, jsonPath, baked)
	return made, nil
}

// parseTokenizerJSON 은 원본을 풀어 캐시가 담을 것만 남긴다.
func parseTokenizerJSON(path string) (*cachedTokenizer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config := tokenizer.Config{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	params := util.NewParams(config.Model)
	if !params.Has("vocab") {
		return nil, errors.New("토크나이저에 어휘가 없다")
	}
	items, ok := params.Get("vocab").([]interface{})
	if !ok {
		return nil, errors.New("Unigram 어휘가 아니다")
	}
	baked := &cachedTokenizer{fuse: true, vocab: make([]unigram.TokenScore, 0, len(items))}
	for _, item := range items {
		pair, ok := item.([]interface{})
		if !ok || len(pair) != 2 {
			return nil, errors.New("어휘 줄 꼴이 안 맞는다")
		}
		piece, okPiece := pair[0].(string)
		score, okScore := pair[1].(float64)
		if !okPiece || !okScore {
			return nil, errors.New("어휘 줄 꼴이 안 맞는다")
		}
		baked.vocab = append(baked.vocab, unigram.TokenScore{Token: piece, Score: score})
	}
	if params.Has("unk_id") {
		if value, ok := params.Get("unk_id").(float64); ok {
			baked.unkID = int(value)
		}
	}
	if params.Has("byte_fallback") {
		baked.byteF, _ = params.Get("byte_fallback").(bool)
	}
	if params.Has("fuse_unk") {
		if value, ok := params.Get("fuse_unk").(bool); ok {
			baked.fuse = value
		}
	}
	// 어휘를 뺀 나머지는 그대로 다시 담는다. 정규화기·후처리기가 모델마다
	// 다르므로 우리가 뜻을 해석해 옮겨 적지 않는다.
	delete(config.Model, "vocab")
	shape, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	baked.shape = shape
	return baked, nil
}

// buildTokenizer 는 캐시가 담은 것으로 토크나이저를 조립한다. 원본을 풀 때와
// 같은 길을 지나야 결과가 같다 — 그래서 나머지 부품은 pretrained 가 만든다.
func buildTokenizer(baked *cachedTokenizer) (*tokenizer.Tokenizer, error) {
	config := tokenizer.Config{}
	if err := json.Unmarshal(baked.shape, &config); err != nil {
		return nil, err
	}
	options := util.NewParams(nil)
	options.Set("unk_id", baked.unkID)
	options.Set("byte_fallback", baked.byteF)
	options.Set("fuse_unk", baked.fuse)
	model, err := unigram.New(baked.vocab, options)
	if err != nil {
		return nil, err
	}
	made := tokenizer.NewTokenizer(model)
	normalize, err := pretrained.CreateNormalizer(config.Normalizer)
	if err != nil {
		return nil, err
	}
	made.WithNormalizer(normalize)
	pre, err := pretrained.CreatePreTokenizer(config.PreTokenizer)
	if err != nil {
		return nil, err
	}
	made.WithPreTokenizer(pre)
	post, err := pretrained.CreatePostProcessor(config.PostProcessor)
	if err != nil {
		return nil, err
	}
	made.WithPostProcessor(post)
	decode, err := pretrained.CreateDecoder(config.Decoder)
	if err != nil {
		return nil, err
	}
	made.WithDecoder(decode)
	special, plain := pretrained.CreateAddedTokens(config.AddedTokens)
	if len(special) > 0 {
		made.AddSpecialTokens(special)
	}
	if len(plain) > 0 {
		made.AddTokens(plain)
	}
	return made, nil
}

// writeCache 는 캐시를 굽는다. 원본의 크기·수정시각을 같이 적어 두어
// 원본이 바뀌면 캐시가 저절로 안 맞게 한다.
func writeCache(path, source string, baked *cachedTokenizer) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	file, err := os.Create(temporary)
	if err != nil {
		return err
	}
	out := bufio.NewWriterSize(file, 1<<20)
	head := make([]byte, 0, 64)
	head = append(head, cacheMagic...)
	head = binary.LittleEndian.AppendUint32(head, cacheVersion)
	head = binary.LittleEndian.AppendUint64(head, uint64(info.Size()))
	head = binary.LittleEndian.AppendUint64(head, uint64(info.ModTime().UnixNano()))
	head = binary.LittleEndian.AppendUint32(head, uint32(baked.unkID))
	head = binary.LittleEndian.AppendUint32(head, flagBits(baked.fuse, baked.byteF))
	head = binary.LittleEndian.AppendUint32(head, uint32(len(baked.shape)))
	head = binary.LittleEndian.AppendUint32(head, uint32(len(baked.vocab)))
	writeAll(out, head, baked.shape)
	line := make([]byte, 0, 64)
	for _, item := range baked.vocab {
		line = line[:0]
		line = binary.AppendUvarint(line, uint64(len(item.Token)))
		line = append(line, item.Token...)
		line = binary.LittleEndian.AppendUint32(line, math.Float32bits(float32(item.Score)))
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

func writeAll(out *bufio.Writer, parts ...[]byte) {
	for _, part := range parts {
		out.Write(part)
	}
}

func flagBits(fuse, byteFallback bool) uint32 {
	bits := uint32(0)
	if fuse {
		bits |= 1
	}
	if byteFallback {
		bits |= 2
	}
	return bits
}

// readCache 는 구운 캐시를 읽는다. 표식·판·원본 크기·수정시각 중 하나라도
// 안 맞으면 오류다 — 부르는 쪽이 그냥 원본으로 간다.
func readCache(path, source string) (*cachedTokenizer, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	const headSize = 8 + 4 + 8 + 8 + 4 + 4 + 4 + 4
	if len(raw) < headSize || string(raw[:8]) != cacheMagic {
		return nil, errors.New("토크나이저 캐시가 아니다")
	}
	at := 8
	if binary.LittleEndian.Uint32(raw[at:]) != cacheVersion {
		return nil, errors.New("토크나이저 캐시 판이 다르다")
	}
	at += 4
	if binary.LittleEndian.Uint64(raw[at:]) != uint64(info.Size()) {
		return nil, errors.New("토크나이저 원본이 바뀌었다")
	}
	at += 8
	if binary.LittleEndian.Uint64(raw[at:]) != uint64(info.ModTime().UnixNano()) {
		return nil, errors.New("토크나이저 원본이 바뀌었다")
	}
	at += 8
	baked := &cachedTokenizer{unkID: int(binary.LittleEndian.Uint32(raw[at:]))}
	at += 4
	bits := binary.LittleEndian.Uint32(raw[at:])
	baked.fuse, baked.byteF = bits&1 != 0, bits&2 != 0
	at += 4
	shapeLen := int(binary.LittleEndian.Uint32(raw[at:]))
	at += 4
	count := int(binary.LittleEndian.Uint32(raw[at:]))
	at += 4
	if shapeLen < 0 || at+shapeLen > len(raw) || count <= 0 || count > maxVocab {
		return nil, errors.New("토크나이저 캐시 머리말이 이상하다")
	}
	baked.shape = raw[at : at+shapeLen]
	at += shapeLen
	baked.vocab = make([]unigram.TokenScore, 0, count)
	for slot := 0; slot < count; slot++ {
		size, used := binary.Uvarint(raw[at:])
		if used <= 0 || at+used+int(size)+4 > len(raw) {
			return nil, errors.New("토크나이저 캐시 어휘가 잘렸다")
		}
		at += used
		piece := string(raw[at : at+int(size)])
		at += int(size)
		score := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[at:])))
		at += 4
		baked.vocab = append(baked.vocab, unigram.TokenScore{Token: piece, Score: score})
	}
	if at != len(raw) {
		return nil, fmt.Errorf("토크나이저 캐시 끝이 %d 바이트 남았다", len(raw)-at)
	}
	return baked, nil
}
