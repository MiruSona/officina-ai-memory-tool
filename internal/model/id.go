package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"time"
)

// DayLayout 은 YYYY-MM-DD 하루를 적는 단 하나의 꼴이다. 모든 명령이 이 상수로
// 날짜를 찍어야 두 자리가 서로 어긋나지 않는다.
const DayLayout = "2006-01-02"

// idSequence 는 한 프로세스 안에서 만든 두 id 를 가른다. 윈도 시계는 같은
// 나노초를 두 번 줄 수 있고, 그러면 본문·시각·PID 가 다 똑같아진다.
var idSequence atomic.Uint64

// NewID 는 YYYYMMDD-<여덟자리 hex> 를 만든다. 해시 재료가 본문·시각·PID·세는수라
// 한 프로세스 안에서도 두 프로세스 사이에서도 id 가 안 부딪힌다.
func NewID(body string, at time.Time) string {
	seed := fmt.Sprintf("%s\x00%d\x00%d\x00%d", body, at.UnixNano(), os.Getpid(), idSequence.Add(1))
	sum := sha256.Sum256([]byte(seed))
	return at.Format("20060102") + "-" + hex.EncodeToString(sum[:4])
}

// QueueID 는 큐 파일 하나에서 승격된 기억의 id 다. 큐 파일 이름이 이미 유일하므로
// (나노초·PID·순번) 같은 큐 항목은 늘 같은 id 가 된다 — 두 번 승격해도 같은 자리에
// 덮어쓸 뿐 두 벌이 안 생긴다.
func QueueID(queueName, body, date string) string {
	sum := sha256.Sum256([]byte(queueName + "\x00" + body))
	return strings.ReplaceAll(date, "-", "") + "-" + hex.EncodeToString(sum[:4])
}

// IsID 는 글이 제대로 된 id 인지다. 꼴의 주인은 validate.go 의 idPattern 하나뿐이라
// 그 밖의 값으로는 경로를 만들 수 없다.
func IsID(id string) bool {
	return idPattern.MatchString(id)
}

// StorePath 는 저장소 안 기억의 자리다 : store/YYYY/MM/<id>.md.
// 슬래시로 적어야 OS 가 달라도 견줄 수 있다. 뿌리와 잇는 것은 부르는 쪽 몫이다.
func StorePath(id string) string {
	if !IsID(id) {
		return ""
	}
	return path.Join("store", id[:4], id[4:6], id+".md")
}
