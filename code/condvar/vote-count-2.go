package main

import "sync"
import "time"
import "math/rand"

// 修复 vote-count-1 中存在的问题
func main() {
	rand.New(rand.NewSource(time.Now().UnixNano()))

	count := 0
	finished := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		go func() {
			vote := requestVote2()
			mu.Lock()
			defer mu.Unlock()
			if vote {
				count++
			}
			finished++
		}()
	}

	for {
		mu.Lock()

		if count >= 5 || finished == 10 {
			break
		}
		mu.Unlock()
	}
	if count >= 5 {
		println("received 5+ votes!")
	} else {
		println("lost")
	}
	mu.Unlock()
}

func requestVote2() bool {
	time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
	return rand.Int()%2 == 0
}
