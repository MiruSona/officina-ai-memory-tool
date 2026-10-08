# judge-laya — Laya 판정 프로세스

mem 의 판정 사다리 둘째 단(`laya-v2`)을 맡는 작은 HTTP 서버다. 근거·주장 한 쌍을 받아 **지지(a) · 반대(b) · 무관(c)** 확률을 돌려준다.
mem(Go)에 torch·DLL 을 넣지 않으려고 따로 띄운다. 설계는 스튜디오 `Docs/Design/2026-10-08-자체판정프로그램설계.md` 4절·6절.

- 서빙은 **ONNX Runtime 판이 기본**이다 (torch 없음). torch 판(`--backend torch`)은 물러설 길로 같이 둔다.
- **모델 파일은 git 밖**이다. 기본 자리 `~/.aimemory/laya/laya-v2/` (`--model` 이나 `LAYA_MODEL_DIR` 로 바꾼다).
- 127.0.0.1 에만 붙는다. 다른 기계에서 부를 수 없다.

## 파일

| 파일 | 하는 일 |
| --- | --- |
| `serve.py` | 판정 서버. `POST /judge` · `GET /health` |
| `export_onnx.py` | 학습한 모델 폴더 → 서빙 폴더 (`model.onnx` · `tokenizer/` · `question.json` · torch 판 파일) |
| `check_onnx.py` | 일치 검사. 쌍마다 torch 판 확률과 서버 응답 확률을 견주고 HTTP p95 를 잰다 |
| `question-support.json` | 물음 글·선택지 순서 (학습·평가·내보내기가 같이 읽는다. 글을 바꾸면 다시 학습) |
| `train/laya_ft_exp.py` | 미세조정 |
| `train/laya_eval_exp.py` | 평가 (fwd·rev 두 순서 · 정확도 · 갈래 · 거짓 지지 · 순서 뒤집기 · 지연) |
| `train/freeze.py` · `train/memstore.py` | 실물 측정 셋 얼리기 · mem 저장소 읽기 부품 |

## 까는 법

venv 는 둘이다. 서빙만 하는 기계는 첫째만 깐다.

```powershell
# 서빙용 (onnxruntime · tokenizers · numpy)
python -m venv ~/.laya-local/venv-serve
~/.laya-local/venv-serve/Scripts/python -m pip install -r requirements.txt

# 학습·내보내기·일치 검사·torch 판 서빙용
python -m venv ~/.laya-local/venv-train
~/.laya-local/venv-train/Scripts/python -m pip install -r requirements-train.txt --extra-index-url https://download.pytorch.org/whl/cpu
```

미니PC(리눅스)는 `Scripts/python` 대신 `bin/python` 이다.

## 띄우기

```powershell
~/.laya-local/venv-serve/Scripts/python -I serve.py --model ~/.aimemory/laya/laya-v2 --port 8091 --threads 4
```

- 인자 : `--model`(서빙 폴더) · `--port`(기본 8091) · `--threads`(기본 4) · `--backend onnx|torch`(기본 onnx).
- torch 판은 학습용 venv 로 띄운다 : `~/.laya-local/venv-train/Scripts/python -I serve.py --backend torch …` (RSS 약 2.1GB).
- 뜨면 `laya onnx 판 · 모델 564b7ff8 · 올림 3.6s · http://127.0.0.1:8091` 한 줄을 찍는다.

## 끝점

| 끝점 | 요청 | 응답 |
| --- | --- | --- |
| `POST /judge` | `{"evidence":"…","claim":"…"}` (UTF-8 JSON, 1MB 까지) | `{"a":0.81,"b":0.12,"c":0.07,"model":"564b7ff8","ms":42.0}` |
| `GET /health` | — | `{"ok":true,"model":"564b7ff8","backend":"onnx"}` |

- `model` 은 학습 가중치(`model.safetensors`) SHA-256 앞 8자다. ONNX 판과 torch 판이 같은 값을 낸다.
- `ms` 는 서버 안에서 잰 판정 시간이다 (HTTP 왕복 빼고).
- 한 순서(fwd: 지지·반대·무관)로만 묻는다. 잘못된 요청은 400 · 너무 크면 413 · 없는 길은 404.

## mem 쪽 설정

`llm.toml` 에 세 칸 (기본은 꺼짐):

```toml
laya_url = "http://127.0.0.1:8091"   # 비면 Laya 단을 건너뛴다
laya_timeout_ms = 1000                # 1~5000
laya_sure = 0.70                      # 최고 확률이 이 아래면 다음 단(SemIf)으로 넘긴다
```

## 모델 자리

| 자리 | 무엇 |
| --- | --- |
| `~/.aimemory/laya/laya-v1/` | 10-08 실험 모델 (합성 900건 · ep4 · 모델 `564b7ff8`). 실물 정확도 0.68 이라 **비교·배선 확인용**이다 |
| `~/.aimemory/laya/laya-v2/` | 10-08 실물 1,230건 학습 (ep4 · 모델 `2aa6cc03`). game90 0.757 로 **접음** — `laya_url` 을 비워 둔다 (`../Docs/Research/2026-10-08-자체판정프로그램실측.md`) |

서빙 폴더 안 : `model.onnx` · `question.json` · `tokenizer/` (ONNX 판) + `model.safetensors` · `encoder/` · `rl_agent_config.json` (torch 판).

## 학습 고리

학습은 미니PC CPU 에서 돌린다. 자리 확인(게임 서버 · 다른 판 · 가용 메모리)은 설계 6절 2번.

```bash
# 1. 학습 (dev 를 주면 저장한 epoch 마다 평가까지)
systemd-run --user --scope -p MemoryMax=8G -p MemorySwapMax=0 nice -n 10 \
  ~/judge-laya/.venv/bin/python train/laya_ft_exp.py --threads 8 \
  --train laya2/train.jsonl --dev laya2/dev.jsonl --out laya2/ft --epochs 6 --save-epochs 3,4

# 2. 따로 평가 (jsonl 이나 .laya.json)
python train/laya_eval_exp.py --tag game90 --set game90.jsonl --out-dir laya2/eval --model laya2/ft/ep4 --threads 8

# 3. 내보내기 (고른 epoch 폴더 → 서빙 폴더) · --check-pairs 는 열이 laya 와 같은지 쌍마다 본다
python export_onnx.py --src laya2/ft/ep4 --out ~/.aimemory/laya/laya-v2 --check-pairs <K 48 pairs.jsonl>

# 4. 일치 검사 (서버를 먼저 띄운다) — 결과를 txt 에 덧붙인다
python check_onnx.py --model ~/.aimemory/laya/laya-v2 --pairs <K 48 pairs.jsonl> --url http://127.0.0.1:8091 --out ~/.laya-local/laya2/onnx-check.txt
```

- 입력 jsonl 은 한 줄에 `{"id","evidence","claim","want"}` (`want` 는 `support`·`contradict`·`unrelated`). `_meta` 줄은 건너뛴다.
- `freeze.py` : `python train/freeze.py --new <새 쌍.jsonl> --out-dir <git 밖 폴더> [--k-pairs …] [--store Memory/store]`.
- `python -I` 로 돌릴 때 출력은 UTF-8 로 나온다 (스크립트가 직접 맞춘다).

## 자

| 항목 | 자 |
| --- | --- |
| ONNX 와 torch 확률 차 (K 48) | ≤ 1e-3 |
| HTTP 포함 p95 (순차) | ≤ 0.1초 |
