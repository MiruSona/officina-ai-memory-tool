# 임베딩 표 굽는 도구 (`tools/embed`)

`mem` 검색의 임베딩 갈래가 쓰는 표 `ko.bin` 을 만드는 자리다.
**팀원 기계에는 아무것도 안 깐다** — 여기서 한 번 구워서 파일 하나를 나눠 준다.

> **먼저 읽을 것** : 이 표를 실제로 재 본 결과는 `../../Docs/History/2026-08-23-임베딩실측.md` 에 있다.
> **결론은 「기본은 꺼 둔다」** 다. 표는 도는데 낱말 검색보다 나아지지 않았다.
> 도구는 다음 판이 다시 재 볼 수 있게 그대로 남긴다.

## 굽는 순서 (세 걸음)

### 0. 파이썬 자리 만들기 (한 번만)

```
python -m venv venv
venv/Scripts/python -m pip install numpy safetensors tokenizers huggingface_hub torch transformers
```

- **경로가 짧은 자리에 만든다.** 윈도우는 260자 넘는 경로에서 `torch` 설치가 깨진다(실제로 깨졌다).
- 선생 모델도 같이 내려받는다 (아래 표).

### 1. 범용 어휘 뽑기 — `generic_words.py`

```
venv/Scripts/python generic_words.py --tokenizer <선생>/tokenizer.json --out generic.txt
```

우리 기억 말뭉치에서 나오는 조각은 1만 개뿐인데 **질의는 저장소에 없는 말로 들어온다**
(`인덱싱`, `느려지나`). 선생 토크나이저의 어휘에서 순한글·순영문 조각을 꺼내 빈출 목록으로 쓴다.
어휘 번호가 빠를수록 자주 쓰는 조각이라 그것을 어림 빈도로 삼는다.

### 2. 어휘 만들기 — `vocab` (Go)

```
go run ./tools/embed/vocab -out vocab.tsv \
  -words testdata/corpus-words.txt -words generic.txt \
  <기억 폴더> testdata/quality/store
```

**쪼개기는 반드시 Go 가 한다.** 표의 어휘와 `internal/token` 의 조각이 글자 하나까지 같아야
검색이 표를 짚는다. 파이썬으로 다시 구현하면 언젠가 어긋난다.

### 3. 표 굽기 — `distill.py`

```
venv/Scripts/python tools/embed/distill.py \
  --vocab vocab.tsv --model <선생> --out ko.bin
```

model2vec 방식이다 — 조각 하나하나를 선생에 통과시켜 벡터를 얻고(학습 자료가 필요 없다),
PCA 로 128차원으로 줄이고, int8 로 양자화한다. 흔한 조각은 SIF 가중으로 눌러 둔다.

## 만들어진 표

| 항목 | 값 |
| --- | --- |
| 파일 | `testdata/model/ko.bin` · **2.65 MB** (설계 상한 10MB, 목표 8MB 안) |
| 어휘 | **19,973** 조각 (한글 바이그램 6,878 · 영문·숫자 13,095) |
| 차원 | 128 (선생 384 → PCA) |
| 셈 | int8 + 전역 계수 하나 |
| 굽는 시간 | CPU 로 약 4분 (조각 2만 개 통과) |

## 선생 모델과 라이선스

| 모델 | 크기 | 라이선스 | 골든셋 80건 홀로 recall@5 |
| --- | --- | --- | --- |
| **`intfloat/multilingual-e5-small`** ← **이걸 썼다** | 471MB · 118M | **MIT** | **0.746** |
| `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` | 471MB · 118M | **Apache-2.0** | 0.254 |

**둘 다 회사에서 공짜로 쓸 수 있다.** 그런데 품질이 세 배 가까이 갈렸다 —
e5 는 **찾기(retrieval)용으로 학습한 모델**이고 `query: `/`passage: ` 머리말을 붙여야 제 성능이 난다.
paraphrase 쪽은 **닮음(similarity)용**이라 "질문 → 답이 든 문서" 를 못 짚는다.
**다음에 선생을 고를 때 이 한 줄을 먼저 본다.**

- 굽는 코드(model2vec 방식)와 우리 코드는 우리 것이다. 내려받은 선생 모델·venv 는 저장소에 안 넣는다.
- CC BY-SA 인 fastText 압축본 계열은 재배포 조건이 붙어서 **쓰지 않는다** (설계 4-8).

## 파일

| 파일 | 하는 일 |
| --- | --- |
| `vocab/main.go` | 우리 토크나이저로 말뭉치를 쪼개 어휘표 `vocab.tsv` 를 낸다 |
| `generic_words.py` | 선생 토크나이저에서 범용 한글·영문 조각을 꺼낸다 |
| `distill.py` | 선생 통과 → PCA 128 → int8 → `ko.bin` |

`ko.bin` 을 읽는 쪽은 `internal/embed` 다. 꼴이 바뀌면 두 곳을 같이 고친다
(`distill.py` 머리말 주석 · `internal/embed/table.go`).
