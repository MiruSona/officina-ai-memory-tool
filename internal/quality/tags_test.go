package quality

import "testing"

// TestStripTagsMatchesPython 은 JudgeModel `train/data_prep.py` strip_tags 와 같은 입력에 같은 답을
// 내는지 본다. 앞 여섯 줄은 Python 시험(`tests/test_data_prep.py` StripTagsTest)을 그대로 옮긴 것이고,
// 나머지 오른쪽 값은 2026-10-11 에 Python 3.14 strip_tags 를 실제로 돌려 받은 답이다.
// 전각 공백 · NBSP · U+2028 · 수직탭은 Go `\s` 가 못 먹는 글자라 따로 넣었다.
func TestStripTagsMatchesPython(t *testing.T) {
	cases := [][2]string{
		{"왜 : 스왑이 밀렸다", "스왑이 밀렸다"},
		{"  무엇을：문턱을 올렸다", "문턱을 올렸다"},
		{"정한 것 : A\n다음에 : B", "A\nB"},
		{"까닭은 왜 : 이다", "까닭은 왜 : 이다"},
		{"왜냐 : 그렇다", "왜냐 : 그렇다"},
		{"다음 판", "다음 판"},
		{"근거: 실측 3번", "실측 3번"},
		{"무엇이 : 바뀌었나", "바뀌었나"},
		{"무슨 일이 : 났나", "났나"},
		{"결론 :   끝", "끝"},
		{"\u3000주의 : 조심", "조심"},
		{"\n\n왜 : 빈 줄 뒤", "빈 줄 뒤"},
		{"왜 : \n근거 : 둘", "둘"},
		{"왜 : 근거 : 사슬", "근거 : 사슬"},
		{"주의 :", ""},
		{"다음 : 이어서\n가운데 왜 : 그대로", "이어서\n가운데 왜 : 그대로"},
		{"확인：끝\r\n어떻게 : 됐나", "끝\r\n됐나"},
		{"\u00a0이후 : 공백", "공백"},
		{"무엇 : 하나", "하나"},
		{"내용 :x", "x"},
		{"왜 ::", ":"},
		{"\u2028왜 : 줄가름", "줄가름"},
		{"\u000b왜 : 수직탭", "수직탭"},
	}
	for _, one := range cases {
		if got := StripTags(one[0]); got != one[1] {
			t.Errorf("StripTags(%q) = %q, Python 답은 %q", one[0], got, one[1])
		}
	}
}
