#!/bin/sh
# add 관문 실연 — 좋은 기억 10 · 나쁜 기억 21.
#
# 쓰는 법 : sh gate-demo.sh <mem.exe> <저장소 Memory 폴더>
#
# 좋은 10 은 다 통과해야 하고(오거절 0), 나쁜 21 은 많이 막을수록 좋다.
# 좋은 것은 재고 나서 진짜로 넣는다 — 뒤에 올 나쁜 것이 견줄 이웃이 있어야 한다.
#
# **저장소는 부르는 쪽이 미리 만든다.** scope 둘(aimemorytool·officina)을 표준으로
# 넣고, 태그 표준도 한 번 손대 둬야 한다 (`mem tags --add <태그>`). 안 그러면
# 태그 규칙이 「배우는 중」이라 경고로만 나서 B06 이 안 막힌다.
#
#   mem init --repo <프로젝트> --no-hook
#   mem tags --add-scope aimemorytool --repo <프로젝트>/Memory
#   mem tags --add-scope officina     --repo <프로젝트>/Memory
#   for t in index design cli doc bug hook concurrency test quality gc search effort; do
#     mem tags --add $t --repo <프로젝트>/Memory
#   done
#
# 태그를 한 번이라도 넣으면 태그 표준이 「배우는 중」에서 벗어나 목록 밖 태그가
# 거절이 된다. 그래서 **좋은 10 이 쓰는 태그를 다 넣어 둬야** 오거절이 안 난다.
# 안 넣으면 좋은 것이 5/10 만 지나가고, 그것은 관문 탓이 아니라 준비 탓이다.
#
# 2026-08-23 리뷰 C 에서 자료를 갈았다 :
#   B06 태그가 `tilemap`·`levelgen` 이었는데 둘 다 씨앗 vocab 의 표준 하위 태그가 돼
#       더는 「표준 밖 태그」가 아니었다 → 정말 목록에 없는 낱말로 바꿨다.
#   B24 제목이 36자라 6~40 안이었다 → 정말 40자를 넘게 늘렸다.
MEM="$1"
REPO="$2"
GOOD=0; GOODWARN=0; BAD=0

run() {   # run <good|bad> <이름> <옵션들…>
  kind="$1"; name="$2"; shift 2
  out=$("$MEM" add --check --json --repo "$REPO" "$@" 2>&1)
  code=$?
  if [ "$kind" = good ]; then
    if [ $code -eq 0 ]; then GOOD=$((GOOD+1)); else echo "  오거절 $name : $out"; fi
    case "$out" in *duplicate*) GOODWARN=$((GOODWARN+1)); echo "  좋은데 중복 경고 $name" ;; esac
    # 재고 나서 진짜로 넣는다 — 뒤에 올 나쁜 22 가 견줄 이웃이 있어야 한다.
    "$MEM" add --repo "$REPO" "$@" >/dev/null 2>&1; "$MEM" index --repo "$REPO" >/dev/null 2>&1
  else
    if [ $code -ne 0 ]; then BAD=$((BAD+1)); else echo "  못 막음 $name"; fi
  fi
}

B="본문 첫 줄에 결론을 쓴다. 여기서 정한 것은 아래와 같다.
둘째 줄에 왜 그렇게 했는지 근거를 적는다.
셋째 줄에 다음에 볼 사람이 알아야 할 주의를 적는다."

echo "== 좋은 기억 10 =="
run good G01 --type decision --title "색인 스키마를 세 번째 판으로 올린다" --summary "색인 스키마를 v3 으로 올리고 마이그레이션은 한 번만 돌게 못 박는다" --tags index,design --scope aimemorytool --sources note:설계9절 --body "결론 : 스키마를 v3 으로 올린다.
왜 : 빠진 열을 채우지 않으면 정렬을 못 한다.
주의 : 마이그레이션은 한 번만 돌고 다시 부르면 아무 일도 안 한다."
run good G02 --type howto --title "새 기계에서 처음 깔 때 하는 일" --summary "새 기계에서 도구를 처음 깔 때 밟는 차례를 넷으로 적어 둔다" --tags cli,doc --scope aimemorytool --body "1. 저장소를 받아 온다.
2. 명령 하나로 실행 파일을 만든다.
3. 설치 명령을 한 번 돌린다.
4. 첫 기억을 하나 넣어 본다."
run good G03 --type issue --title "긴 경로에서 설치가 멈추는 일" --summary "경로가 아주 길면 설치가 중간에 멈춘다. 짧은 자리에 풀면 지나간다" --tags bug,cli --scope aimemorytool --severity mid --sources note:설치로그 --body "## 증상
설치가 마지막 단계에서 멈춘다.
로그 끝 줄에 경로가 길다는 말이 남는다.
같은 자료를 짧은 경로에 두면 안 멈춘다.

## 해결
짧은 자리에 풀고 다시 돌린다.
그 뒤로는 다시 안 났다.
긴 경로는 아예 피하는 편이 낫다."
run good G04 --type caution --title "훅 안에서 무거운 일을 하지 않는다" --summary "세션이 열릴 때 도는 자리에서는 저장소 전체를 훑는 일을 하지 않는다" --tags hook,concurrency --scope aimemorytool --severity high --sources note:훅설계 --body "결론 : 훅 안에서는 무거운 일을 안 한다.
왜 : 세션이 열릴 때 사람을 기다리게 하면 도구를 안 쓰게 된다.
주의 : 시간이 넘으면 그 자리에서 그만두고 조용히 나간다."
run good G05 --type history --title "규모 시험에서 걸린 세 자리" --summary "큰 말뭉치로 돌려 보고 나서야 드러난 느린 자리 셋을 적어 둔다" --tags test,quality --scope aimemorytool --body "결론 : 규모 시험을 안 하면 못 보는 자리가 있었다.
첫째는 짝마다 집합을 다시 만드는 자리였다.
둘째는 후보를 자를 때 자를 다시 재는 자리였다.
셋째는 안 읽는 색인을 만드는 자리였다."
run good G06 --type decision --title "자동 삭제는 만들지 않는다" --summary "도구가 스스로 기억을 지우는 길은 아예 만들지 않고 접어 두기만 한다" --tags gc,design --scope aimemorytool --sources note:설계5절 --body "결론 : 지우는 코드 경로를 만들지 않는다.
왜 : 되돌릴 수 없는 자동 조치는 사람이 도구를 못 믿게 만든다.
주의 : 정리는 옮기기까지만 하고 되돌리기를 늘 남긴다."
run good G07 --type todo --title "큰 저장소에서 정리 시간 재기" --summary "이만 건 규모에서 정리 명령이 얼마나 걸리는지 아직 안 쟀다. 다음 판에 잰다" --tags test,effort --scope aimemorytool --todo-status open --body "결론 : 큰 저장소에서의 시간은 아직 모른다.
왜 : 지금까지는 백여 건 규모에서만 봤다.
할 것 : 자료를 만들어 한 번 돌리고 시간을 적는다."
run good G08 --type howto --title "기억을 찾을 때 쓰는 세 가지 길" --summary "낱말로 찾기, 종류로 좁히기, 이어진 기억 따라가기 세 길을 차례로 쓴다" --tags search,doc --scope aimemorytool --body "1. 먼저 낱말 두셋으로 찾는다.
2. 너무 많으면 종류로 좁힌다.
3. 그래도 못 찾으면 이어진 기억을 따라간다."
run good G09 --type issue --title "같은 낱말 하나가 결과를 죽이는 일" --summary "안 걸리는 낱말이 하나 끼면 교집합이 비어 결과가 통째로 0건이 된다" --tags search,bug --scope aimemorytool --severity mid --sources note:검색기록 --body "## 증상
낱말 셋으로 찾으면 0건이 나온다.
그중 둘로만 찾으면 잘 나온다.
남은 하나는 저장소에 아예 없는 말이었다.

## 해결
후보가 하나도 없는 낱말은 교집합에서 뺀다.
뺐다는 것을 사람에게 알린다.
그 뒤로 0건이 크게 줄었다."
run good G10 --type decision --title "설정과 배선은 코드에 둔다" --summary "눈으로만 볼 수 있는 배선은 만들지 않고 설정과 연결을 코드에 적어 둔다" --tags design,doc --scope officina --sources note:구조조사 --body "결론 : 배선을 코드에 둔다.
왜 : 화면에서만 알 수 있는 연결은 도구가 못 읽는다.
주의 : 자료 통은 수치만 담고 사건 배선에는 쓰지 않는다."

"$MEM" index --repo "$REPO" >/dev/null 2>&1
echo "== 나쁜 기억 21 =="
run bad B01 --type decision --title "정리" --summary "제목이 너무 짧아서 무엇에 대한 기억인지 알 수 없는 경우를 본뜬 것이다" --tags gc,design --scope aimemorytool --sources note:x --body "$B"
run bad B02 --type decision --title "색인 스키마를 세 번째 판으로" --summary "색인 스키마를 세 번째 판으로 올리고 마이그레이션은 한 번만 돌게 못 박는다" --tags index,design --scope aimemorytool --sources note:x --body "$B"
run bad B03 --type decision --title "정리 명령 쿨다운 정하기" --summary "쿨다운 24시간" --tags gc,design --scope aimemorytool --sources note:x --body "$B"
run bad B04 --type decision --title "태그를 하나만 붙인 기억" --summary "태그가 하나뿐이라 나중에 이 기억을 찾을 길이 좁아지는 경우를 본뜬 것이다" --tags gc --scope aimemorytool --sources note:x --body "$B"
run bad B05 --type decision --title "출처를 태그로 적은 기억" --summary "출처를 태그 자리에 적어서 태그가 분류 구실을 못 하게 된 경우를 본뜬 것이다" --tags impl,poc --scope aimemorytool --sources note:x --body "$B"
run bad B06 --type decision --title "표준 밖 태그를 쓴 기억" --summary "표준 목록에 없는 태그를 그냥 붙여서 나중에 아무도 못 찾게 되는 경우다" --tags mygamefeature,spritepacker --scope aimemorytool --sources note:x --body "$B"
run bad B07 --type decision --title "표준 밖 범위를 쓴 기억" --summary "범위 이름을 표준 목록 밖으로 지어서 저장소가 갈라지는 경우를 본뜬 것이다" --tags gc,design --scope mygameproject --sources note:x --body "$B"
run bad B08 --type decision --title "근거 없이 정한 결정 기억" --summary "무엇을 정했는지는 있는데 왜 그렇게 정했는지 근거가 하나도 없는 경우다" --tags gc,design --scope aimemorytool --body "$B"
run bad B10 --type issue --title "세기를 안 적은 문제 기억" --summary "문제를 적으면서 얼마나 급한지를 안 적어 우선순위를 못 매기는 경우다" --tags bug,cli --scope aimemorytool --sources note:x --body "## 증상
무언가 잘 안 된다.
언제 나는지도 모른다.
다시 내는 길도 모른다.

## 해결
아직 없다.
더 봐야 한다.
다음 판으로 미룬다."
run bad B11 --type todo --title "할 일인데 상태를 안 적었다" --summary "할 일을 적으면서 시작했는지 끝났는지를 안 적어 아무도 못 이어받는 경우다" --tags test,effort --scope aimemorytool --body "$B"
run bad B12 --type history --title "본문이 한 줄뿐인 기억" --summary "본문이 한 줄뿐이라 여섯 달 뒤에 읽으면 아무것도 알 수 없는 경우를 본뜬 것" --tags test,quality --scope aimemorytool --body "고쳤다."
run bad B13 --type decision --title "결정 다섯 개를 한 표에 넣었다" --summary "서로 다른 결정 다섯 개를 표 하나에 몰아넣어 하나가 낡으면 다 못 믿는 경우" --tags design,quality --scope aimemorytool --sources note:x --body "| 무엇 | 정한 것 |
| --- | --- |
| 색인 | 세 번째 판으로 올린다 |
| 정리 | 하루에 한 번만 돈다 |
| 훅 | 무거운 일을 안 한다 |
| 검색 | 낱말 여덟 개까지만 본다 |
| 보안 | 값을 안 찍는다 |"
run bad B14 --type decision --title "요약 한 줄에 결정 셋" --summary "색인은 올리고 정리는 하루 한 번 돌리며 훅은 무거운 일을 안 하기로 정했다" --tags design,quality --scope aimemorytool --sources note:x --body "$B"
run bad B18 --type history --title "오늘 이것저것 손본 기록" --summary "오늘은 여기저기 조금씩 손봤고 다음에 무엇을 할지는 아직 정하지 못했다" --tags test,quality --scope aimemorytool --body "$B"
run bad B19 --type history --title "본문에 열쇠가 그대로 든 기억" --summary "본문에 열쇠처럼 보이는 글자가 그대로 들어가 저장소에 남는 경우를 본뜬 것" --tags security,bug --scope aimemorytool --body "결론 : 연동을 붙였다.
열쇠는 ghp_A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8 였다.
주의 : 다음에는 환경 변수로 뺀다."
run bad B20 --type history --title "어제와 지난주로 적은 기록" --summary "날짜를 어제 지난주처럼 적어서 나중에 읽으면 언제인지 알 수 없는 경우다" --tags test,quality --scope aimemorytool --body "어제 고쳤다. 지난주에 났던 문제다.
왜 : 그때는 급했다.
주의 : 다음 주에 다시 본다."
run bad B21 --type howto --title "차례 없이 적은 하는 법" --summary "하는 법인데 차례가 없어서 무엇부터 해야 하는지 알 수 없는 경우를 본뜬 것" --tags cli,doc --scope aimemorytool --body "$B"
run bad B22 --type decision --title "요약과 본문이 딴 이야기" --summary "훅은 세션이 열릴 때만 돌고 저장소 전체를 훑지 않기로 못 박아 정한 것이다" --tags hook,design --scope aimemorytool --sources note:x --body "결론 : 태그 표를 늘렸다.
왜 : 남의 프로젝트가 첫 기억부터 막혔다.
주의 : 씨앗 목록에는 우리 낱말을 안 넣는다."
run bad B23 --type decision --title "빈 절만 있는 결정 기억" --summary "절 제목만 세워 두고 그 아래를 안 채워서 읽을 것이 없는 경우를 본뜬 것이다" --tags design,doc --scope aimemorytool --sources note:x --body "## 결론

## 왜

## 주의
"
run bad B24 --type decision --title "제목이 마흔 자를 훌쩍 넘어가서 한눈에 안 들어오고 목록에서도 뒷부분이 잘려 나가는 아주 긴 제목" --summary "제목이 너무 길어서 목록에서 한눈에 안 들어오는 경우를 본뜬 것이다" --tags design,doc --scope aimemorytool --sources note:x --body "$B"
run bad B25 --type issue --title "절 이름이 규격과 다른 문제" --summary "문제 기억인데 증상과 해결 절이 없어서 무엇이 문제였는지 못 읽는 경우다" --tags bug,quality --scope aimemorytool --sources note:x --body "$B"

echo
echo "좋은 10 중 통과 : $GOOD / 10   (그중 중복 경고 : $GOODWARN)"
echo "나쁜 21 중 거절 : $BAD / 21"
