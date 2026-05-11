package agent

import (
	"testing"
	"time"
)

func TestBudget_NotExceeded(t *testing.T) {
	b := &Budget{MaxTurns: 10, MaxTokens: 1000, MaxWall: time.Hour}
	b.Start()
	b.RecordTurn()
	b.RecordTokens(Usage{InputTokens: 100, OutputTokens: 50})

	reason, code := b.Exceeded()
	if reason != "" || code != 0 {
		t.Errorf("expected within budget, got reason=%q code=%d", reason, code)
	}
}

func TestBudget_TurnCap(t *testing.T) {
	b := &Budget{MaxTurns: 2}
	b.Start()
	b.RecordTurn()
	b.RecordTurn()

	reason, code := b.Exceeded()
	if reason == "" {
		t.Error("expected turn cap exceeded")
	}
	if code != ExitIterationCap {
		t.Errorf("expected ExitIterationCap (%d), got %d", ExitIterationCap, code)
	}
}

func TestBudget_TokenBudget(t *testing.T) {
	b := &Budget{MaxTokens: 100}
	b.Start()
	b.RecordTokens(Usage{InputTokens: 60, OutputTokens: 50})

	reason, code := b.Exceeded()
	if reason == "" {
		t.Error("expected token budget exceeded")
	}
	if code != ExitTokenBudget {
		t.Errorf("expected ExitTokenBudget (%d), got %d", ExitTokenBudget, code)
	}
}

func TestBudget_WallClock(t *testing.T) {
	b := &Budget{MaxWall: time.Millisecond}
	b.Start()
	time.Sleep(5 * time.Millisecond)

	reason, code := b.Exceeded()
	if reason == "" {
		t.Error("expected wall-clock exceeded")
	}
	if code != ExitWallClock {
		t.Errorf("expected ExitWallClock (%d), got %d", ExitWallClock, code)
	}
}

func TestBudget_Counters(t *testing.T) {
	b := &Budget{}
	b.Start()
	b.RecordTurn()
	b.RecordTurn()
	b.RecordTokens(Usage{InputTokens: 10, OutputTokens: 5, CacheTokens: 2})
	b.RecordTokens(Usage{InputTokens: 20, OutputTokens: 3})

	if b.Turns() != 2 {
		t.Errorf("expected 2 turns, got %d", b.Turns())
	}
	if b.Tokens() != 38 { // 10+5+20+3 (cache not counted)
		t.Errorf("expected 38 tokens, got %d", b.Tokens())
	}
}

func TestBudget_ZeroLimitsUnlimited(t *testing.T) {
	b := &Budget{}
	b.Start()
	for i := 0; i < 1000; i++ {
		b.RecordTurn()
	}
	b.RecordTokens(Usage{InputTokens: 1e9, OutputTokens: 1e9})
	reason, code := b.Exceeded()
	if reason != "" || code != 0 {
		t.Errorf("zero limits should be unlimited, got reason=%q code=%d", reason, code)
	}
}
