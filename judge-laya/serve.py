# Laya 판정 서버 — POST /judge {"evidence","claim"} → {"a","b","c","model","ms"} · GET /health. 127.0.0.1 에만 붙는다.
# 기본은 ONNX Runtime 판(onnxruntime·tokenizers·numpy 만). --backend torch 는 학습용 venv 에서만 돈다. README.md 참고.
import argparse
import json
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import numpy as np

HOST = "127.0.0.1"
MAX_BODY = 1 << 20          # 1MB — 근거 한 덩이로 넉넉하다. 더 크면 413
READ_TIMEOUT = 10           # 초 — 몸통을 덜 보내고 멈춘 손님이 스레드를 붙잡지 않게
DEFAULT_DIR = os.path.join("~", ".aimemory", "laya", "laya-v2")


class OnnxJudge:
    """question.json 의 머리 열에 상태 토큰을 붙여 ONNX 한 번 돌린다. laya build_sequence 와 같은 셈."""

    def __init__(self, model_dir, spec, threads):
        import onnxruntime as ort
        from tokenizers import Tokenizer

        self.spec = spec
        self.tokenizer = Tokenizer.from_file(os.path.join(model_dir, "tokenizer", "tokenizer.json"))
        opts = ort.SessionOptions()
        opts.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
        opts.intra_op_num_threads = threads
        opts.inter_op_num_threads = 1
        self.session = ort.InferenceSession(os.path.join(model_dir, "model.onnx"), sess_options=opts,
                                            providers=["CPUExecutionProvider"])
        markers = [m for m in spec["markers"] if m < spec["max_len"]]
        self.markers = np.array([markers], dtype=np.int64)
        self.marker_mask = np.ones((1, len(markers)), dtype=bool)
        self.qtype = np.array([spec["qtype"]], dtype=np.int64)

    def probs(self, evidence, claim):
        spec = self.spec
        keys = spec["state_keys"]
        text = json.dumps({keys[0]: evidence, keys[1]: claim}, ensure_ascii=False)
        state_ids = self.tokenizer.encode(text.replace(spec["mask_token"], " "), add_special_tokens=False).ids
        head = spec["head_ids"]
        room = max(0, spec["max_len"] - len(head) - 1)
        # export_onnx.numpy_sequence 와 같은 셈이어야 한다 (자르기·표지 거름까지)
        ids = (head + state_ids[:room] + [spec["sep_id"]])[: spec["max_len"]]
        feeds = {
            "input_ids": np.array([ids], dtype=np.int64),
            "attention_mask": np.ones((1, len(ids)), dtype=np.int64),
            "marker_pos": self.markers,
            "marker_mask": self.marker_mask,
            "qtype": self.qtype,
        }
        logits = self.session.run(["logits"], feeds)[0][0] / spec["temperature"]
        z = np.exp(logits - logits.max())
        return z / z.sum()


class TorchJudge:
    """물러설 길 — laya 패키지(torch CPU)로 같은 물음을 묻는다. RSS 약 2.3GB."""

    def __init__(self, model_dir, spec, threads):
        import torch
        import laya

        torch.set_num_threads(threads)
        self.spec = spec
        self.agent = laya.load(model_dir, device="cpu")
        self.question = {"q": spec["laya_question"]}
        self.ids = [o["id"] for o in spec["options"]]

    def probs(self, evidence, claim):
        keys = self.spec["state_keys"]
        r = self.agent.predict({keys[0]: evidence, keys[1]: claim}, self.question)
        p = r["answers"]["q"]["probabilities"]
        return np.array([float(p[i]) for i in self.ids])


def make_handler(judge, spec, model_id, backend):
    letters = [o["letter"] for o in spec["options"]]
    lock = threading.Lock()  # 한 번에 한 판 — 스레드 수는 --threads 가 정한다

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"
        timeout = READ_TIMEOUT

        def log_message(self, fmt, *args):
            return

        def reply(self, code, body):
            data = json.dumps(body, ensure_ascii=False).encode("utf-8")
            self.send_response(code)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(data)))
            if code != 200:
                # 안 읽은 몸통이 남았을 수 있다 — 연결을 이어 쓰면 그 몸통이 다음 요청으로 읽힌다
                self.send_header("Connection", "close")
                self.close_connection = True
            self.end_headers()
            self.wfile.write(data)

        def do_GET(self):
            if self.path != "/health":
                self.reply(404, {"error": "not found"})
                return
            self.reply(200, {"ok": True, "model": model_id, "backend": backend})

        def do_POST(self):
            if self.path != "/judge":
                self.reply(404, {"error": "not found"})
                return
            try:
                length = int(self.headers.get("Content-Length") or 0)
            except ValueError:
                length = -1
            if length <= 0 or length > MAX_BODY:
                self.reply(413, {"error": "body size"})
                return
            try:
                req = json.loads(self.rfile.read(length).decode("utf-8"))
            except (UnicodeDecodeError, json.JSONDecodeError):
                self.reply(400, {"error": "bad json"})
                return
            if not isinstance(req, dict) or not isinstance(req.get("evidence"), str) or not isinstance(req.get("claim"), str):
                self.reply(400, {"error": "need evidence and claim strings"})
                return
            start = time.perf_counter()
            try:
                with lock:
                    p = judge.probs(req["evidence"], req["claim"])
            except Exception as err:  # 경계 — 판정 하나가 죽어도 서버는 산다. 까닭은 stderr 로
                print(f"judge 실패: {type(err).__name__}: {err}", file=sys.stderr, flush=True)
                self.reply(500, {"error": "judge failed"})
                return
            ms = (time.perf_counter() - start) * 1000
            if len(p) != len(letters) or not np.all(np.isfinite(p)):
                self.reply(500, {"error": "bad probabilities"})
                return
            body = {letter: round(float(v), 6) for letter, v in zip(letters, p)}
            body["model"] = model_id
            body["ms"] = round(ms, 1)
            self.reply(200, body)

    return Handler


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser(description="Laya 판정 서버 (127.0.0.1)")
    ap.add_argument("--model", default=os.environ.get("LAYA_MODEL_DIR", DEFAULT_DIR), help="서빙 폴더 (export_onnx.py 결과)")
    ap.add_argument("--port", type=int, default=8091)
    ap.add_argument("--threads", type=int, default=4)
    ap.add_argument("--backend", choices=["onnx", "torch"], default="onnx")
    a = ap.parse_args()

    model_dir = os.path.abspath(os.path.expanduser(a.model))
    spec_path = os.path.join(model_dir, "question.json")
    if not os.path.exists(spec_path):
        sys.exit(f"question.json 없음: {spec_path} — export_onnx.py 로 먼저 내보낸다")
    spec = json.load(open(spec_path, encoding="utf-8"))
    model_id = spec["weights_sha256"][:8]

    t0 = time.time()
    if a.backend == "onnx":
        judge = OnnxJudge(model_dir, spec, a.threads)
    else:
        judge = TorchJudge(model_dir, spec, a.threads)
    judge.probs("데우기", "데우기")
    print(f"laya {a.backend} 판 · 모델 {model_id} · 올림 {time.time() - t0:.1f}s · http://{HOST}:{a.port}", flush=True)

    server = ThreadingHTTPServer((HOST, a.port), make_handler(judge, spec, model_id, a.backend))
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
