# 서빙 일치 검사 — 쌍마다 torch 판(laya.Agent.predict, fwd) 확률과 serve.py 응답 확률을 견주고 HTTP p95 를 잰다.
# 학습용 venv 에서 돈다. serve.py 는 따로 띄워 둔다. 자: 확률 차 최대 ≤ 1e-3 · p95 ≤ 100ms (설계 4절).
import argparse
import json
import math
import os
import sys
import time
import urllib.request

import torch
import laya

LIMIT_DIFF = 1e-3
LIMIT_P95_MS = 100.0


def pct(xs, q):
    xs = sorted(xs)
    k = (len(xs) - 1) * q
    f = math.floor(k)
    c = math.ceil(k)
    if f == c:
        return xs[f]
    return xs[f] + (xs[c] - xs[f]) * (k - f)


def read_pairs(path):
    rows = []
    for line in open(path, encoding="utf-8"):
        line = line.strip()
        if not line:
            continue
        row = json.loads(line)
        if "_meta" not in row:
            rows.append(row)
    return rows


def ask(url, evidence, claim):
    data = json.dumps({"evidence": evidence, "claim": claim}, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(url + "/judge", data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as res:
        return json.loads(res.read().decode("utf-8"))


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", required=True, help="서빙 폴더 (question.json · model.safetensors)")
    ap.add_argument("--pairs", required=True, help="jsonl (id·evidence·claim)")
    ap.add_argument("--url", default="http://127.0.0.1:8091")
    ap.add_argument("--label", default="onnx")
    ap.add_argument("--out", required=True, help="결과를 덧붙일 txt")
    ap.add_argument("--threads", type=int, default=4)
    a = ap.parse_args()

    torch.set_num_threads(a.threads)
    model_dir = os.path.abspath(os.path.expanduser(a.model))
    spec = json.load(open(os.path.join(model_dir, "question.json"), encoding="utf-8"))
    ids = [o["id"] for o in spec["options"]]
    letters = [o["letter"] for o in spec["options"]]
    keys = spec["state_keys"]
    agent = laya.load(model_dir, device="cpu")
    question = {"q": spec["laya_question"]}
    rows = read_pairs(a.pairs)

    ask(a.url, "데우기", "데우기")
    lines = []
    diffs = []
    lat = []
    agree = 0
    for row in rows:
        r = agent.predict({keys[0]: row["evidence"], keys[1]: row["claim"]}, question)
        want = [float(r["answers"]["q"]["probabilities"][i]) for i in ids]
        got_body = ask(a.url, row["evidence"], row["claim"])
        got = [float(got_body[x]) for x in letters]
        d = max(abs(w - g) for w, g in zip(want, got))
        diffs.append(d)
        if want.index(max(want)) == got.index(max(got)):
            agree += 1
        lines.append(f"{row['id']}\tdiff {d:.2e}\ttorch {['%.4f' % v for v in want]}\tserve {['%.4f' % v for v in got]}\t")

    # 지연은 torch 를 끼우지 않은 따로 한 바퀴로 잰다 — 사이에 torch 가 돌면 캐시가 식어 서버가 느려 보인다.
    for row in rows:
        start = time.perf_counter()
        ask(a.url, row["evidence"], row["claim"])
        lat.append((time.perf_counter() - start) * 1000)

    worst = max(diffs)
    p95 = pct(lat, 0.95)
    passed = worst <= LIMIT_DIFF and p95 <= LIMIT_P95_MS
    head = [
        f"# 서빙 일치 검사 · {a.label} · {time.strftime('%Y-%m-%d %H:%M')}",
        f"모델 {spec['weights_sha256'][:8]} · 쌍 {len(rows)} ({os.path.basename(a.pairs)}) · 서버 {a.url} · {got_body['model']}",
        f"확률 차 최대 {worst:.2e} (자 {LIMIT_DIFF:g}) · 최고 답 일치 {agree}/{len(rows)}",
        f"HTTP 지연 p50 {pct(lat, 0.5):.1f}ms · p95 {p95:.1f}ms · 최대 {max(lat):.1f}ms (자 {LIMIT_P95_MS:g}ms, 순차 · 따로 한 바퀴)",
        f"판정 : {'통과' if passed else '미달'}",
        "참고 : torch 판 확률은 laya 가 소수 넷째 자리로 반올림해 준다 (차 바닥 약 5e-5)",
    ]
    text = "\n".join(head + ["", "쌍별 (id · 차 · torch a,b,c · serve a,b,c)"] + lines) + "\n\n"
    os.makedirs(os.path.dirname(os.path.abspath(a.out)), exist_ok=True)
    with open(a.out, "a", encoding="utf-8", newline="\n") as f:
        f.write(text)
    print("\n".join(head), flush=True)


if __name__ == "__main__":
    main()
