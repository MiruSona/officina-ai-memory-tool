# 함께 배포하는 남의 코드 (THIRD-PARTY)

`mem` 실행 파일 하나에는 아래 라이브러리가 들어 있다. **전부 허용형(permissive)**이라
저작권 고지만 같이 배포하면 사내·상용 배포에 걸리는 것이 없다. **GPL · AGPL · LGPL · MPL 은 하나도 없다.**

- 빌드는 `CGO_ENABLED=0` 이라 시스템 SQLite 도, 다른 어떤 C 라이브러리도 링크하지 않는다.
- 아래 목록은 `go list -deps ./cmd/mem` 이 준 **실제로 실행 파일에 들어가는 것**이다.
- 라이선스 전문은 각 모듈의 `LICENSE` 파일에 있다 (`go env GOMODCACHE` 아래).
- 근거 : `Docs/History/2026-08-23-보안연동시험.md` T12 (모듈 캐시의 LICENSE 를 직접 열어 확인).

## 목록

| 모듈 | 판 | 라이선스 | 저작권 |
| --- | --- | --- | --- |
| `gopkg.in/yaml.v3` | v3.0.1 | MIT + Apache-2.0 (파일마다 갈림) | Copyright (c) 2006-2011 Kirill Simonov · Copyright (c) 2011-2019 Canonical Ltd |
| `golang.org/x/text` | v0.41.0 | BSD-3-Clause | Copyright 2009 The Go Authors |
| `golang.org/x/sys` | v0.47.0 | BSD-3-Clause | Copyright 2009 The Go Authors |
| `modernc.org/sqlite` | v1.57.0 | BSD-3-Clause | Copyright (c) 2017 The Sqlite Authors |
| `modernc.org/libc` | v1.74.4 | BSD-3-Clause | Copyright (c) 2017 The Libc Authors |
| `modernc.org/mathutil` | v1.7.1 | BSD-3-Clause | Copyright (c) 2014 The mathutil Authors |
| `modernc.org/memory` | v1.11.0 | BSD-3-Clause | Copyright (c) 2017 The Memory Authors |
| `github.com/dustin/go-humanize` | v1.0.1 | MIT | Copyright (c) 2005-2008 Dustin Sallings |
| `github.com/mattn/go-isatty` | v0.0.24 | MIT (Expat) | Copyright (c) Yasuhiro MATSUMOTO |
| `github.com/ncruces/go-strftime` | v1.0.0 | MIT | Copyright (c) 2022 Nuno Cruces |
| `github.com/remyoudompheng/bigfft` | 2023-01-29 | BSD-3-Clause | Copyright (c) 2012 The Go Authors |
| `github.com/getcharzp/onnxruntime_purego` | v1.24.0 | MIT | Copyright (c) 2026 getcharzp |
| `github.com/ebitengine/purego` | v0.9.0 | Apache-2.0 | Copyright ebitengine authors |
| `github.com/sugarme/tokenizer` | v0.3.0 | Apache-2.0 | Copyright sugarme |
| `github.com/sugarme/regexpset` | 2020-09-20 | Apache-2.0 | Copyright sugarme |
| `github.com/emirpasic/gods` | v1.18.1 | BSD-2-Clause | Copyright (c) 2015 Emir Pasic |
| `github.com/patrickmn/go-cache` | v2.1.0 | MIT | Copyright (c) 2012-2017 Patrick Mylund Nielsen |
| `github.com/rivo/uniseg` | v0.4.7 | MIT | Copyright (c) 2019 Oliver Kuederle |
| `github.com/mitchellh/colorstring` | 2019-02-13 | MIT | Copyright (c) 2014 Mitchell Hashimoto |
| `github.com/schollz/progressbar/v2` | v2.15.0 | MIT | Copyright (c) 2017 Zack |
| `github.com/up-zero/gotool` | 2026-01-05 | MIT | Copyright (c) 2023 up-zero |

`go.mod` 에는 `github.com/google/uuid` v1.6.0 (BSD-3-Clause · Copyright (c) 2009,2014 Google Inc.) 도
적혀 있지만 **`CGO_ENABLED=0` 빌드에는 안 들어간다.** 소스를 받아 다른 방식으로 빌드하면 들어갈 수 있어 같이 적어 둔다.

**v0.3 에서 더한 것 (의미 검색)** : 위 표의 아래 열한 줄이다. `getcharzp/onnxruntime_purego` 가
ONNX Runtime 을 부르고(그 아래 `ebitengine/purego` 가 DLL 을 연다), `sugarme/tokenizer` 가 XLM-R
어휘를 다룬다. 나머지 여덟은 그 둘이 끌고 오는 것이다. **전부 허용형이고 cgo 는 하나도 안 쓴다.**

## 같이 놓는 파일 (exe 안이 아니라 옆에 놓는 것)

`mem install --apply` 가 `~/.aimemory/` 에 놓는 것은 **실행 파일에 안 들어간다.** 따로 적는다.

| 무엇 | 어디서 | 라이선스 | 전문 자리 |
| --- | --- | --- | --- |
| `onnxruntime.dll` win-x64 1.29.0 | Microsoft ONNX Runtime GitHub Release | **MIT** | 꾸러미 `bin/LICENSE-onnxruntime.txt` |
| `dragonkue/multilingual-e5-small-ko-v2` int8 (기본 모델) | Hugging Face → 우리가 ONNX 로 export | **Apache-2.0** | 꾸러미 `models/multilingual-e5-small-ko-v2/LICENSE.txt` |
| `intfloat/multilingual-e5-small` int8 (대조군) | Hugging Face | **MIT** | 꾸러미 `models/multilingual-e5-small/LICENSE.txt` |
| 두 모델의 `tokenizer.json` | 같은 저장소 | 모델과 같음 | 위와 같은 자리 |

`--bundle <폴더>` 로 놓을 때도, 내려받아 놓을 때도 **LICENSE 파일을 같은 폴더에 같이 둔다.**

## 안에 또 들어 있는 것

| 어디 | 무엇 | 라이선스 |
| --- | --- | --- |
| `modernc.org/sqlite` | SQLite 원본 (`LICENSE-SQLITE`) | **퍼블릭 도메인** |
| `modernc.org/sqlite` | sqlite-vec (`LICENSE-SQLITE_VEC`) | MIT |
| `modernc.org/libc` | musl · tre · Bionic 등 (`LICENSE-3RD-PARTY.md`) | MIT · BSD-2-Clause |
| `modernc.org/memory` | Go 표준 라이브러리 조각 · mmap-go · 로고 | BSD-3 · BSD-2 · 위키미디어 그림 |

## 이 목록을 고칠 때

의존을 더하거나 판을 올리면 **이 파일도 같이 고친다.** 새 목록은 이렇게 뽑는다.

```sh
CGO_ENABLED=0 go list -deps -f '{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}' ./cmd/mem | sort -u
```

새 모듈이 나오면 `go env GOMODCACHE` 아래에서 그 모듈의 `LICENSE` 를 **직접 열어 읽고**
허용형인지 확인한 다음 표에 넣는다. GPL·AGPL·LGPL·MPL 이 하나라도 있으면 그 의존은 쓰지 않는다.
