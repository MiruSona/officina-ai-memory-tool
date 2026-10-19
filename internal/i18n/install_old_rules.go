package i18n

// InstallOldRulesBlocks 는 예전 판 mem init 이 AGENTS.md 에 붙이던 규칙 블록이다.
// init 은 지금 블록이 이 중 하나와 (줄 끝 공백·CRLF 를 빼고) 같을 때만 새 문안으로
// 갈아 끼운다. 어느 것과도 다르면 사람이 고친 블록으로 보고 손대지 않는다
// (2026-10-19 사용자 결정 — 10-10 게임 저장소에서 사람이 더한 두 줄이 덮였다).
//
// **한 번 들어간 판은 고치지 않는다.** 이미 퍼진 파일과 바이트로 맞춰 보는 자료다.
// 경로는 그때 값 그대로 글자로 박는다 — InstallUsagePath 가 바뀌어도 옛 판은 그대로다.
// 문안을 바꿀 때는 바뀌기 전 InstallRulesBlock 을 여기 맨 뒤에 더한다.
//
// 출처는 스튜디오 저장소 이력이다 (서브모듈은 v1.0.0 부터 새 이력이라 옛 판이 없다).
var InstallOldRulesBlocks = []string{
	// 0feb840 (1단계 MVP) — 표식 없이 제목부터 붙였다.
	"## 기억 저장소 (mem)\n\n" +
		"- 지난 결정·이슈해결·주의할점이 필요하면 먼저 `mem search \"낱말\"` 로 찾는다. 문서를 통째로 읽지 않는다.\n" +
		"- 남길 값어치가 있는 것이 생기면 그 자리에서 `mem add --type <todo|history|issue|caution|decision|howto> --scope <범위> --summary \"한 줄\" --tags a,b` 로 넣는다.\n" +
		"- 남길 값어치 = **다음 세션이 다시 물어볼 만한 것.** 파일을 고쳤다는 사실 자체는 git 이 기억하므로 넣지 않는다.\n" +
		"- **코드를 읽으면 아는 것 · git 이 아는 것 · 이미 문서에 있는 것은 넣지 않는다. 기억은 어디에도 안 남는 시행착오·함정·결정만.**\n" +
		"- 옛 결정이 뒤집히면 지우지 말고 `mem invalidate <id> --by <새id>` 로 무효 날짜만 적는다.\n" +
		"- 세션 시작 기억은 훅이 알아서 넣어 준다. `mem` 사용법은 `mem help`.\n",
	// b7f7c0b (MVP 다듬은 판) — 아직 표식 없음.
	"## 기억 저장소 (mem)\n\n" +
		"- 기억(지난 결정·기록·할 일·조사 결과)은 문서가 아니라 `mem` 에 있다. 세션을 시작하면 `Memory/사용법.md` 를 먼저 읽는다.\n" +
		"- 찾을 때 `mem search 낱말 낱말`, 남길 때 `mem add …`. 작업 완료 보고를 쓰기 전에 '남길 것 있나' 를 한 번 본다.\n" +
		"- 자세한 건 `Memory/사용법.md` → `mem help`.\n",
	// v0.1 — 같은 세 줄에 표식을 둘렀다.
	InstallBlockOpen + "\n## 기억 저장소 (mem)\n\n" +
		"- 기억(지난 결정·기록·할 일·조사 결과)은 문서가 아니라 `mem` 에 있다. 세션을 시작하면 `Memory/사용법.md` 를 먼저 읽는다.\n" +
		"- 찾을 때 `mem search 낱말 낱말`, 남길 때 `mem add …`. 작업 완료 보고를 쓰기 전에 '남길 것 있나' 를 한 번 본다.\n" +
		"- 자세한 건 `Memory/사용법.md` → `mem help`.\n" +
		InstallBlockClose + "\n",
	// v0.2 — 다섯 줄 (--hold 줄이 v0.3 에 더해졌다).
	InstallBlockOpen + "\n## 기억 저장소 (mem)\n\n" +
		"- 지난 결정·기록·할 일·조사 결과는 `mem` 에 있다. 세션을 열면 훅이 요약을 밀어 넣는다.\n" +
		"- 찾을 때 `mem search <낱말> <낱말>`, 남길 때 `mem add --type … --title … --tags … --sources …`.\n" +
		"- **팀이 나눠 볼 결정·기록·조사 결과는 `mem` 에 남긴다. 개인 취향·말투는 Claude 내장 기억에 둔다.**\n" +
		"- 결정을 뒤집을 때는 새로 쓰지 말고 `mem set <옛id> --by-new` 로 덮는다.\n" +
		"- 작업 완료 보고를 쓰기 전에 '남길 것 있나' 를 한 번 본다. 자세한 건 `Memory/사용법.md`.\n" +
		InstallBlockClose + "\n",
}
