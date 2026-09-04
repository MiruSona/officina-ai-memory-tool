---
id: 20260820-aaaa1111
type: history
date: 2026-08-20
summary: 이 기억은 lint 규칙 하나를 일부러 어기게 만든 시험용 자료다. 규칙마다 파일 하나씩 둔다
tags: [lint, design]
source: ai
scope: mem-lint
---

락은 하나뿐이다. index 와 gc 와 lint --fix 만 store 를 고쳐 쓴다.
그 밖의 명령은 inbox 에 새 파일만 만든다. rename 은 원자적이라 반쯤 쓰인 파일이 안 보인다.
죽은 락은 rename 으로 먼저 옮기고 성공한 쪽만 새 락을 만든다.
PID 만 보면 재사용된 번호를 산 주인으로 오인하니 시작시각까지 본다.
읽기 명령은 락을 기다리지 않는다. 못 잡으면 지금 있는 색인으로 그냥 읽는다.
