# 실물 100건 frozen test 를 만든다 — K 48쌍 + 새 52쌍 → frozen_test.jsonl · exclude_ids.txt · FROZEN.sha256
# 쓰는 법은 ../README.md 「학습 고리」. 결과는 --out-dir(git 밖)에 쓴다. 라벨은 사용자 확정 전이라 「잠정」이다.
import os
import sys
import json
import argparse
import hashlib
import collections

# python -I 는 스크립트 폴더를 sys.path 에 안 넣는다 — 옆 파일 memstore 를 찾게 직접 넣는다.
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import memstore

OFFICINA = os.environ.get("OFFICINA", os.getcwd())

# K 측정 문서 9절에서 「약한 쌍」으로 가른 여덟. v3 make 판이 없어 그대로 쓰되 표시만 남긴다.
WEAK_K = {"k004", "k007", "k009", "k016", "k020", "k023", "k026", "k035"}


def read_jsonl(path):
    rows = []
    for line in open(path, encoding="utf-8"):
        line = line.strip()
        if line:
            rows.append(json.loads(line))
    return rows


def verify_evidence(rows, store):
    """새 쌍의 근거가 그 기억 본문에 글자 그대로 있는지 본다. 하나라도 없으면 멈춘다."""
    bad = []
    for r in rows:
        mem_id = r["src"].split("+")[0]
        item = store.get(mem_id)
        if item is None:
            bad.append((r["id"], "기억 없음 " + mem_id))
            continue
        flat = " ".join(item["body"].split())
        if " ".join(r["evidence"].split()) not in flat:
            bad.append((r["id"], "근거가 본문에 없음"))
    return bad


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("--k-pairs", default=os.path.join(OFFICINA, "Memory", "local", "kmeasure", "pairs.jsonl"))
    ap.add_argument("--new", required=True, help="새 52쌍 jsonl")
    ap.add_argument("--store", default=memstore.STORE, help="근거를 대조할 mem 저장소 (Memory/store)")
    ap.add_argument("--out-dir", required=True, help="frozen_test.jsonl · exclude_ids.txt · FROZEN.sha256 을 쓸 곳 (git 밖)")
    a = ap.parse_args()
    out = os.path.join(a.out_dir, "frozen_test.jsonl")
    exclude_path = os.path.join(a.out_dir, "exclude_ids.txt")
    sha_path = os.path.join(a.out_dir, "FROZEN.sha256")
    os.makedirs(a.out_dir, exist_ok=True)

    k_rows = read_jsonl(a.k_pairs)
    new_rows = read_jsonl(a.new)
    if len(k_rows) != 48 or len(new_rows) != 52:
        sys.exit(f"건수 어긋남 K {len(k_rows)} 새 {len(new_rows)}")

    store = {it["id"]: it for it in memstore.load_all(store=a.store)}
    bad = verify_evidence(new_rows, store)
    if bad:
        for b in bad:
            print("근거 검증 실패", b)
        sys.exit(1)

    rows = []
    for r in k_rows:
        rows.append({
            "id": r["id"], "evidence": r["evidence"], "claim": r["claim"], "want": r["want"],
            "rule": r["rule"], "src": r["src"], "origin": "k48",
            "confidence": "low" if r["id"] in WEAK_K else "high",
        })
    for r in new_rows:
        rows.append({
            "id": r["id"], "evidence": r["evidence"], "claim": r["claim"], "want": r["want"],
            "rule": r["rule"], "src": r["src"], "origin": "new52",
            "confidence": r.get("confidence", "high"),
        })

    want = collections.Counter(r["want"] for r in rows)
    if want != collections.Counter({"support": 34, "contradict": 33, "unrelated": 33}):
        sys.exit(f"갈래 분포 어긋남 {dict(want)}")

    meta = {
        "_meta": "frozen_test v1",
        "status": "잠정 — 라벨은 Opus 초안이고 사용자 확정 전이다. 확정되면 status 를 바꾸고 SHA 를 다시 적는다",
        "date": "2026-10-08",
        "count": len(rows),
        "want": dict(want),
        "k48": "v3 make 판 없음 — 10-07 pairs.jsonl 그대로. 약한 쌍 8 은 confidence low",
        "low_confidence": [r["id"] for r in rows if r["confidence"] == "low"],
    }
    with open(out, "w", encoding="utf-8", newline="\n") as f:
        f.write(json.dumps(meta, ensure_ascii=False) + "\n")
        for r in rows:
            f.write(json.dumps(r, ensure_ascii=False) + "\n")

    ids = set()
    for r in rows:
        ids.update(r["src"].split("+"))
    with open(exclude_path, "w", encoding="utf-8", newline="\n") as f:
        f.write("# frozen_test 100건이 쓴 기억 id — 합성 근거 풀에서 뺀다 (잠정 · 2026-10-08)\n")
        for i in sorted(ids):
            f.write(i + "\n")

    digest = hashlib.sha256(open(out, "rb").read()).hexdigest()
    with open(sha_path, "w", encoding="utf-8", newline="\n") as f:
        f.write("# 잠정 — 사용자 라벨 확정 전. 확정 뒤 다시 적는다\n")
        f.write(f"{digest}  frozen_test.jsonl\n")
    print("frozen_test", len(rows), dict(want), "exclude ids", len(ids))
    print("sha256", digest)
    print("low confidence", meta["low_confidence"])


if __name__ == "__main__":
    main()
