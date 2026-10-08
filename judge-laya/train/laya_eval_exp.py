# Laya 평가 — 쌍 셋을 CPU 에서 fwd·rev 두 순서로 묻고 정확도·갈래·거짓 지지·순서 뒤집기·지연을 낸다.
# 쓰는 법은 ../README.md 「학습 고리」. 결과: <out-dir>/laya_<tag>.jsonl (쌍·순서마다 한 줄) · <out-dir>/laya_<tag>.summary.json
import os
os.environ["CUDA_VISIBLE_DEVICES"] = ""
os.environ["HIP_VISIBLE_DEVICES"] = ""
os.environ["ROCR_VISIBLE_DEVICES"] = ""
import sys
import json
import time
import math
import argparse

import torch
import laya

HERE = os.path.dirname(os.path.abspath(__file__))
GOLD = {"support": "support", "contradict": "contra", "unrelated": "unrelated"}


def peak_rss_mb():
    """최대 RSS(MB). resource 가 없는 Windows 에서는 0 을 적는다."""
    try:
        import resource
    except ImportError:
        return 0.0
    return resource.getrusage(resource.RUSAGE_SELF).ru_maxrss / 1024


def question(task, order):
    return {"type": "choice", "instructions": task["question"],
            "criteria": {o["id"]: o["description"] for o in task["options"]}, "option_order": order}


def pct(xs, q):
    xs = sorted(xs); k = (len(xs) - 1) * q; f = math.floor(k); c = math.ceil(k)
    return xs[f] if f == c else xs[f] + (xs[c] - xs[f]) * (k - f)


def read_items(path, keys):
    """.laya.json(items 꼴) 이나 우리 꼴 jsonl(id·evidence·claim·want) 을 같은 items 로 읽는다."""
    if path.endswith(".json"):
        return json.load(open(path, encoding="utf-8"))["items"]
    items = []
    for line in open(path, encoding="utf-8"):
        line = line.strip()
        if not line:
            continue
        r = json.loads(line)
        if "_meta" in r:
            continue
        items.append({"id": r["id"], "gold": GOLD[r["want"]], "state": {keys[0]: r["evidence"], keys[1]: r["claim"]}})
    return items


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("--tag", required=True)
    ap.add_argument("--set", required=True, help=".laya.json 이나 jsonl")
    ap.add_argument("--out-dir", required=True)
    ap.add_argument("--model", required=True, help="모델 폴더나 HF id")
    ap.add_argument("--sub", default="", help="HF id 일 때 하위 폴더 (예 multilingual)")
    ap.add_argument("--question", default=os.path.join(HERE, "..", "question-support.json"))
    ap.add_argument("--threads", type=int, default=8)
    a = ap.parse_args()
    torch.set_num_threads(a.threads)
    task = json.load(open(a.question, encoding="utf-8"))
    ids = [o["id"] for o in task["options"]]
    letter = {o["id"]: o["letter"].upper() for o in task["options"]}
    items = read_items(a.set, task["state_keys"])
    os.makedirs(a.out_dir, exist_ok=True)
    t0 = time.time()
    if a.sub:
        agent = laya.load(a.model, subfolder=a.sub, device="cpu")
    else:
        agent = laya.load(a.model, device="cpu")
    load_s = time.time() - t0
    print("cuda available:", torch.cuda.is_available(), "load", round(load_s, 1), "s", flush=True)
    keys = task["state_keys"]
    agent.predict({keys[0]: "데우기", keys[1]: "데우기"}, {"q": question(task, [0, 1, 2])})
    out = open(os.path.join(a.out_dir, f"laya_{a.tag}.jsonl"), "w", encoding="utf-8")
    lat = []; res = {}
    n = len(ids)
    for it in items:
        for oname, order in [("fwd", list(range(n))), ("rev", list(reversed(range(n))))]:
            s = time.perf_counter()
            r = agent.predict(it["state"], {"q": question(task, order)})
            dt = time.perf_counter() - s
            lat.append(dt)
            probs = {k: float(v) for k, v in r["answers"]["q"]["probabilities"].items()}
            pred = max(probs, key=probs.get)
            rec = {"id": it["id"], "gold": it["gold"], "order": oname, "pred": pred,
                   "right": pred == it["gold"], "probs": probs, "lat_s": dt}
            out.write(json.dumps(rec, ensure_ascii=False) + "\n")
            res[(it["id"], oname)] = rec
    out.close()
    rss_mb = peak_rss_mb()
    # 채점 — 대표 답은 fwd. 갈래별 · 거짓 지지(B/C→A) · 순서 뒤집기.
    total = len(items); right = 0; flip = 0; false_support = 0
    branch = {k: {"n": 0, "right": 0} for k in ids}
    confusion = {g: {p: 0 for p in ids} for g in ids}
    for it in items:
        pf = res[(it["id"], "fwd")]; pr = res[(it["id"], "rev")]
        g = it["gold"]; p = pf["pred"]
        branch[g]["n"] += 1
        confusion[g][p] += 1
        if p == g:
            right += 1; branch[g]["right"] += 1
        if g != ids[0] and p == ids[0]:
            false_support += 1
        if pf["pred"] != pr["pred"]:
            flip += 1
    summary = {"tag": a.tag, "model": a.model, "set": os.path.basename(a.set), "n": total,
               "accuracy": right / total,
               "branch": {letter[k] + "-" + k: {"n": v["n"], "right": v["right"],
                                                 "acc": (v["right"] / v["n"]) if v["n"] else None} for k, v in branch.items()},
               "false_support": false_support, "flip": flip, "flip_rate": flip / total,
               "confusion": confusion, "threads": a.threads,
               "lat_p50_ms": pct(lat, .5) * 1000, "lat_p95_ms": pct(lat, .95) * 1000, "lat_max_ms": max(lat) * 1000,
               "load_s": load_s, "peak_rss_mb": rss_mb}
    json.dump(summary, open(os.path.join(a.out_dir, f"laya_{a.tag}.summary.json"), "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    print(f"[{a.tag}] n={total} acc={summary['accuracy']:.3f} "
          + " ".join(f"{k}={v['right']}/{v['n']}" for k, v in summary["branch"].items())
          + f" false_support={false_support} flip={flip} p50={summary['lat_p50_ms']:.0f}ms p95={summary['lat_p95_ms']:.0f}ms rss={rss_mb:.0f}MB",
          flush=True)


if __name__ == "__main__":
    main()
