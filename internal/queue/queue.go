package queue

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrQueueFull = errors.New("queue full")

type Job func(context.Context) error

type Config struct {
	MaxQueue   int
	NumWorker  int
}

type Queue struct {
	cfg    Config
	ch     chan Job
	wg     sync.WaitGroup
	stopCh chan struct{}
	once   sync.Once
}

func New(cfg Config) *Queue {
	return &Queue{
		cfg:    cfg,
		ch:     make(chan Job, cfg.MaxQueue),
		stopCh: make(chan struct{}),
	}
}

func (queue *Queue) Start() {
	for i := 0; i < queue.cfg.NumWorker; i++ {
		queue.wg.Add(1)
		go queue.worker()
	}
}

func (queue *Queue) Stop() {
	queue.once.Do(func() {
		close(queue.stopCh)
	})
	queue.wg.Wait()
}

func (queue *Queue) Enqueue(job Job) error {
	select {
	case queue.ch <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

func (queue *Queue) Depth() int {
	return len(queue.ch)
}

func (queue *Queue) worker() {
	defer queue.wg.Done()

	for {
		select {
		case <-queue.stopCh:
			return
		case job := <-queue.ch:
			// each job gets bounded time to avoid hanging workers forever
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = job(ctx)
			cancel()
		}
	}
}