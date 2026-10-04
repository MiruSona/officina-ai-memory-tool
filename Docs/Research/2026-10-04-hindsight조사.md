# hindsight 조사 — 우리 `mem` 에 가져올 것이 있나

> 조사일 2026-10-04 · 조사자 Opus 서브에이전트 · 코드는 안 고쳤다.
> 표시 : **[문서]** 바깥 문서·논문에서 읽음 · **[코드]** hindsight 소스(2026-10-02 커밋 `f7dd3f4`)나 우리 소스에서 직접 봄 · **[추정]** 내 판단.

## 0. 결론 요약

1. hindsight 는 **서버형 기억 엔진**이다 — Python + Postgres(pgvector) + 넣을 때마다 LLM. 우리 전제(단일 exe · 서버 없음 · LLM 0 · Markdown 원본)와 뿌리부터 다르다. **통째 교체는 안 권한다.**
2. 벤치 수치(LongMemEval 91.4~94.6%)는 **전부 자기 측정**이다. 「독립 재현」이라는 곳은 논문 공저자 기관이고, 리더보드(AMB)도 Vectorize 가 만들었다. 값의 크기는 참고만 한다.
3. 논문의 「opinion + 확신도(confidence) 갱신」은 **2026-04 에 코드에서 지워졌다** [코드]. 지금은 world · experience · observation · mental model 넷이다.
4. 가장 값진 배움은 LLM 이 아니라 **검색 구조**다 — 「의미·낱말·그래프·시간 네 길이 **저마다 후보를 데려오고** RRF 로 섞은 뒤, 재순위 모델이 정밀도를 되찾는다」.
5. 우리 의미 검색은 **후보를 새로 데려오지 않는다** [코드 `internal/search/rerank.go` `vectorsInto`]. 그런데 G1 을 못 맞힌 12건 중 11건이 **「순위 없음」(후보에 아예 없음)**이다. 이 둘이 딱 맞물린다.
6. 추천 1순위 : **의미 길과 링크 1-hop 을 「느슨 칸」 후보 길로 열기** (strict 판정은 안 건드려 G2 를 지킨다). 2순위 : **덮은 기억(`superseded_by`) 따라가기**. 3순위 : **다국어 cross-encoder 재순위**(선택 설치).
7. 4순위 : **observation 식 「모음 기억」을 로컬 LLM 으로 만들되 `--hold` 로만 넣기** — 「LLM 호출 0」 결정을 뒤집어야 해서 사용자 판단 몫이다.
8. hindsight 의 mental model **delta 연산**(LLM 은 「어느 블록을 어떻게」 JSON 만 내고, 코드가 적용)은 우리 큰 원칙과 같은 생각이라 4순위를 할 때 그대로 빌린다.
9. 먼저 해 볼 작은 실험 : **시험용 뒷문 환경변수 둘로 「의미 후보 길」·「1-hop 확장」을 켜고 v2 80건·손 30건 골든셋을 다시 잰다** (S, 약 2~3시간).

---

## 1. hindsight 는 무엇인가

### 1-1. 한 장 요약

| 항목 | 내용 | 출처 |
| --- | --- | --- |
| 목표 | 「시간이 갈수록 배우는 에이전트 기억」. RAG·지식 그래프의 약점을 없앤다고 주장 | [문서] [README](https://github.com/vectorize-io/hindsight) |
| 만든 곳 | Vectorize.io. 논문 공저자에 Virginia Tech·The Washington Post | [문서] [arXiv 2512.12818](https://arxiv.org/html/2512.12818v1) |
| 라이선스 | MIT | [코드] GitHub API |
| 언어 | Python 이 대부분(약 23MB). TypeScript(UI·SDK)·Rust·Go(SDK) | [코드] GitHub API `languages` |
| 구성 | REST API 서버(8888) · 웹 UI(9999) · **MCP 끝점 내장**(`/mcp/{bank_id}/`) · SDK(Python·Node·Go) · CLI | [문서] README · `mcp-server.md` |
| 저장소 | **PostgreSQL + pgvector** (또는 Oracle 23ai). 개발용으로 임베디드 Postgres `pg0` | [문서] `installation.md` |
| LLM | **넣을 때마다 필수.** 25종 넘는 제공자(OpenAI·Anthropic·Gemini·Groq·Ollama·LM Studio·llama.cpp…). 기본값 `openai` / 대체 `gpt-4o-mini` | [문서] README · [코드] `config.py:1091,1126` |
| 임베딩 · 재순위 기본 | `BAAI/bge-small-en-v1.5` · `cross-encoder/ms-marco-MiniLM-L-6-v2` — **둘 다 영어 전용 모델** | [코드] `config.py:1212,1275` |
| 설치 | Docker(권장) · docker compose · `pip install hindsight-api` · Helm · 클라우드 · `hindsight-all`(서버 없이 Python 안에 묻기) | [문서] README |
| Windows | 지원. pg0 로 바로 돈다고 적혀 있다. Claude Code 플러그인의 상태 파일 락은 Windows 에서 **락 없이 진행**으로 떨어진다 | [문서] `installation.md` · [코드] `claude-code/scripts/lib/state.py:13-123` |
| 활동성 (2026-10-04) | 별 **45,359** · 포크 5,903 · 열린 이슈+PR 321 · 만든 날 2025-10-30 · 마지막 푸시 2026-10-02 · 최근 릴리스 v0.10.2(09-29) · v0.10.1(09-21) · v0.10.0(09-14) | [코드] `gh api repos/vectorize-io/hindsight` |
| 붙는 곳 | Claude Code 플러그인 · 코딩 에이전트 20여 종 공용 패키지 · LangGraph·CrewAI 등 약 55개 연동 | [코드] `hindsight-integrations/` 폴더 목록 |

### 1-2. 운영 부담 — 3년차 눈으로

| 부담 | 크기 | 근거 |
| --- | --- | --- |
| 상주 프로세스 | API 서버 + Postgres + (선택) 워커 · UI. Claude Code 플러그인은 `hindsight-embed` 데몬을 띄우고 끈다 | [문서] `claude-code/README.md` |
| 한글 낱말 검색 | 기본 `native` BM25 는 Postgres 영어 사전이라 **한·중·일은 토큰을 아예 못 나눈다.** `pgroonga`·`pg_search` 확장(도커 예제)으로 바꿔야 한다 | [문서] `multilingual.mdx` 113~130행 |
| 로컬 LLM | 기본 동시 32요청. 로컬 서버면 `LLM_MAX_CONCURRENT=2`, 공용이면 더 낮추라고 문서가 스스로 권한다 | [문서] `performance.md` 「Tuning for Local」 |
| 비용 사고 | 「플러그인이 만든 bank 가 비싼 기본값(관찰·자동 consolidation 켜짐)을 물려받는다」 이슈 | [문서] [#4725](https://github.com/vectorize-io/hindsight/issues/4725) |
| 토큰 낭비 | 「같은 911자 문맥 블록이 매 턴 주입돼 토큰을 불린다」(열림, 10-01) · 「앞에 붙이는 주입이 프롬프트 캐시를 깬다」 | [문서] [#5026](https://github.com/vectorize-io/hindsight/issues/5026) · [#3061](https://github.com/vectorize-io/hindsight/issues/3061) |

---

## 2. 기억 모델

### 2-1. 기억의 종류 — 논문과 지금 코드가 다르다

| 이름 | 뜻 | 논문(2025-12) | 지금 코드(2026-10) |
| --- | --- | --- | --- |
| **world** | 바깥 세상에 대한 사실 (「Alice 는 Google 에서 일한다」) | 있음 | 있음 |
| **experience** | 이 bank 의 에이전트 자신이 한 일, 1인칭 (「내가 Alice 에게 Python 을 권했다」). 문법이 아니라 **누가 말했나**로 가른다 | 있음 (ℬ) | 있음 |
| **opinion** | 에이전트의 주관 판단 + **확신도 c∈[0,1]**. 강화 +α · 약화 −α · 모순 −2α | 있음 | **지워짐.** 마이그레이션 `remove_opinion_fact_type`(2026-04-02)이 `confidence_score` 열까지 뺐다 [코드] |
| **observation** | 여러 사실을 묶은 「근거 달린 믿음」. 근거 기억 목록 · `proof_count` · 이력을 가진다 | 개체 요약(𝒮) | **consolidation 이 자동으로 만든다** |
| **mental model** | 사람이 정한 「상설 질문」의 답 문서. 뒤에서 다시 쓴다 | 없음 | 2026-01 「새 지식 구조」로 생김 [코드 `p1k2l3m4n5o6`] |
| directive | 반드시 지킬 규칙 (reflect 때만) | 없음 | 있음 |

출처 : [문서] [논문 HTML](https://arxiv.org/html/2512.12818v1) · `retain.md` · `observations.mdx` · `mental-models.mdx` · [코드] `alembic/versions/g2h3i4j5k6l7_remove_opinion_fact_type.py`

**「믿음 갱신」은 지금 확신도 숫자가 아니라 observation 글을 LLM 이 다시 쓰는 방식이다.**
예 : 「React 를 좋아한다」 → 3주 뒤 「Vue 로 바꿨다」가 오면 「예전엔 React 를 좋아했지만 지금은 Vue 를 쓴다」로 고쳐 쓴다. 원래 사실은 지우지 않는다 [문서 `observations.mdx`].

### 2-2. 저장 단위와 연결

| 무엇 | 내용 |
| --- | --- |
| 단위 | 대화·문서를 3,000자 조각(`RETAIN_CHUNK_SIZE`)으로 잘라 LLM 이 **사실(fact)** 여러 개를 뽑는다. 사실마다 what·when·where·who·why·fact_type·`occurred_start/end`·인과 관계 [코드 `retain/fact_extraction.py:161-304`, `config.py:1629`] |
| 시간 두 축 | **언제 일어났나**(occurred) / **언제 알았나**(mentioned) [문서 `retain.md`] |
| 개체 | 사람·조직·장소·제품·개념. **이름 닮음 + 같이 나온 개체 + 시간 가까움**으로 같은 개체로 합친다. 「통제 어휘 라벨」(`key:value`)은 정확 일치만 [문서 `retain.md`] |
| 링크 넷 | 개체(w=1.0) · 시간(w=exp(−Δt/σ)) · 의미(코사인 ≥ θ) · 인과(causes·caused_by·enables·prevents) [문서 논문 2절] |
| 원본 | 넣은 원문(chunk)도 남긴다. 검색 때 `include_chunks` 로 같이 받는다 |

---

## 3. 동작 셋 — retain · recall · reflect

### 3-1. retain (넣기)

| 단계 | 하는 일 | LLM |
| --- | --- | --- |
| ① 조각 내기 | 3,000자 단위 | 0 |
| ② 사실 뽑기 | 조각마다 구조화 출력(JSON) 한 번. 모드 `concise`(기본)·`verbose`·`verbatim`·`custom`·**`chunks`(LLM 없음, 그대로 저장)** | **조각당 1회** |
| ③ 개체 합치기 · 링크 · 임베딩 | 규칙 + 임베딩 | 0 [코드 — LLM 호출 파일 목록에 `entity_processing.py`·`link_creation.py` 없음] |
| ④ consolidation (뒤에서) | 새 사실을 기존 observation 과 견줘 만들기·고치기. 기본 **사실 8개당 1회** | **배치당 1회** [코드 `config.py:1745`] |
| ⑤ 닮은 observation 합치기 | 코사인 ≥ 0.97 이면 LLM 이 「합칠까 둘까」 판정 | 짝당 1회 |
| ⑥ mental model 다시 쓰기 | 범위 안에 새 기억이 들어왔을 때만 | 모델당 1회+ |

- 지연 : retain 배치당 **0.5~2초**(병목은 LLM) [문서 `performance.md`]. 논문은 비용·지연 수치를 안 냈다 [문서 논문].
- 권장 모델 : `gpt-oss-20b` — 「똑똑한 모델이 필요 없다」 [문서 `performance.md`].
- 같은 글을 넣어도 사실이 나올 때와 안 나올 때가 있다고 문서가 스스로 적는다 (「extraction is not fully deterministic」) [문서 `retain.md`].

### 3-2. recall (찾기) — 논문 이름 TEMPR

1. **네 길을 함께 돌린다** — 의미(pgvector HNSW 로 전체에서 최근접) · 낱말(BM25) · 그래프(개체·의미·인과 링크로 퍼짐) · 시간(질의 날짜 → 기간 안에서 의미 순).
2. **RRF(k=60)** 로 섞는다. 네 길 무게는 같다.
3. 상위 300 을 **cross-encoder 로 재순위**한다.
4. **최근성(±10%) × 시간 근접(±10%) × 근거 수(±5%)** 를 곱한다.
5. **토큰 예산**(기본 4,096)까지 위에서부터 채운다.

| 요점 | 내용 | 출처 |
| --- | --- | --- |
| **네 길이 저마다 후보를 데려온다** | 의미 길은 낱말 결과를 다시 매기는 게 아니라 **전체에서** 따로 찾는다 | [문서 `retrieval.md`] |
| RRF | k=60, 점수 아닌 **순위**로 섞어 척도가 다른 길을 맞춘다 | 같음 |
| 재순위 | cross-encoder 가 (질의, 기억) 쌍을 같이 읽어 점수. 32쌍씩 묶음. 없으면 RRF 순위로 대신 | 같음 |
| 가산은 **곱 · 상한 있음** | 최근성 `clamp(1−일수/365, 0.1, 1)` → ±10% · 합쳐도 +27% / −23% 안쪽 | 같음 |
| 시간 길 | 질의 날짜를 규칙(`dateparser`)으로 읽어 기간을 정하고, **기간 안에서는 최근 순이 아니라 의미 순**, 기간을 칸으로 나눠 고르게 뽑는다 | [문서] · [코드 `query_analyzer.py:277`] |
| 깊이 손잡이 | `budget` low/mid/high = 길마다 후보 100/300/1,000 | [문서] |
| 토큰 예산 | 너무 긴 결과는 **건너뛰고 다음 것을 계속 채운다.** 하나도 안 들어가면 1위는 통째로 준다 | [문서] |
| LLM | **0회** (시간 해석도 규칙). 지연 100~600ms, 병목은 CPU 재순위 | [문서 `performance.md`] |

알려진 약점 : 「cross-encoder 가 그래프로 데려온 결과를 깎는다(의미는 안 닮았으니까)」 — 열린 이슈 [#2841](https://github.com/vectorize-io/hindsight/issues/2841).

### 3-3. reflect (되새김)

| 요점 | 내용 |
| --- | --- |
| 무엇 | 질문에 **에이전트 루프**로 답한다. 도구 : mental model 찾기·읽기 → observation 찾기 → recall(원 사실) → expand → done |
| 순서 | mental model → observation → 원 사실. 낡은(새 기억이 아직 안 섞인) 것은 아래 층으로 확인 |
| 성향 | skepticism · literalism · empathy 1~5 + mission 글 + directive(지킬 규칙) |
| 한도 | 최대 10회 반복. 인용은 실제로 가져온 id 만 |
| 비용 | 질문마다 LLM 여러 번. 0.8~3초 |

출처 : [문서 `reflect.mdx` · `performance.md`]

**mental model 의 delta 갱신** [코드 `engine/reflect/delta_ops.py` 머리 주석] :
LLM 은 「섹션 id·블록 id 를 지목한 연산 목록」만 낸다. 코드가 꼴을 검사하고(틀리면 통째로 거절하고 다시 묻는다),
없는 id 를 가리키는 연산은 버리고, **안 건드린 블록은 글자 그대로 복사**한다.
「연산 0개 = 문서 그대로」라 고칠 때마다 글이 조금씩 변하는 현상(drift)이 구조적으로 없다. **우리 큰 원칙(AI 는 짧은 JSON · 결과물은 코드)과 같은 생각이다.**

---

## 4. 성능 주장과 믿을 만한가

### 4-1. 논문 표 [문서 [arXiv 2512.12818](https://arxiv.org/html/2512.12818v1)]

| LongMemEval-S (500문항) | 전체 | multi-session | temporal | knowledge-update |
| --- | --- | --- | --- | --- |
| 전체 문맥 GPT-4o | 60.2 | 44.3 | 45.1 | 78.2 |
| Zep (GPT-4o) | 71.2 | 57.9 | 62.4 | 83.3 |
| Supermemory (Gemini-3) | 85.2 | 76.7 | 82.0 | 89.7 |
| **Hindsight (OSS-20B)** | **83.6** | 79.7 | 79.7 | 84.6 |
| **Hindsight (Gemini-3)** | **91.4** | 87.2 | 91.0 | 94.9 |

| LoCoMo | 전체 |
| --- | --- |
| Backboard | **90.00** |
| Memobase | 75.78 |
| Zep | 75.14 |
| Mem0 | 66.88 |
| **Hindsight (Gemini-3)** | 89.61 |

- 채점 LLM : GPT-OSS-120B, 온도 0 [문서 논문].
- **초록은 「가장 강한 이전 시스템 75.78%」라고 쓰지만 같은 논문 표에 Backboard 90.00% 가 있다.** 표 안에서 앞뒤가 안 맞는다 [문서 논문 · 내 대조].

### 4-2. 그 뒤 수치 — 매체마다 다르다

| 어디 | 값 | 출처 |
| --- | --- | --- |
| 자사 벤치 사이트 | LongMemEval-S **94.6** · LoCoMo10 92 · BEAM 10M 64.1 등 여덟 개 | [문서] [benchmarks.hindsight.vectorize.io](https://benchmarks.hindsight.vectorize.io) |
| 자사 벤치 글 | 다섯 갈래 표에 「Multi-Hop Reasoning」 — **LongMemEval 에 없는 갈래 이름**이다 | [문서] [vectorize.io/benchmarks](https://vectorize.io/benchmarks) |
| Mem0 값 | 같은 회사 글에서 **49.0** 과 **67.6** 두 값 | [문서] [블로그 05-21](https://hindsight.vectorize.io/blog/2026/05/21/agent-memory-consolidation) · 검색 결과 요약 |

### 4-3. 누가 쟀나

| 물음 | 답 |
| --- | --- |
| 제3자 재현 | **확인 못 함.** 「독립 재현」으로 든 Virginia Tech·Washington Post 는 **논문 공저자 기관**이다 [문서 [akitaonrails 조사](https://github.com/akitaonrails/ai-memory/blob/main/docs/research-hindsight.md)] |
| 리더보드 | Agent Memory Benchmark(AMB)는 **Vectorize 가 만든** 공개 하네스다. 하네스·프롬프트는 공개라 재현은 가능한 꼴 [문서] [agent-memory-benchmark](https://github.com/vectorize-io/agent-memory-benchmark) |
| 동료 심사 | 프리프린트(v1, 2025-12-14). 성향(disposition) 절제 실험 없음 [문서 akitaonrails 조사] |
| 우리와 견줄 수 있나 | **없다.** 저쪽은 「답의 정확도」(LLM 이 답을 쓰고 LLM 이 채점), 우리는 「검색 r@5·MRR」. 과제도 대화형 개인 기억 ↔ 개발 결정 기록으로 다르다 [추정] |

**판단** [추정] : 「네 길 + 재순위」 구조가 대화 기억 문항에서 강하다는 방향은 믿을 만하다(구조 자체가 표준 기법이다). 수치의 크기는 자기 측정이라 낮춰 읽는다.

### 4-4. 경쟁 셋과 다른 점 (짧게)

| | 넣을 때 LLM | 저장 | 모순 처리 | 우리와 거리 |
| --- | --- | --- | --- | --- |
| **hindsight** | 조각당 1회 + 묶음 | Postgres | observation 글을 다시 씀 | 서버·LLM |
| mem0 | 매번 | 벡터+그래프+KV | 추출 때 덮기 | 서버·LLM |
| Zep/Graphiti | 매번 | 시간 지식 그래프 | `valid_at`·`invalid_at` 구간 | 서버·LLM |
| Letta | 편집마다 LLM 턴 | 에이전트 런타임 | 에이전트가 스스로 고침 | 런타임 |
| **우리 mem** | **0** | **md 파일 + SQLite 파생** | `superseded_by`·`invalid_at` + 사람 | — |

출처 : [문서] 우리 `2026-08-22-설계검토-E-외부동향.md` 41~44행 · [agentmarketcap 비교](https://agentmarketcap.ai/blog/2026/04/11/agent-memory-architecture-production-2026)

---

## 5. 우리 `mem` 의 지금 모습 (관련된 것만)

| 갈래 | 지금 | 근거 |
| --- | --- | --- |
| 원본 | `Memory/store/**/*.md` 1건 1파일 · `index.db`·`vectors.bin` 은 파생물 | [문서] `Docs/Guide/설계개요.md` |
| LLM | **0.** 「사용자 확정」 (mem `20260822-b3cb1d6a`) | [문서] 설계개요 4절 |
| 기억 종류 | 일곱 (`decision`·`caution`·`issue`·`todo`·`history`·`howto`·`fact`) + 종류마다 반감기·관문·본문 꼴 | [문서] `명령명세.md` 124~213행 |
| 검색 | 완화 사다리 7칸 · RRF 10신호(k=10) · MMR · 1-hop **가산만** · 묶음 번호 · `--budget` · 시간 표현(`어제`·`지난주`) | [문서] `명령명세.md` 5·6절 |
| 의미 검색 | ONNX 선택 설치. **낱말 후보 상위 200건만 다시 매긴다 — 후보를 새로 안 데려온다** | [코드] `internal/search/rerank.go:170-197` 주석 「그래야 abstain 관문이 낱말 쪽 결과 그대로 판정된다」 |
| 링크 | 사람 `links` + 기계 `auto_links`(태그·같은 근거·같은 scope 식별자 통) | [코드] `internal/link/link.go:47-299` |
| 덮기 | `superseded_by`+`invalid_at` 한 짝 · 무효 기억은 신뢰 ×0.5 · 무효화 전파(`review --kind basis`) | [문서] `기억파일규격.md` · 진행상황 4-0 |
| 감쇠 | `0.3 + 0.7·0.5^(나이/반감기)` — history 90일이면 1년 뒤 약 ×0.34 | [문서] `명령명세.md` 6절 |

**지금 약점 — 이번 조사가 겨누는 자리** (`Docs/History/2026-08-24-최종측정.md` 2-4~2-5) :

| 약점 | 값 | 성격 |
| --- | --- | --- |
| G1 r@5 | 의미 0.761 (자 0.85) | |
| `multisession` | **0.438** (16건) | 답이 여러 기억에 흩어짐 |
| `paraphrase` | **0.567** (30건) | 질의 말 ≠ 기억 말 |
| 못 맞힌 12건 | **11건이 「순위 없음」** — 후보에 아예 안 들어왔다. 1건만 순위 7 | **후보 생성의 구멍** |
| `update` | 0.800 (10건 중 2건 놓침) | 바뀐 결정 |
| `lint` 20k · 증분 | 16초 · 530ms | 이번 조사와 무관 |

---

## 6. 기능 대조표

| hindsight | 우리 mem | 판정 |
| --- | --- | --- |
| retain 때 LLM 사실 뽑기 | AI 가 직접 짧게 써서 `add` · 관문·lint 로 거름 | **다른 방식으로 있음** (우리가 더 싸고 결정적) |
| world / experience | 종류 일곱 | 다른 방식 (개발 기록엔 우리 쪽이 맞음) |
| observation (자동 모음 믿음) | 없음. DUP 이 「닮은 기억 뒤에 붙이기」까지만 | **없음** |
| mental model (상설 질문 답) | `SessionStart` 훅 요약(규칙) | 일부 있음 |
| 개체 해석·개체 링크 | `auto_links` 식별자 통 · 태그 · `[canon]` 대표말 | 다른 방식 (규칙) |
| 시간 링크 · 인과 링크 | 없음 | 없음 (인과는 LLM 필요) |
| 의미 길 **독립 후보** | **재정렬만** | **없음 — 핵심 차이** |
| 낱말 BM25 | FTS5 + 우리 bm25 + 한글 정규화 | 있음 (한글은 우리가 앞섬) |
| 그래프 퍼짐 (후보 데려옴) | 1-hop **가산만** (결정 44) | 일부 |
| 시간 길 (기간 안 의미 순) | 시간 표현 → 기간 목록 / 거르기 | 일부 |
| RRF | RRF 10신호 k=10 | 있음 |
| cross-encoder 재순위 | 없음 | **없음** |
| 최근성 상한 있는 곱 ±10% | 감쇠 바닥 0.3 (최대 −70%) | 다름 — 우리가 훨씬 세게 깎음 |
| 토큰 예산 recall | `--budget` · 훅 예산 | 있음 |
| 원문 chunk 같이 주기 | 기억 = 원문 (`mem show`) | 필요 없음 |
| reflect 에이전트 루프 | 없음 (부르는 AI 가 그 일을 함) | 안 가져옴 |
| Memory Defense (비밀·PII 45패턴, 주입 탐지) | 불변조건 5·6 (주입 중화 · 비밀 차단) | 있음 |
| 태그 범위 · bank 격리 | `scope` · 저장소 = 프로젝트 | 있음 |
| MCP · REST · 웹 UI | CLI + 훅만 (MCP 금지) | 안 가져옴 |

---

## 7. 가져올 만한 것 — 후보 일곱

크기 : S(반나절 이하) · M(1~2일) · L(3~5일) · XL(그 이상). 「잰다」는 기존 자(`Docs/Guide/측정절차.md`) 그대로 — **v2 80건·손 30건 r@5·MRR, G2 14판, 유형별 표, 검색 지연.**

### 후보 A. 의미 길을 「후보 데려오는 길」로 연다 — **1순위**

| 칸 | 내용 |
| --- | --- |
| 왜 | 못 맞힌 12건 중 11건이 「순위 없음」이다. 지금 의미 검색은 낱말이 데려온 200건 안에서만 순서를 바꾸니 **질의 말과 기억 말이 다르면 영영 못 닿는다.** hindsight 는 의미 길이 전체에서 따로 찾는다 |
| 고정 결정과 | 맞음. 서버 없이 `vectors.bin` 을 통째로 훑으면 된다(20k × 문서벡터, 7.9MiB). **G2 는 strict 판정에 안 섞어서 지킨다** |
| 어떻게 | ① 사다리에 **7번 칸 「의미 후보」**를 더한다 — 코사인 ≥ 새 문턱(`[embed] candidate_floor`, 예 0.55)인 상위 N(예 30)을 후보로 넣는다. ② 신뢰 계수 ×0.5 · **strict 아님** → 훅·eval strict·abstain 판정은 지금 그대로. ③ RRF 에는 지금 코사인 표를 그대로 쓰되 후보 목록이 넓어진다. ④ 결과 표에 `[의미]` 꼬리표. 색인·저장 형식 변화 없음 |
| 크기 | **M** |
| 위험 | 느슨 칸이 「없는 것」 질의에도 비슷한 기억을 보여 줄 수 있다 → `[의미]` 꼬리표와 문턱으로 막는다. 지연 +수십 ms [추정]. 결정 21(「순수 재정렬로 갈아타기 안 한다」)과는 다르다 — 낱말 판정은 그대로 두고 칸만 더한다 |
| 잰다 | `paraphrase`·`multisession` r@5 · 「순위 없음」 건수(11 → ?) · **G2 14판이 그대로인지** · abstain 10건에서 느슨 칸이 몇 건 뜨나 · 의미 모드 p95(자 900ms) |

### 후보 B. 링크 1-hop 이 후보를 데려오게 (그래프 퍼짐) — 1순위와 한 묶음

| 칸 | 내용 |
| --- | --- |
| 왜 | `multisession` 은 답이 여러 기억에 흩어진 문제다. 하나를 찾으면 `links`·`auto_links` 이웃이 나머지인 경우가 많을 것이다 [추정]. hindsight 그래프 길이 이 일을 한다 |
| 고정 결정과 | **결정 44(1-hop 은 가산으로만) 일부 뒤집기.** strict 칸 안에서는 지금처럼 가산만, **느슨 칸에서만** 이웃을 데려온다 |
| 어떻게 | strict 결과 상위 k(예 5)의 이웃을 8번 칸(×0.4, strict 아님)으로 넣는다. 이웃 점수는 hindsight 처럼 `tanh(공통 근거 수 × 0.5)` 로 포화시켜 흔한 태그가 판을 덮지 않게 한다 |
| 크기 | **S~M** |
| 위험 | 흔한 이웃이 매번 끼는 잡음. MMR 이 일부 걸러 준다 |
| 잰다 | `multisession` r@5 (expect_mode all) · MRR · G2 |

### 후보 C. 덮은 기억 따라가기 — 2순위

| 칸 | 내용 |
| --- | --- |
| 왜 | 질의 말이 **옛 결정**과 맞으면 옛 것(무효, ×0.5)만 걸리고, 다른 말로 쓴 새 결정은 못 닿는다. hindsight observation 이 「예전엔 X, 지금은 Y」를 한 글로 묶는 것의 **LLM 없는 판**이다 |
| 고정 결정과 | 맞음 (머리말 칸 그대로 · 규칙만) |
| 어떻게 | 무효 기억이 후보에 들면 `superseded_by` 사슬 끝의 살아 있는 기억을 **같은 칸·바로 위 자리**에 데려오고 `[덮은 것]` 꼬리표를 단다. 색인에 이미 있는 칸이라 스키마 변화 없음 |
| 크기 | **S** |
| 위험 | 사슬이 길거나 고리면 → 깊이 상한 · 방문 표 |
| 잰다 | `update` 10건 r@5(0.800) · 손 30건 `update` 2건 · G2 |

### 후보 D. 다국어 cross-encoder 재순위 (선택 설치) — 3순위

| 칸 | 내용 |
| --- | --- |
| 왜 | A·B 로 후보가 넓어지면 정밀도가 떨어진다. hindsight 는 「넓게 데려오고 재순위로 되찾는」 짝으로 쓴다. 우리 5회차 「모델 교체 실험」 말고 **두 번째 검색 손잡이**가 생긴다 |
| 고정 결정과 | 맞음. ONNX·`onnxruntime.dll` 길이 이미 있다(`mem install`). 없으면 지금처럼 돈다 |
| 어떻게 | 상위 30 쌍만 재순위. hindsight 처럼 **곱 · 상한 있는 가산**으로 섞어 RRF 를 뒤엎지 않게 한다. **후보는 안 늘린다**(G2). 모델은 다국어여야 한다 — hindsight 기본(`ms-marco-MiniLM-L-6-v2`)은 영어 전용이라 그대로 못 쓴다. 후보 모델의 한국어 품질·크기는 **확인 못 함** — 실험 몫 |
| 크기 | **M~L** (모델 고르기·꾸러미·SHA·install 갈래 포함) |
| 위험 | CPU 지연(30쌍 × 모델 크기). 훅에는 안 싣는다(결정 15 그대로). 이슈 #2841 처럼 링크로 데려온 결과를 깎는다 → B 결과는 재순위에서 빼거나 바닥을 둔다 |
| 잰다 | MRR · r@5 · 순위 6~10에 있던 정답이 올라오나 · 의미 모드 지연 |

### 후보 E. 감쇠를 「상한 있는 곱」으로 — 실험만

| 칸 | 내용 |
| --- | --- |
| 왜 | 우리는 history 를 1년 뒤 약 ×0.34 까지 깎고, hindsight 는 최근성을 ±10% 로 묶는다. 「multisession · 오래된 history」 놓침의 일부가 감쇠 탓일 수 있다 [추정] |
| 어떻게 | **코드 없이** `vocab.toml` `[type.history] half_life` 를 바꿔 eval 만 돈다. 효과가 보이면 그때 바닥값 손잡이를 `mem.toml` 로 |
| 크기 | **S** |
| 잰다 | 유형별 r@5 · temporal(0.917)이 안 떨어지나 |

### 후보 F. 「모음 기억」 (observation 의 우리 판) — 4순위 · **결정 뒤집기 필요**

| 칸 | 내용 |
| --- | --- |
| 왜 | `multisession` 을 근본에서 푸는 길 — 흩어진 기억 N건을 근거로 한 **모음 기억 1건**이 있으면 한 번에 닿는다. hook 요약 품질도 오른다 |
| 고정 결정과 | **「LLM 호출 0 (사용자 확정)」을 뒤집어야 한다.** 다만 넣을 때(add)·훅·검색에는 LLM 을 안 쓰고, **사람이 부르는 오프라인 명령에만** 쓴다. 결과는 `--hold` 로만 들어가고 사람이 `review --promote` 한다(결정 6 그대로). 원본은 여전히 md |
| 어떻게 | `mem consolidate --scope X` (가칭) : ① 코드가 `auto_links` 묶음에서 후보 무리를 고른다 ② 로컬 LLM 에 무리를 주고 **짧은 JSON** `{title, summary, bullets[], sources:[mem:id…]}` 만 받는다 ③ 코드가 md 를 만들고 관문·lint 를 그대로 건다 — `sources` 는 무리 안 id 만 허용(hindsight 「가져온 id 만 인용」과 같다) ④ `fact`·`howto` 종류 · `review: true`. 다시 만들 때는 hindsight **delta 연산** 꼴(블록 id 지목)로 안 바뀐 줄을 글자 그대로 둔다. 근거가 덮이면 기존 무효화 전파(`review --kind basis`)가 그대로 잡는다 |
| 크기 | **L** |
| 위험 | 로컬 LLM 날조(근거 없는 말). 로컬 서버는 한 번에 한 요청이라 작업과 겹친다. 모음 기억이 원 기억과 DUP 관문에 걸린다 → 종류·관문 예외 설계 필요 |
| 잰다 | 골든셋 정답 id 에 모음 기억이 없으니 **자를 새로 정해야 한다** (「정답 id 중 하나 또는 그것을 근거로 둔 모음 기억이 상위 5에」). 사람 승격률 · 날조 건수(근거 대조 코드 검사) |

### 후보 G. 상설 질문 → 훅 절 (mental model 의 규칙 판) — 5순위

| 칸 | 내용 |
| --- | --- |
| 왜 | hindsight 코딩 에이전트 연동은 세션 시작에 「지식 페이지」(구조·관례·진행 중인 일)를 싣는다. 우리 훅도 같은 자리인데 절이 종류별로 고정이다 |
| 고정 결정과 | 맞음 (LLM 없음 · 규칙) |
| 어떻게 | `mem.toml [hook] standing = ["scope:datatool #todo", …]` — 저장된 질의를 훅 예산 안에서 절로 싣는다 |
| 크기 | **S~M** |
| 위험 | 훅 예산(1,000토큰·8,000B·600ms) 압박. 훅은 침묵 규칙(I3) |
| 잰다 | 훅 크기·지연(G3) · 사용 피드백 |

---

## 8. 가져오지 말 것

| 무엇 | 까닭 |
| --- | --- |
| `Stop` 훅 자동 retain (대화를 통째로 LLM 에 넣기) | 매 턴 LLM 비용. 「도구 호출마다 자동 히스토리 안 남긴다 — 왜가 빠지고 잡음이 정확도를 깎는다」(mem `20260822-07ce6094`)와 정면 충돌. 로컬 Qwen 한 슬롯을 계속 잡는다 |
| Postgres · 서버 · 데몬 · MCP · 웹 UI | 단일 exe · 서버 없음 · MCP 금지(사용자 확정) |
| reflect 에이전트 루프 · 성향(disposition) | 부르는 AI(Claude) 가 이미 그 일을 한다. 성향은 절제 실험도 없다 |
| 확신도(confidence) 숫자 | hindsight 자신이 2026-04 에 지웠다 [코드] |
| LLM 개체 추출 · 인과 링크 | 넣을 때마다 LLM. 개발 기록에서 인과는 본문 글로 충분 [추정] |
| 이미지·첨부 retain | 기억은 짧은 글이 원칙 |
| bank 격리 · 다중 사용자 테넌트 | 우리는 저장소 = 프로젝트 · git 이 팀 공유를 맡는다 |

---

## 9. 통째로 갈아 끼우는 길 — 공정하게

| 길 | 얻는 것 | 잃는 것 |
| --- | --- | --- |
| **① mem 을 버리고 hindsight** | 네 길 검색 · 재순위 · observation · 55개 연동 · 활발한 개발 | **md 원본 + git diff 리뷰**(PR 로 기억을 본다는 설계 3절) · 관문·lint(문서 품질 > 검색 품질 원칙) · 한글 정규화 · LLM 0 · 단일 exe. 게임 저장소마다 서버·Postgres·LLM 키. 한글 BM25 는 도커 확장 필요 |
| **② mem 옆에 MCP 로 붙이기** | 대화 기억(개인 취향·흐름)을 hindsight 가 맡고, 결정 기록은 mem | MCP 금지(도구 정의가 매 턴 토큰을 먹는다) · 기억이 두 곳으로 갈라져 「어디서 찾나」가 생긴다 · Claude 내장 auto memory 와도 자리가 겹친다 |
| **③ hindsight 를 「채점용 비교군」으로만** | 같은 골든셋으로 「LLM 을 쓰면 얼마나 오르나」의 천장을 안다 | 설치·도커·LLM 비용. 이번 조사 범위 밖(설치 금지) |

**판단** [추정] : **①·②는 안 권한다.** 두 도구는 겨누는 일이 다르다 — hindsight 는 「대화에서 사실을 뽑아 개인화」, mem 은 「팀이 git 으로 리뷰하는 개발 결정 기록」. 우리가 원하는 것은 hindsight 의 **검색 구조**이지 저장 방식이 아니고, 그것은 우리 안에서 서버·LLM 없이 만들 수 있다(후보 A~E).
③은 5회차에서 G1 천장을 가늠하고 싶을 때만 따로 판단한다.

---

## 10. 우선순위와 먼저 해 볼 실험

| 순 | 무엇 | 왜 | 크기 | 위험 |
| --- | --- | --- | --- | --- |
| **1** | A + B : 의미 후보 칸 · 1-hop 확장 칸 (느슨 칸만) | 놓친 12건 중 11건이 「후보에 없음」 — 바로 그 구멍 | M | 느슨 칸 잡음 · 결정 44 일부 뒤집기 |
| **2** | C : `superseded_by` 따라가기 | `update` 놓침, LLM 없는 observation | S | 낮음 |
| **3** | D : 다국어 cross-encoder (선택 설치) | 넓힌 후보의 정밀도 · 두 번째 손잡이 | M~L | 한국어 모델 품질·지연 미확인 |
| **4** | F : 모음 기억 (오프라인 · `--hold`) | `multisession` 근본 해법 | L | **LLM 0 결정 뒤집기** · 날조 |
| **5** | G : 상설 질문 훅 절 | 세션 시작 품질 | S~M | 훅 예산 |

E(감쇠)는 코드 없는 실험이라 1번 실험과 같이 돈다.

### 먼저 해 볼 작은 실험 — 「후보 길만 넓히면 r@5 가 오르나」

1. 시험용 뒷문 환경변수 둘을 더한다 (`명령명세.md` 9절 관례) — 예 `MEM_TEST_EMBED_CANDIDATES=30` · `MEM_TEST_HOP_CANDIDATES=5`. 꺼져 있으면 지금과 바이트까지 같아야 한다 (시험 1건).
2. `Docs/Guide/측정절차.md` 대로 스크래치패드 사본 · 동결 exe · 시험 HOME 으로 **G2 를 먼저**, 그다음 v2 80건·손 30건 r@5·MRR 을 네 판(끔 / 의미만 / 1-hop 만 / 둘 다)으로 잰다.
3. 같은 판에서 `vocab.toml` history 반감기 둘(90일 · none)을 더 돌려 E 를 본다.
4. **판정** : 「순위 없음」 11건이 몇 건 줄었나 · `multisession`/`paraphrase` r@5 · G2 14판 그대로 · 의미 모드 지연 ≤ 900ms. 오르면 설계로, 안 오르면 「후보 생성도 천장」을 문서에 못 박는다.
- 크기 : **S** (코드 2~3시간 + 재기 1시간) [추정].

---

## 11. 사용자가 정할 것

| # | 물음 | 이 문서의 권장 |
| --- | --- | --- |
| 1 | 결정 44(1-hop 가산만)를 **느슨 칸에 한해** 풀어도 되나 | 실험 결과를 보고 정한다 |
| 2 | 「LLM 호출 0」을 **사람이 부르는 오프라인 명령 + `--hold`** 에 한해 풀 것인가 (후보 F) | 1~3순위를 먼저 하고, 그래도 `multisession` 이 낮으면 그때 |
| 3 | 재순위 모델을 `mem install` 선택 꾸러미에 더할 것인가 (용량 · 라이선스) | 실험으로 한국어 품질 확인 뒤 |
| 4 | 5회차 「모델 교체 실험」에 이 실험을 합칠 것인가 | 합친다 — 둘 다 검색 한 갈래라 측정 판을 나눠 쓸 수 있다 |

---

## 12. 확인한 것과 못 한 것

| 무엇 | 어떻게 봤나 |
| --- | --- |
| 별·릴리스·라이선스·언어 | **1차** — GitHub API (2026-10-04) |
| 기억 종류 · opinion 삭제 · 기본 모델 · 조각 크기 · 배치 크기 · delta 연산 · LLM 부르는 파일 | **1차** — 얕은 clone 의 소스 (`scratchpad/hindsight`, 커밋 `f7dd3f4`) |
| retain·recall·reflect·observation·mental model·성능·다국어·MCP 동작 | **1차** — 저장소 안 공식 문서 `hindsight-docs/docs/developer/*` |
| 벤치 수치 · 저자 소속 · 채점 LLM | **1차** — arXiv 논문 HTML (WebFetch 요약으로 읽음 — 표 숫자는 요약 도구를 거쳤다) |
| 「독립 재현이 아니다」 비판 · 운영 부담 비판 | **2차** — akitaonrails 조사 글 · 검색 결과 요약 |
| 경쟁 비교 · Mem0 49.0% | **2차** — 비교 블로그 · 자사 글 |
| 우리 쪽 수치·구조 | **1차** — 우리 문서 · `internal/search/rerank.go` · `internal/link/link.go` |
| 「순위 없음 11건이 A·B 로 닿는다」 | **추정** — 안 돌려 봤다. 10절 실험 몫 |
| 다국어 재순위 모델의 한국어 품질·크기·지연 | **확인 못 함** |
| hindsight 실제 돌려 본 지연·비용 | **확인 못 함** (설치 금지) |
| AMB 리더보드 본문(경쟁사 값) | **확인 못 함** — 페이지가 스크립트로 그려져 내용을 못 읽었다 |
| 가격 | 이번 결론에 안 써서 조사 안 함 |

그림(도면)은 넣지 않았다 — 도면 검사도 돌릴 것이 없다.
참고 프로젝트 이름 검사(doc-writing (가))는 `*.local.md` 를 열지 말라는 지시 때문에 목록 대조를 못 했다. 이 문서에는 Unity 프로젝트 이름이 하나도 없다(바깥 제품 이름만 있다).
