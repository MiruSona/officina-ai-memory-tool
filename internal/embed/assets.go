package embed

// 설치 자리와 파일 목록 (결정 18).
//
// 자리는 v0.2 가 쓰던 기계 폴더 하나뿐이다 — `~/.aimemory/`. `%LOCALAPPDATA%`
// 같은 네 번째 경로를 만들지 않는다.
//
//	~/.aimemory/models/<모델이름>/model_int8.onnx   추론 그래프
//	~/.aimemory/models/<모델이름>/tokenizer.json    어휘
//	~/.aimemory/models/<모델이름>/config.json       차원·층수 (사람 확인용)
//	~/.aimemory/models/<모델이름>/tokenizer.bin     우리가 구운 캐시 (파생물)
//	~/.aimemory/bin/onnxruntime.dll                 런타임
//
// **받은 파일은 아래 SHA-256 과 맞아야만 쓴다.** 안 맞으면 지우고 낱말 모드다.

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

// DefaultModel 은 기본 모델 이름이다. Control 은 T1' 대조군이다 (결정 11).
const (
	DefaultModel = "multilingual-e5-small-ko-v2"
	ControlModel = "multilingual-e5-small"
)

// Dim 은 e5-small 계열의 벡터 차원이다. 다른 모델을 넣으면 헤더가 안 맞아
// vectors.bin 이 통째로 다시 만들어진다.
const Dim = 384

// 파일 이름. 모델 폴더 안은 어느 모델이든 이 이름으로 맞춘다 — 받는 곳마다
// 이름이 다르면 경로 계산이 모델마다 갈라진다.
const (
	ModelFile     = "model_int8.onnx"
	TokenizerFile = "tokenizer.json"
	ConfigFile    = "config.json"
	CacheFile     = "tokenizer.bin"
	DLLFile       = "onnxruntime.dll"
)

// Asset 은 설치가 놓아야 할 파일 하나다.
type Asset struct {
	// Name 은 표에 찍는 이름이다.
	Name string
	// Rel 은 꾸러미 안 상대 경로다. `--bundle` 이 이 자리에서 찾는다.
	Rel string
	// SHA 는 exe 안에 박아 둔 해시다. 빈 글이면 검사를 못 한다.
	SHA string
	// Bytes 는 사람에게 보여줄 크기다 (참고값).
	Bytes int64
}

// ModelAssets 는 모델 하나가 필요로 하는 파일이다.
func ModelAssets(model string) []Asset {
	switch model {
	case ControlModel:
		return []Asset{
			{Name: ModelFile, Rel: "models/" + ControlModel + "/" + ModelFile,
				SHA: "dd476dd0c2514e9b9be83aeb3853fac0763e0bdf4a71645407587d77c48a2d88", Bytes: 118346824},
			{Name: TokenizerFile, Rel: "models/" + ControlModel + "/" + TokenizerFile,
				SHA: "0b44a9d7b51c3c62626640cda0e2c2f70fdacdc25bbbd68038369d14ebdf4c39", Bytes: 17082730},
			{Name: ConfigFile, Rel: "models/" + ControlModel + "/" + ConfigFile,
				SHA: "69137736cab8b8903a07fe8afaafdda25aac55415a12a55d1bffa9f581abf959", Bytes: 655},
		}
	default:
		return []Asset{
			{Name: ModelFile, Rel: "models/" + DefaultModel + "/" + ModelFile,
				SHA: "fd72d15365440c62d41450c80c648c560bf01cb75d72a4d00fa365917e0d4f8c", Bytes: 118071048},
			{Name: TokenizerFile, Rel: "models/" + DefaultModel + "/" + TokenizerFile,
				SHA: "cd98e5698b201ba914efb8c18b6709fa8735ab71dcad8d2b431e52e8bf68d932", Bytes: 17082800},
			{Name: ConfigFile, Rel: "models/" + DefaultModel + "/" + ConfigFile,
				SHA: "0191eaed39cf8e12be8ddab1890c6a2012e345e7c14e15a1a22ba2375bc62aff", Bytes: 686},
		}
	}
}

// RuntimeAsset 은 onnxruntime.dll 이다 (win-x64 1.29.0).
func RuntimeAsset() Asset {
	return Asset{Name: DLLFile, Rel: "bin/" + DLLFile,
		SHA: "69d8e6d3879a3b4001cdc74c8ed9ccc7e7f799a5b847059738323404519ec471", Bytes: 16149344}
}

// 내려받는 자리. **모델 int8 은 아직 아무 데도 안 올라가 있다** — ko-v2 는
// HF 에 ONNX 가 없어 우리가 export 했다. 배포 때 GitHub Release 에 올리고 나면
// 이 상수가 그 주소를 가리킨다. 그때까지는 `install --bundle <폴더>` 로 놓는다.
const (
	// ReleaseBase 는 모델·토크나이저를 올릴 자리다 (배포 시 채운다).
	ReleaseBase = "https://github.com/mirusona/officina-ai-memory-tool/releases/download/models-v1/"
	// RuntimeURL 은 onnxruntime win-x64 1.29.0 zip 이다. 안에서 lib/onnxruntime.dll
	// 하나만 꺼낸다.
	RuntimeURL = "https://github.com/microsoft/onnxruntime/releases/download/v1.29.0/onnxruntime-win-x64-1.29.0.zip"
	// RuntimeInZip 은 그 zip 안의 자리다.
	RuntimeInZip = "onnxruntime-win-x64-1.29.0/lib/onnxruntime.dll"
)

// Root 는 기계별 폴더다 — `~/.aimemory`. 집 폴더를 못 찾으면 빈 글이고,
// 그러면 임베딩은 통째로 꺼진다.
func Root() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".aimemory")
}

// ModelDir 은 모델 하나가 사는 자리다.
func ModelDir(model string) string {
	root := Root()
	if root == "" {
		return ""
	}
	if model == "" {
		model = DefaultModel
	}
	return filepath.Join(root, "models", model)
}

// DLLPath 는 런타임 자리다. **여기서 절대경로를 만들어 넘기는 것이 규칙이다** —
// 이름만 넘기면 System32 의 옛 onnxruntime.dll(API v10)이 잡혀 죽는다 (0실측).
func DLLPath() string {
	root := Root()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "bin", DLLFile)
}

// FileSHA 는 파일 하나의 SHA-256 이다. 113MB 를 통째로 메모리에 올리지 않는다.
func FileSHA(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// SHAOK 는 파일이 박아 둔 해시와 맞는지다. 박아 둔 해시가 없으면 파일이
// 있기만 하면 참이다 — 검사할 것이 없는데 막으면 아무것도 못 쓴다.
func SHAOK(path, want string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if want == "" {
		return true
	}
	got, err := FileSHA(path)
	return err == nil && got == want
}

// SizeOK 는 파일이 있고 크기가 박아 둔 값과 같은지다. **싼 검사**다 —
// 해시는 install 때 한 번 한다 (113MB 해시가 0.2초라 검색마다 하면 안 된다).
func SizeOK(path string, want int64) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return want <= 0 || info.Size() == want
}

// ModelVersion 은 모델 파일의 판 표식이다 — 박아 둔 SHA 앞 12자.
// vectors.bin 머리말에 들어가고, 모델을 바꾸면 이 값이 바뀌어 벡터가 통째로
// 다시 만들어진다 (결정 13).
func ModelVersion(model string) string {
	for _, asset := range ModelAssets(model) {
		if asset.Name == ModelFile && len(asset.SHA) >= 12 {
			return asset.SHA[:12]
		}
	}
	return "unknown"
}
