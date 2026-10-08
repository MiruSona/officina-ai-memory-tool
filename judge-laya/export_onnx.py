# 학습한 Laya 모델을 서빙 폴더로 내보낸다 — model.onnx · tokenizer/ · question.json (+ torch 판 파일).
# 쓰는 법은 README.md 「내보내기」. torch·laya 가 든 학습용 venv 에서 돈다.
import argparse
import hashlib
import json
import os
import shutil
import sys
import time

import numpy as np
import torch
import laya
from laya.common import QTYPES, build_head, build_sequence, clamp_temperature, temp_bucket

HERE = os.path.dirname(os.path.abspath(__file__))
OPSET = 17
FORMAT = 1


def sha256_of(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def laya_question(task):
    """laya.Agent.predict 가 받는 물음 꼴. 순서는 늘 fwd(0,1,2) — 서빙은 한 순서만 묻는다."""
    ids = [o["id"] for o in task["options"]]
    return {"type": "choice", "instructions": task["question"],
            "criteria": {o["id"]: o["description"] for o in task["options"]},
            "option_order": list(range(len(ids)))}


def internal_question(task):
    """laya.common 의 build_head 가 받는 안쪽 꼴 (Agent._to_internal 결과와 같다)."""
    lq = laya_question(task)
    return {"t": lq["type"], "ins": lq["instructions"], "crit": lq["criteria"], "option_order": lq["option_order"]}


def state_text(task, evidence, claim):
    """Laya 가 상태를 글로 바꾸는 방식(serialize_state = json.dumps ensure_ascii=False) 그대로."""
    keys = task["state_keys"]
    return json.dumps({keys[0]: evidence, keys[1]: claim}, ensure_ascii=False)


def numpy_sequence(spec, tokenizer, text):
    """serve.py 와 같은 셈으로 열을 만든다. build_sequence 와 같은지 여기서 미리 본다."""
    state_ids = tokenizer.encode(text.replace(spec["mask_token"], " "), add_special_tokens=False).ids
    head = spec["head_ids"]
    room = max(0, spec["max_len"] - len(head) - 1)
    ids = (head + state_ids[:room] + [spec["sep_id"]])[: spec["max_len"]]
    markers = [m for m in spec["markers"] if m < spec["max_len"]]
    return ids, markers


def check_sequences(spec, agent, task, pairs_path):
    """쌍 파일의 모든 쌍에서 tokenizers 판 열 == laya build_sequence 열인지 본다. 다르면 멈춘다."""
    from tokenizers import Tokenizer
    tokenizer = Tokenizer.from_file(os.path.join(spec["_out"], "tokenizer", "tokenizer.json"))
    q = internal_question(task)
    n = 0
    for line in open(pairs_path, encoding="utf-8"):
        line = line.strip()
        if not line:
            continue
        row = json.loads(line)
        if "_meta" in row:
            continue
        keys = task["state_keys"]
        state = {keys[0]: row["evidence"], keys[1]: row["claim"]}
        want_ids, want_markers = build_sequence(agent.tok, state, q, spec["max_len"], spec["head_max_len"],
                                                option_order=q["option_order"])
        got_ids, got_markers = numpy_sequence(spec, tokenizer, state_text(task, row["evidence"], row["claim"]))
        if got_ids != want_ids or got_markers != want_markers:
            sys.exit(f"열 어긋남 {row.get('id')} : tokenizers 판 {len(got_ids)} · laya 판 {len(want_ids)}")
        n += 1
    print(f"열 검사 통과 {n}쌍", flush=True)


class Wrapper(torch.nn.Module):
    """DecisionModel 의 forward 를 ONNX 입력 다섯 개로 감싼다 (detach_encoder 인자를 숨긴다)."""

    def __init__(self, model):
        super().__init__()
        self.model = model

    def forward(self, input_ids, attention_mask, marker_pos, marker_mask, qtype):
        return self.model(input_ids, attention_mask, marker_pos, marker_mask, qtype)


def export_graph(agent, spec, onnx_path):
    ids = spec["head_ids"] + [5] * 40 + [spec["sep_id"]]
    inputs = (
        torch.tensor([ids], dtype=torch.long),
        torch.ones((1, len(ids)), dtype=torch.long),
        torch.tensor([spec["markers"]], dtype=torch.long),
        torch.ones((1, len(spec["markers"])), dtype=torch.bool),
        torch.tensor([spec["qtype"]], dtype=torch.long),
    )
    names = ["input_ids", "attention_mask", "marker_pos", "marker_mask", "qtype"]
    axes = {"input_ids": {0: "batch", 1: "seq"}, "attention_mask": {0: "batch", 1: "seq"},
            "marker_pos": {0: "batch", 1: "k"}, "marker_mask": {0: "batch", 1: "k"}, "qtype": {0: "batch"},
            "logits": {0: "batch", 1: "k"}, "act_logits": {0: "batch"}}
    wrapper = Wrapper(agent.model).eval()
    # 추론 빠른 길(_transformer_encoder_layer_fwd)은 ONNX 로 못 옮긴다 — 내보낼 때만 끈다.
    torch.backends.mha.set_fastpath_enabled(False)
    t0 = time.time()
    with torch.no_grad():
        torch.onnx.export(wrapper, inputs, onnx_path, input_names=names, output_names=["logits", "act_logits"],
                          dynamic_axes=axes, opset_version=OPSET, dynamo=False, do_constant_folding=True)
    print(f"ONNX 내보냄 {onnx_path} ({time.time() - t0:.0f}s, {os.path.getsize(onnx_path) / 1e6:.0f}MB)", flush=True)


def smoke_compare(agent, spec, onnx_path):
    """길이가 다른 열 둘로 torch 와 ONNX logits 를 견준다 — 길이 축이 굳지 않았는지 본다."""
    import onnxruntime as ort
    sess = ort.InferenceSession(onnx_path, providers=["CPUExecutionProvider"])
    worst = 0.0
    for extra in (7, 200):
        ids = spec["head_ids"] + list(range(1000, 1000 + extra)) + [spec["sep_id"]]
        feeds = {
            "input_ids": np.array([ids], dtype=np.int64),
            "attention_mask": np.ones((1, len(ids)), dtype=np.int64),
            "marker_pos": np.array([spec["markers"]], dtype=np.int64),
            "marker_mask": np.ones((1, len(spec["markers"])), dtype=bool),
            "qtype": np.array([spec["qtype"]], dtype=np.int64),
        }
        got = sess.run(["logits"], feeds)[0]
        with torch.no_grad():
            want = agent.model(*[torch.from_numpy(feeds[k]) for k in
                                 ("input_ids", "attention_mask", "marker_pos", "marker_mask", "qtype")])[0].numpy()
        worst = max(worst, float(np.abs(got - want).max()))
    print(f"연기 견줌 logits 차 최대 {worst:.2e}", flush=True)
    return worst


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser(description="Laya 모델 → 서빙 폴더(model.onnx · tokenizer/ · question.json)")
    ap.add_argument("--src", required=True, help="학습 모델 폴더 (model.safetensors · encoder/ · tokenizer/ · rl_agent_config.json)")
    ap.add_argument("--out", required=True, help="서빙 폴더 — 예 ~/.aimemory/laya/laya-v2")
    ap.add_argument("--question", default=os.path.join(HERE, "question-support.json"))
    ap.add_argument("--check-pairs", default="", help="jsonl(evidence·claim) — 열이 laya 와 같은지 쌍마다 본다")
    ap.add_argument("--no-onnx", action="store_true", help="question.json·토크나이저·torch 파일만 (torch 판 서빙용)")
    ap.add_argument("--threads", type=int, default=4)
    a = ap.parse_args()

    torch.set_num_threads(a.threads)
    src = os.path.abspath(os.path.expanduser(a.src))
    out = os.path.abspath(os.path.expanduser(a.out))
    task = json.load(open(a.question, encoding="utf-8"))
    os.makedirs(out, exist_ok=True)

    weights_sha = sha256_of(os.path.join(src, "model.safetensors"))
    agent = laya.load(src, device="cpu")
    agent.model.float().eval()
    cfg = agent.cfg
    q = internal_question(task)
    head_ids, markers, _ = build_head(agent.tok, q, cfg["head_max_len"], option_order=q["option_order"])
    qtype = QTYPES[q["t"]]
    k = len(task["options"])

    spec = {
        "format": FORMAT,
        "weights_sha256": weights_sha,
        "laya_version": laya.__version__,
        "max_len": cfg["max_len"],
        "head_max_len": cfg["head_max_len"],
        "head_ids": head_ids,
        "markers": markers,
        "sep_id": agent.tok.sep_token_id,
        "pad_id": agent.tok.pad_token_id,
        "mask_token": agent.tok.mask_token,
        "qtype": qtype,
        "temperature": agent.temperature_by_options.get(temp_bucket(qtype, k), agent.temperature[qtype]),
        "state_keys": task["state_keys"],
        "options": [{"id": o["id"], "letter": o["letter"]} for o in task["options"]],
        "laya_question": laya_question(task),
    }
    spec["temperature"] = clamp_temperature(spec["temperature"])

    shutil.copytree(os.path.join(src, "tokenizer"), os.path.join(out, "tokenizer"), dirs_exist_ok=True)
    shutil.copytree(os.path.join(src, "encoder"), os.path.join(out, "encoder"), dirs_exist_ok=True)
    shutil.copy2(os.path.join(src, "rl_agent_config.json"), os.path.join(out, "rl_agent_config.json"))
    if os.path.abspath(os.path.join(src, "model.safetensors")) != os.path.join(out, "model.safetensors"):
        shutil.copy2(os.path.join(src, "model.safetensors"), os.path.join(out, "model.safetensors"))

    if a.check_pairs:
        spec["_out"] = out
        check_sequences(spec, agent, task, a.check_pairs)
        del spec["_out"]

    if not a.no_onnx:
        onnx_path = os.path.join(out, "model.onnx")
        export_graph(agent, spec, onnx_path)
        spec["onnx_logit_diff"] = smoke_compare(agent, spec, onnx_path)
        spec["onnx_sha256"] = sha256_of(onnx_path)

    with open(os.path.join(out, "question.json"), "w", encoding="utf-8", newline="\n") as f:
        json.dump(spec, f, ensure_ascii=False, indent=1)
    print(f"question.json 씀 · 모델 {weights_sha[:8]} · 머리 {len(head_ids)} 토큰 · 표지 {markers}", flush=True)


if __name__ == "__main__":
    main()
