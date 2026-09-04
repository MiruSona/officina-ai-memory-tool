# 정적 임베딩 표 굽기 (model2vec 방식) — 설계 4-8 · 조사 B 4절
#
# 하는 일은 셋뿐이다.
#   ① 어휘의 조각 하나하나를 선생 모델에 한 번씩 통과시켜 벡터를 얻는다
#   ② PCA 로 384 → 128 차원으로 줄인다
#   ③ int8 로 양자화해서 우리 꼴 `ko.bin` 으로 쓴다
#
# 학습 자료가 필요 없다. 어휘를 **우리 토크나이저가 낸 조각**으로 못박았기
# 때문에 Go 쪽에는 토크나이저 구현이 하나도 안 든다.
#
# 쓰는 법 :
#   python distill.py --vocab vocab.tsv --model <선생 폴더> --out ko.bin
#
# ko.bin 꼴 (전부 리틀엔디언) :
#   "MEMKOBIN"        8바이트 표식
#   u32 version       판 (지금 1)
#   u32 dim           차원
#   u32 count         어휘 수
#   f32 scale         실수값 = int8 × scale
#   어휘              조각마다 u8 바이트수 + UTF-8 바이트
#   f32 × count       조각마다 가중치 (SIF)
#   i8 × count × dim  벡터 행렬

import argparse
import struct
import sys

import numpy as np
import torch
from transformers import AutoModel, AutoTokenizer

MAGIC = b"MEMKOBIN"
VERSION = 1


def read_vocab(path, limit):
    """`<조각>\\t<횟수>` 를 읽어 많이 나온 차례로 자른다."""
    rows = []
    with open(path, encoding="utf-8") as file:
        for line in file:
            line = line.rstrip("\n")
            if not line or line.startswith("#"):
                continue
            parts = line.split("\t")
            if len(parts) != 2:
                continue
            piece, times = parts[0], int(parts[1])
            # 글자·숫자가 하나도 없는 조각(`|`, `---`)은 뜻이 없다.
            if not any(ch.isalnum() for ch in piece):
                continue
            if len(piece.encode("utf-8")) > 255:
                continue
            rows.append((piece, times))
    rows.sort(key=lambda row: (-row[1], row[0]))
    return rows[:limit]


def encode(pieces, model_dir, prefix="", batch=256):
    """조각마다 선생 모델의 문장 벡터(마스크 평균)를 얻는다.

    prefix 는 선생이 요구하는 머리말이다. e5 계열은 `query: ` 를 늘 붙여
    학습했기 때문에 안 붙이면 배운 적 없는 입력이 된다.
    """
    tokenizer = AutoTokenizer.from_pretrained(model_dir)
    model = AutoModel.from_pretrained(model_dir).eval()
    out = []
    with torch.no_grad():
        for start in range(0, len(pieces), batch):
            chunk = [prefix + piece for piece in pieces[start:start + batch]]
            got = tokenizer(chunk, padding=True, truncation=True,
                            max_length=16, return_tensors="pt")
            hidden = model(**got).last_hidden_state
            mask = got["attention_mask"].unsqueeze(-1).float()
            pooled = (hidden * mask).sum(1) / mask.sum(1).clamp(min=1e-9)
            out.append(pooled.numpy().astype(np.float32))
            done = min(start + batch, len(pieces))
            print(f"\r  선생 통과 {done}/{len(pieces)}", end="", file=sys.stderr)
    print(file=sys.stderr)
    return np.concatenate(out, axis=0)


def pca(matrix, dim):
    """평균을 빼고 특잇값 분해로 차원을 줄인다. sklearn 없이 numpy 만 쓴다."""
    centered = matrix - matrix.mean(axis=0, keepdims=True)
    _, _, right = np.linalg.svd(centered, full_matrices=False)
    return centered @ right[:dim].T


def sif_weights(counts, a=1e-3):
    """자주 나오는 조각은 뜻을 덜 가른다. 흔한 것을 눌러 준다 (SIF)."""
    total = float(counts.sum())
    probability = counts.astype(np.float64) / max(total, 1.0)
    return (a / (a + probability)).astype(np.float32)


def write_bin(path, pieces, weights, matrix):
    scale = float(np.abs(matrix).max()) / 127.0
    if scale <= 0:
        scale = 1.0
    quantized = np.clip(np.rint(matrix / scale), -127, 127).astype(np.int8)
    with open(path, "wb") as file:
        file.write(MAGIC)
        file.write(struct.pack("<III f", VERSION, matrix.shape[1], len(pieces), scale))
        for piece in pieces:
            raw = piece.encode("utf-8")
            file.write(struct.pack("<B", len(raw)))
            file.write(raw)
        file.write(weights.astype("<f4").tobytes())
        file.write(quantized.tobytes())
    return quantized, scale


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--vocab", required=True, help="Go vocab 도구가 낸 tsv")
    parser.add_argument("--model", required=True, help="선생 모델 폴더")
    parser.add_argument("--out", required=True)
    parser.add_argument("--dim", type=int, default=128)
    parser.add_argument("--max-vocab", type=int, default=30000)
    parser.add_argument("--prefix", default="query: ",
                        help="선생에게 넣을 때 조각 앞에 붙일 말 (e5 계열은 `query: `)")
    args = parser.parse_args()

    rows = read_vocab(args.vocab, args.max_vocab)
    pieces = [row[0] for row in rows]
    counts = np.array([row[1] for row in rows], dtype=np.float64)
    print(f"어휘 {len(pieces)} 개", file=sys.stderr)

    raw = encode(pieces, args.model, args.prefix)
    reduced = pca(raw, args.dim)
    norms = np.linalg.norm(reduced, axis=1, keepdims=True)
    reduced = reduced / np.clip(norms, 1e-9, None)

    weights = sif_weights(counts)
    quantized, scale = write_bin(args.out, pieces, weights, reduced)
    size = 8 + 16 + sum(len(p.encode("utf-8")) + 1 for p in pieces) + 4 * len(pieces) + quantized.size
    print(f"{args.out} : 어휘 {len(pieces)} · 차원 {args.dim} · 계수 {scale:.6g} · "
          f"{size / 1024 / 1024:.2f} MB", file=sys.stderr)


if __name__ == "__main__":
    main()
