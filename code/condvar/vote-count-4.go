package main

import "sync"
import "time"
import "math/rand"

// 使用 Condition，等待共享数据中某个属性或变量变成 true，类似 Java ReentrantLock 绑定 Condition
func main() {
	rand.New(rand.NewSource(time.Now().UnixNano()))

	count := 0
	finished := 0
	var mu sync.Mutex
	cond := sync.NewCond(&mu)

	for i := 0; i < 10; i++ {
		go func() {
			vote := requestVote4()
			mu.Lock()
			defer mu.Unlock()
			if vote {
				count++
			}
			finished++
			// 持有锁时，修改数据时调用，唤醒正在等待的线程
			cond.Broadcast()
		}()
	}

	mu.Lock()
	for count < 5 && finished != 10 {
		// 等待，条件不满足则加入等待线程
		// 被唤醒时，先拿锁，再检查条件，cond.Wait() 也需要在拿到锁的时候执行，会先释放锁，再加入等待队列
		// 下次执行时，会回到 30 行？？？直到离开循环，离开循环时意味着已经不满足 31 行的条件
		cond.Wait()
	}
	if count >= 5 {
		println("received 5+ votes!")
	} else {
		println("lost")
	}
	mu.Unlock()
}

func requestVote4() bool {
	time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
	return rand.Int()%2 == 0
}
