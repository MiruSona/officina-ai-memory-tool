package i18n

// keys 칸(C1)과 A1 뒷정리 둘이 쓰는 말이다 (모음기억설계 4-3).

const (
	BadKeyCount Key = "bad-key-count"
	BadKey      Key = "bad-key"

	// AddAutoNoNew 는 자동 관문 거절 화면에서 `--new`·`--by` 안내 대신 찍는 줄이다.
	AddAutoNoNew Key = "add-auto-no-new"
)

var keysMessages = map[Key]string{
	BadKeyCount: "`keys` 는 %d개까지다. 지금 %d 개다.",
	BadKey:      "`keys` 한 칸은 %d~%d자 한 줄이어야 하고 쉼표를 못 넣는다 : %q",

	AddAutoNoNew: "다음 : 자동 기억은 --new · --by 로 밀어 넣지 못한다. 같은 얘기면 넣지 않고, 다른 얘기면 제목·요약을 그 차이가 보이게 다시 쓴다.",
}

func init() {
	for key, text := range keysMessages {
		messages[key] = text
	}
}
