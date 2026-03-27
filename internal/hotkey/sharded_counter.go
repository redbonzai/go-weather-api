package hotkey

import (
	"math/rand"
	"sync"
)

type ShardedCounter struct {
	mu     sync.Mutex
	shards int
	counts map[string]int64 // key = base#shard
}

func New(shards int) *ShardedCounter {
	return &ShardedCounter{
		shards: shards,
		counts: make(map[string]int64),
	}
}

func (counter *ShardedCounter) Inc(base string) {
	shardIndex := rand.Intn(counter.shards)
	key := base + "#" + itoa(shardIndex)

	counter.mu.Lock()
	counter.counts[key]++
	counter.mu.Unlock()
}

func (counter *ShardedCounter) Get(base string) int64 {
	var sum int64
	counter.mu.Lock()
	defer counter.mu.Unlock()
	for shardNum := 0; shardNum < counter.shards; shardNum++ {
		sum += counter.counts[base+"#"+itoa(shardNum)]
	}
	return sum
}

func itoa(value int) string {
	// small helper; in real code use strconv.Itoa
	if value == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for value > 0 {
		digit := value % 10
		buf = append([]byte{byte('0' + digit)}, buf...)
		value /= 10
	}
	return string(buf)
}
