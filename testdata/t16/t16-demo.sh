#!/bin/sh
# T16 실연 — 새 빈 프로젝트에 서로 아주 다른 기억 3건.
#
# 쓰는 법 : sh t16-demo.sh <mem.exe> <저장소 폴더(Memory 의 부모)>
#
# 재는 것 셋 : `add` 3건이 다 exit 0 · `lint` 오류 0 · `duplicate-hard` 0.
# **기억 3건의 원문이 이 스크립트 안에 있다** — v0.3 은 「서로 아주 다른 셋」이라고만
# 적어 두어 다음 판이 같은 글을 못 쓰고 다시 지었고, 그래서 경고 건수를 1:1 로
# 못 견줬다 (v0.4 리뷰 A R9). 글을 고치면 다음 판과의 비교도 끊긴다.
#
# 저장소는 이 스크립트가 만든다. scope 하나(client)와 태그 여섯을 먼저 넣는다 —
# 안 넣으면 태그 표준이 「배우는 중」이라 값이 달라진다.
MEM="$1"
ROOT="$2"
if [ -z "$MEM" ] || [ -z "$ROOT" ]; then
  echo "쓰는 법 : sh t16-demo.sh <mem.exe> <저장소 폴더>"
  exit 1
fi

"$MEM" init --repo "$ROOT" --no-hook > /dev/null || exit 1
REPO="$ROOT/Memory"
"$MEM" tags --add-scope client --repo "$REPO" > /dev/null
for t in art texture data save build pipeline; do
  "$MEM" tags --add $t --repo "$REPO" > /dev/null
done

"$MEM" add --repo "$REPO" --type decision --title "아틀라스는 화면 단위로 자른다" \
  --summary "아틀라스를 화면 단위로 잘라 한 화면에서 쓰는 그림이 한 장에 모이게 한다" \
  --tags art,texture --scope client --sources note:아트회의2026-08-01 \
  --body "결론 : 아틀라스는 화면 단위로 자른다. 한 장은 2048×2048 이다.
왜 : 한 화면에서 쓰는 그림이 여러 장에 흩어지면 그리기 호출이 는다.
주의 : 화면을 넘나드는 그림은 공용 장에 따로 모은다." > /dev/null
echo "add1 = $?"

"$MEM" add --repo "$REPO" --type caution --title "세이브 파일은 덮어쓰기 전에 백업한다" \
  --summary "세이브 파일을 덮어쓰기 전에 한 벌 옮겨 두어야 도중에 꺼져도 안 잃는다" \
  --tags data,save --scope client --severity high --sources note:저장검토2026-08-02 \
  --body "결론 : 세이브 파일은 덮어쓰기 전에 한 벌 옮겨 둔다. 옮긴 벌은 3분 뒤에 지운다.
왜 : 쓰는 도중에 전원이 꺼지면 반쯤 쓰인 파일만 남는다.
주의 : 옮긴 벌은 다음 저장이 끝난 뒤에 지운다." > /dev/null
echo "add2 = $?"

"$MEM" add --repo "$REPO" --type howto --title "빌드를 한 줄로 돌리는 차례" \
  --summary "빌드를 한 줄 명령으로 돌리기까지 밟는 차례를 넷으로 적어 둔다" \
  --tags build,pipeline --scope client \
  --body "1. 받아 온 자료를 짧은 경로에 푼다.
2. 설정 파일에서 대상 기계를 고른다.
3. 명령 한 줄로 빌드를 돌린다.
4. 나온 파일을 손으로 한 번 열어 본다." > /dev/null
echo "add3 = $?"

"$MEM" index --full --repo "$REPO" > /dev/null
echo "index = $?"
"$MEM" search 아틀라스 --repo "$REPO" > /dev/null
echo "search = $?"
"$MEM" lint --repo "$REPO"
echo "lint = $?"
