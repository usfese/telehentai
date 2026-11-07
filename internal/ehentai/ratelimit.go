package ehentai

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// Pacer 把并发请求串行化成"最小间隔 + 随机抖动"的节奏，避免触发 E-Hentai 风控。
// Wait 在锁内预占下一个时间槽，因此多个 goroutine 也严格按序放行。
type Pacer struct {
	mu       sync.Mutex
	next     time.Time
	interval time.Duration
	jitter   time.Duration
}

// NewPacer 创建节拍器；interval 为最小间隔，jitter 为每次追加的随机上限。
func NewPacer(interval, jitter time.Duration) *Pacer {
	return &Pacer{interval: interval, jitter: jitter}
}

// Wait 阻塞到本请求被允许发出的时刻；ctx 取消时立即返回错误。
func (p *Pacer) Wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	base := p.next
	if base.Before(now) {
		base = now
	}
	delay := base.Sub(now)
	step := p.interval
	if p.jitter > 0 {
		step += time.Duration(rand.Int64N(int64(p.jitter)))
	}
	p.next = base.Add(step)
	p.mu.Unlock()

	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	return sleepCtx(ctx, delay)
}

// sleepCtx 是可被取消的睡眠。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
