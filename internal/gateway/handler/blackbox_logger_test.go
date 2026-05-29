package handler

import (
	"fmt"
	"sync"
	"testing"
)

func TestBlackboxRingBuffer_OrderAndCapacity(t *testing.T) {
	rb := NewBlackboxRingBuffer()

	// 1. 写入小于容量的数据
	rb.Write("log 1")
	rb.Write("log 2")
	logs := rb.Dump()
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	if logs[0] != "log 1" || logs[1] != "log 2" {
		t.Errorf("incorrect order: %v", logs)
	}

	// 2. 写入超过容量（20条）的数据，写入共 25 条
	for i := 3; i <= 25; i++ {
		rb.Write(fmt.Sprintf("log %d", i))
	}

	logs = rb.Dump()
	if len(logs) != 20 {
		t.Fatalf("expected exactly 20 logs, got %d", len(logs))
	}

	// 最老的一条应该是 log 6 (因为 1-25 写入，容量 20，最后留下的应该是 6-25)
	if logs[0] != "log 6" {
		t.Errorf("expected oldest log to be 'log 6', got '%s'", logs[0])
	}
	if logs[19] != "log 25" {
		t.Errorf("expected newest log to be 'log 25', got '%s'", logs[19])
	}
}

func TestBlackboxRingBuffer_ConcurrencyRace(t *testing.T) {
	rb := NewBlackboxRingBuffer()
	var wg sync.WaitGroup

	// 并发写协程
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				rb.Write(fmt.Sprintf("worker %d - log %d", workerID, j))
			}
		}(i)
	}

	// 并发读协程
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = rb.Dump()
			}
		}()
	}

	wg.Wait()
	logs := rb.Dump()
	if len(logs) != 20 {
		t.Errorf("expected full buffer size of 20, got %d", len(logs))
	}
}
