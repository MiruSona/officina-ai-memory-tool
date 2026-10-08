# mem 저장소(Memory/store/YYYY/MM/*.md)를 읽어 기억 하나를 dict 로 돌려준다.
# 근거 문장을 뽑는 공용 부품이다. freeze.py 와 synth.py 가 같이 쓴다.
# 이 파일은 코드만 담는다. 읽은 기억 본문은 git 밖(~/.laya-local 등)에만 쓴다.
import os
import re
import glob

# OFFICINA 가 없으면 지금 폴더를 스튜디오 뿌리로 본다 (스튜디오 뿌리에서 돌린다)
STORE = os.path.join(
    os.environ.get("OFFICINA", os.getcwd()),
    "Memory", "store",
)

# 문장으로 안 치는 줄 — 표·머리글·코드펜스·링크 줄
_SKIP_LINE = re.compile(r"^\s*(\||#|```|<|>|!\[|\[.*\]\(|---)")
# 글머리표·번호 앞머리
_BULLET = re.compile(r"^\s*(?:[-*]|\d+[.)]|[①-⑩])\s*")
# 문장 끝 — 마침표·물음표·느낌표 뒤 공백 (「다.」 「다 .」 둘 다)
_SENT_END = re.compile(r"(?<=[.!?])\s+")


def parse_file(path):
    """md 파일 하나 → {id, type, scope, title, summary, body, sentences}. 머리말이 없으면 None."""
    text = open(path, encoding="utf-8").read()
    if not text.startswith("---"):
        return None
    end = text.find("\n---", 3)
    if end < 0:
        return None
    head = text[3:end]
    body = text[end + 4:].strip()
    meta = {}
    for line in head.splitlines():
        m = re.match(r"^(\w+):\s*(.*)$", line)
        if m:
            meta[m.group(1)] = m.group(2).strip()
    item = {
        "id": meta.get("id", os.path.basename(path)[:-3]),
        "type": meta.get("type", ""),
        "scope": meta.get("scope", ""),
        "title": meta.get("title", ""),
        "summary": meta.get("summary", ""),
        "body": body,
        "path": path,
    }
    item["sentences"] = split_sentences(body)
    return item


def split_sentences(body, lo=20, hi=200):
    """본문을 문장으로 자른다. 표·머리글은 버리고 길이 lo~hi 글자만 남긴다."""
    out = []
    for raw in body.splitlines():
        if not raw.strip() or _SKIP_LINE.match(raw):
            continue
        line = _BULLET.sub("", raw).strip()
        for s in _SENT_END.split(line):
            s = s.strip()
            if lo <= len(s) <= hi and _looks_like_sentence(s):
                out.append(s)
    return out


def _looks_like_sentence(s):
    """코드·경로 덩어리처럼 보이는 조각은 거른다."""
    if s.count("`") >= 4:
        return False
    hangul = sum(1 for ch in s if "가" <= ch <= "힣")
    if hangul < len(s) * 0.3 or len(s.split()) < 5:
        return False
    # 「근거 : …」 「출처 : …」 같은 꼬리표 줄은 문장이 아니다
    return not re.match(r"^(근거|출처|참고|보기|예)\s*:", s)


def load_all(exclude_ids=(), store=STORE):
    """저장소 전체를 읽는다. exclude_ids 에 든 기억은 뺀다."""
    exclude = set(exclude_ids)
    items = []
    for path in sorted(glob.glob(os.path.join(store, "*", "*", "*.md"))):
        it = parse_file(path)
        if it and it["id"] not in exclude:
            items.append(it)
    return items


def read_ids(path):
    """exclude_ids.txt 같은 한 줄 한 id 파일을 읽는다. # 주석·빈 줄은 건너뛴다."""
    ids = []
    if not os.path.exists(path):
        return ids
    for line in open(path, encoding="utf-8"):
        line = line.strip()
        if line and not line.startswith("#"):
            ids.append(line)
    return ids
