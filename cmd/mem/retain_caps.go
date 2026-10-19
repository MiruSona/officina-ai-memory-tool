package main

import (
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/retain"
)

// retainCaps 는 R6(세션 · 하루 상한)을 미리 센다. 넘을 후보는 자식에 안 보낸다 (헛 호출 줄이기).
type retainCaps struct {
	session, day       int
	perSession, perDay int
}

func loadRetainCaps(repository *config.Repository, sessionID string) retainCaps {
	state := retain.LoadState(repository.Dir)
	caps := retainCaps{day: state.Days[time.Now().Format(model.DayLayout)],
		perSession: repository.Config.Retain.PerSession, perDay: repository.Config.Retain.PerDay}
	if session := state.Sessions[sessionID]; session != nil {
		caps.session = session.AutoAdds
	}
	return caps
}

func (c *retainCaps) full() bool {
	return (c.perSession > 0 && c.session >= c.perSession) || (c.perDay > 0 && c.day >= c.perDay)
}

func (c *retainCaps) count() {
	c.session++
	c.day++
}
