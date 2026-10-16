package service

import (
	"context"
	"os"
	"sync"
)

func watchResizeEvents(ctx context.Context, events <-chan os.Signal, size func() (uint32, uint32, error), session resizableExecSession) func() {
	done := make(chan struct{})
	finished := make(chan struct{})
	var stopOnce sync.Once
	go func() {
		defer close(finished)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-events:
				cols, rows, err := size()
				if err == nil {
					_ = session.Resize(cols, rows)
				}
			}
		}
	}()
	return func() {
		stopOnce.Do(func() {
			close(done)
			<-finished
		})
	}
}
