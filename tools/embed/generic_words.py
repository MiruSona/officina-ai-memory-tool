# 범용 어휘 뽑기 — 선생 모델 토크나이저에서 한글·영문 조각을 꺼낸다.
#
# 왜 필요한가 : 우리 기억 말뭉치(206건 + 실데이터 40건)에서 나오는 조각은
# 1만 개뿐이다. 그런데 **질의는 저장소에 없는 말로 들어온다**(`인덱싱`,
# `느려지나`). 표에 없는 조각은 벡터가 없어 뜻을 못 재므로, 한국어·영어에서
# 자주 쓰는 조각을 미리 깔아 둬야 한다.
#
# 어디서 가져오나 : 선생 모델(XLM-R 계열)의 sentencepiece 어휘는 100개 언어
# 실제 글뭉치에서 배운 것이라 **자주 쓰는 조각이 앞 번호에 온다.** 그 어휘에서
# 순한글 조각과 순영문 조각만 꺼내면 공짜로 얻는 빈출 목록이 된다.
#
# 내는 꼴은 `<낱말> <어림 빈도>` 다. 쪼개기는 우리가 안 한다 —
# `go run ./tools/embed/vocab -words <이 파일>` 이 우리 토크나이저로 쪼갠다.

import argparse
import os
import sys

from tokenizers import Tokenizer


def is_hangul(text):
    return all("가" <= ch <= "힣" for ch in text)


def is_latin(text):
    return all(("a" <= ch <= "z") or ("A" <= ch <= "Z") for ch in text)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--tokenizer", required=True, help="tokenizer.json 경로")
    parser.add_argument("--out", required=True)
    parser.add_argument("--latin-max", type=int, default=12000,
                        help="영문 조각을 몇 개까지 담을지 (번호가 빠른 것부터)")
    args = parser.parse_args()

    tokenizer = Tokenizer.from_file(args.tokenizer)
    vocab = tokenizer.get_vocab()
    rows = []
    latin = 0
    for piece, number in sorted(vocab.items(), key=lambda pair: pair[1]):
        plain = piece.replace("▁", "").strip()
        if len(plain) < 2:
            continue
        if is_hangul(plain):
            pass
        elif is_latin(plain):
            if latin >= args.latin_max:
                continue
            latin += 1
            plain = plain.lower()
        else:
            continue
        # 어림 빈도 : 어휘 번호가 빠를수록 자주 쓰는 조각이다 (Zipf).
        rows.append((plain, max(1, int(1_000_000 / (number + 100)))))

    os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
    with open(args.out, "w", encoding="utf-8") as file:
        file.write("# 선생 토크나이저에서 뽑은 범용 한글·영문 조각\n")
        for plain, times in rows:
            file.write(f"{plain} {times}\n")
    print(f"조각 {len(rows)} 줄 (영문 {latin}) 을 {args.out} 에 적었다", file=sys.stderr)


if __name__ == "__main__":
    main()
