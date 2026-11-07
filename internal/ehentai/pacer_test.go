package ehentai

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPacerWaitsBetweenCalls(t *testing.T) {
	p := NewPacer(20*time.Millisecond, 0)
	ctx := context.Background()

	start := time.Now()
	for range 3 {
		if err := p.Wait(ctx); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	}
	// 3 次调用之间应有 2 个间隔。
	if elapsed := time.Since(start); elapsed < 38*time.Millisecond {
		t.Fatalf("节流未生效：3 次 Wait 仅耗时 %s", elapsed)
	}
}

func TestPacerRespectsContext(t *testing.T) {
	p := NewPacer(200*time.Millisecond, 0)
	if err := p.Wait(context.Background()); err != nil {
		t.Fatalf("首次 Wait: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := p.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("期望 context.Canceled，得到 %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("取消后应立即返回，实际等待 %s", elapsed)
	}
}
