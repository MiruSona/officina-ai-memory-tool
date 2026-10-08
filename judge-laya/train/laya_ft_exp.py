# Laya 미세조정 — RLCD 잡음 정책경사 + CE · AdamW · 코사인 · 임베딩 얼림. 과제는 support 하나(근거·주장 → 지지/반대/무관).
# 쓰는 법은 ../README.md 「학습 고리」. 입력은 우리 꼴 jsonl(id·evidence·claim·want, _meta 줄은 건너뛴다).
import os
os.environ["CUDA_VISIBLE_DEVICES"] = ""
os.environ["HIP_VISIBLE_DEVICES"] = ""
os.environ["ROCR_VISIBLE_DEVICES"] = ""
import sys
import json
import time
import math
import random
import shutil
import argparse
import subprocess

import torch
from huggingface_hub import snapshot_download
from safetensors.torch import load_file, save_file
from laya.agent import _fix_tokenizer_config, _load_tokenizer
from laya.common import QTYPES, build_model, build_sequence, proper_reward, render_options

HERE = os.path.dirname(os.path.abspath(__file__))
REPO, SUB = "convaiinnovations/laya", "multilingual"
GOLD = {"support": "support", "contradict": "contra", "unrelated": "unrelated"}


def peak_rss_mb():
    """최대 RSS(MB). resource 가 없는 Windows 에서는 0 을 적는다."""
    try:
        import resource
    except ImportError:
        return 0.0
    return resource.getrusage(resource.RUSAGE_SELF).ru_maxrss / 1024


def qdef(task):
    return {"t": "choice", "ins": task["question"], "crit": {o["id"]: o["description"] for o in task["options"]}}


def read_pairs(path, limit, keys):
    rows = []
    for line in open(path, encoding="utf-8"):
        line = line.strip()
        if not line:
            continue
        r = json.loads(line)
        if "_meta" in r:
            continue
        rows.append({"id": r["id"], "gold": GOLD[r["want"]], "state": {keys[0]: r["evidence"], keys[1]: r["claim"]}})
        if limit and len(rows) >= limit:
            break
    return rows


def collate(items, pad_id):
    b = len(items); L = max(len(i["ids"]) for i in items); K = max(len(i["markers"]) for i in items)
    ids = torch.full((b, L), pad_id, dtype=torch.long); att = torch.zeros((b, L), dtype=torch.long)
    pos = torch.zeros((b, K), dtype=torch.long); msk = torch.zeros((b, K), dtype=torch.bool); tgt = torch.zeros((b, K))
    for i, it in enumerate(items):
        n = len(it["ids"]); ids[i, :n] = torch.tensor(it["ids"]); att[i, :n] = 1
        k = len(it["markers"]); pos[i, :k] = torch.tensor(it["markers"]); msk[i, :k] = True
        tgt[i, :len(it["target"])] = torch.tensor(it["target"])
    return ids, att, pos, msk, tgt, torch.tensor([it["qtype"] for it in items], dtype=torch.long)


def run_dev(a, saved):
    """저장한 모델마다 dev 를 laya_eval_exp.py 로 잰다 (fwd·rev). 시점 고르기는 사람이 summary 를 보고 한다."""
    for tag, path in saved:
        cmd = [sys.executable, os.path.join(HERE, "laya_eval_exp.py"), "--tag", f"{tag}-dev", "--set", a.dev,
               "--out-dir", a.out, "--model", path, "--threads", str(a.threads), "--question", a.question]
        print("dev 평가", tag, flush=True)
        subprocess.run(cmd, check=True)


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("--train", required=True, help="학습 jsonl")
    ap.add_argument("--out", required=True, help="모델을 저장할 폴더")
    ap.add_argument("--dev", default="", help="dev jsonl — 주면 저장한 시점마다 평가한다")
    ap.add_argument("--question", default=os.path.join(HERE, "..", "question-support.json"))
    ap.add_argument("--threads", type=int, default=8, help="torch CPU 스레드 수 (미니PC 학습 기본 8)")
    ap.add_argument("--epochs", type=int, default=4); ap.add_argument("--accum", type=int, default=4)
    ap.add_argument("--limit", type=int, default=0, help="앞에서 N 건만 (연기 판)")
    ap.add_argument("--from", dest="src_model", default="", help="출발 모델 폴더. 비면 HF 기본(laya multilingual)")
    ap.add_argument("--dry-run", action="store_true", help="자료만 세고 끝")
    ap.add_argument("--save-epochs", default="", help="예: 3,4,6 — 그 epoch 끝에 <out>/ep<N> 으로도 저장")
    a = ap.parse_args()
    torch.set_num_threads(a.threads)
    task = json.load(open(a.question, encoding="utf-8"))
    ids_order = [o["id"] for o in task["options"]]
    micro = 2
    rows = read_pairs(a.train, a.limit, task["state_keys"])
    counts = {k: sum(r["gold"] == k for r in rows) for k in ids_order}
    print(f"train file {a.train} rows {len(rows)} {counts} epochs {a.epochs} accum {a.accum} threads {a.threads} from {a.src_model or 'HF base'}", flush=True)
    if a.dry_run:
        print("dry-run: 자료만 셌다", flush=True); return
    if a.src_model:
        src = a.src_model
    else:
        root = snapshot_download(REPO, allow_patterns=[f"{SUB}/{n}" for n in ("rl_agent_config.json", "model.safetensors", "tokenizer/*", "encoder/*")])
        src = os.path.join(root, SUB)
    _fix_tokenizer_config(src)
    cfg = json.load(open(os.path.join(src, "rl_agent_config.json")))
    max_len, head_max_len = cfg.get("max_len", 1024), cfg.get("head_max_len", 256)
    tok = _load_tokenizer(os.path.join(src, "tokenizer"), cfg)
    train_cfg = dict(cfg); train_cfg["gradient_checkpointing"] = True
    model = build_model(train_cfg, encoder_dir=os.path.join(src, "encoder"))
    model.load_state_dict(load_file(os.path.join(src, "model.safetensors")), strict=True)
    model.float()
    model.encoder.gradient_checkpointing_enable(gradient_checkpointing_kwargs={"use_reentrant": False})
    model.head_checkpointing = True
    frozen = 0
    for n, p in model.named_parameters():
        if "embeddings" in n or "embed_tokens" in n or "tok_embeddings" in n:
            p.requires_grad_(False); frozen += p.numel()
    trainable = sum(p.numel() for p in model.parameters() if p.requires_grad)
    print(f"params frozen {frozen/1e6:.1f}M trainable {trainable/1e6:.1f}M", flush=True)
    model.train()

    q = qdef(task); n_opt = len(render_options(q))
    items = []
    for it in rows:
        target = [1.0 if ids_order[k] == it["gold"] else 0.0 for k in range(len(ids_order))]
        seq, markers = build_sequence(tok, it["state"], q, max_len, head_max_len)
        assert len(markers) == n_opt, it["id"]
        items.append({"ids": seq, "markers": markers, "qtype": QTYPES[q["t"]], "target": target})
    print(f"train items {len(items)}; max seq {max(len(i['ids']) for i in items)}", flush=True)

    enc_p = [p for n, p in model.named_parameters() if "encoder." in n and p.requires_grad]
    head_p = [p for n, p in model.named_parameters() if "encoder." not in n and p.requires_grad]
    opt = torch.optim.AdamW([{"params": enc_p, "lr": 2.5e-5}, {"params": head_p, "lr": 1e-4}], weight_decay=0.01)
    updates = max(1, math.ceil(len(items) / micro / a.accum) * a.epochs)
    sched = torch.optim.lr_scheduler.CosineAnnealingLR(opt, T_max=updates, eta_min=1e-6)
    t0 = time.time()
    log = []
    saved = []
    save_at = {int(x) for x in a.save_epochs.split(",") if x.strip()}

    def save_model(dst):
        os.makedirs(dst, exist_ok=True)
        model.eval()
        save_file({n: v.detach().half().contiguous() for n, v in model.state_dict().items()}, os.path.join(dst, "model.safetensors"))
        for d in ("encoder", "tokenizer"):
            shutil.copytree(os.path.join(src, d), os.path.join(dst, d), dirs_exist_ok=True)
        json.dump(cfg, open(os.path.join(dst, "rl_agent_config.json"), "w"), indent=2)
        model.train()

    for ep in range(a.epochs):
        random.Random(42 + ep).shuffle(items)
        opt.zero_grad(set_to_none=True)
        sigma = 0.4 + (0.1 - 0.4) * ep / max(1, a.epochs - 1)
        tot = 0.0; nb = 0
        for s in range(0, len(items), micro):
            ids, att, pos, msk, tgt, qt = collate(items[s:s + micro], tok.pad_token_id)
            logits, act = model(ids, att, pos, msk, qt)
            logits = logits.float(); k = msk.sum(-1, keepdim=True).float()
            eps = torch.randn((4,) + logits.shape) * sigma * msk
            eps = (eps - eps.sum(-1, keepdim=True) / k) * msk
            noisy = logits.detach().unsqueeze(0) + eps
            probs = torch.softmax(noisy.masked_fill(~msk, -1e4), -1)
            with torch.no_grad():
                rew = proper_reward(probs, tgt.unsqueeze(0), qt, msk, w_sph=0.75, w_rps=1.0)
                adv = rew - rew.mean(0, keepdim=True); adv = adv / (adv.std() + 1e-6)
            logp = -(((noisy - logits.unsqueeze(0)) ** 2) * msk).sum(-1) / (2 * sigma ** 2)
            loss = (-(adv * logp).mean() - (tgt * torch.log_softmax(logits.masked_fill(~msk, -1e4), -1)).sum(-1).mean() + 0.0 * act.sum()) / a.accum
            loss.backward(); nb += 1; tot += loss.item() * a.accum
            if nb % a.accum == 0 or s + micro >= len(items):
                torch.nn.utils.clip_grad_norm_([p for p in model.parameters() if p.requires_grad], 1.0)
                opt.step(); sched.step(); opt.zero_grad(set_to_none=True)
        rss = peak_rss_mb()
        print(f"epoch {ep+1}/{a.epochs} avg_loss {tot/nb:.4f} elapsed {time.time()-t0:.0f}s rss {rss:.0f}MB", flush=True)
        log.append({"epoch": ep + 1, "avg_loss": tot / nb, "elapsed_s": time.time() - t0, "rss_mb": rss})
        if (ep + 1) in save_at and (ep + 1) != a.epochs:
            dst = os.path.join(a.out, f"ep{ep+1}")
            save_model(dst); saved.append((f"ep{ep+1}", dst)); print(f"saved ep{ep+1}", flush=True)
    train_s = time.time() - t0
    save_model(a.out)
    saved.append((f"ep{a.epochs}", a.out))
    model.eval()
    peak = peak_rss_mb()
    json.dump({"train": a.train, "rows": len(rows), "counts": counts, "epochs": a.epochs, "accum": a.accum,
               "threads": a.threads, "from": a.src_model or "HF base", "train_s": train_s, "peak_rss_mb": peak,
               "epochs_log": log},
              open(os.path.join(a.out, "train_info.json"), "w"), ensure_ascii=False, indent=1)
    print(f"saved {a.out}; train time {train_s:.0f}s; peak RSS {peak:.0f}MB", flush=True)
    if a.dev:
        del model
        run_dev(a, saved)


if __name__ == "__main__":
    main()
