package lint

import (
	"runtime"
	"strings"
)

// foldCase 는 경로를 대소문자 없이 견줄지다. Windows 파일 시스템은 `Build/A.apk` 와
// `build/a.apk` 를 같은 파일로 본다.
var foldCase = runtime.GOOS == "windows"

// addKnown 은 경로 집합에 한 줄을 넣는다. foldCase 면 소문자 열쇠도 같이 넣는다.
func addKnown(known map[string]bool, slashed string) {
	known[slashed] = true
	if foldCase {
		known[strings.ToLower(slashed)] = true
	}
}

// knownPath 는 경로 집합에 있는지다. 그대로 없으면 소문자로 한 번 더 찾는다.
func knownPath(known map[string]bool, slashed string) bool {
	if known[slashed] {
		return true
	}
	return foldCase && known[strings.ToLower(slashed)]
}

// hasPlaceholder 는 꼴 자리표(`NN`·`YYYY`·`MM`)가 든 경로인지다. 같은 대문자가
// 둘 이상 이어지고 앞뒤가 대문자가 아니면 자리표로 본다 — `vNN.apk` 는 걸리고 `README` 는 안 걸린다.
func hasPlaceholder(target string) bool {
	for start := 0; start < len(target); {
		end := start + 1
		for end < len(target) && target[end] == target[start] {
			end++
		}
		run := end - start
		upper := isUpper(target[start])
		before := start > 0 && isUpper(target[start-1])
		after := end < len(target) && isUpper(target[end])
		if upper && run >= 2 && !before && !after {
			return true
		}
		start = end
	}
	return false
}

func isUpper(letter byte) bool {
	return letter >= 'A' && letter <= 'Z'
}
