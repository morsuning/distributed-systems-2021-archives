package main

import "time"
import "math/rand"

// 统计选票 candidate，给所有 follow 发送投票请求
// 未保护
func main() {
	rand.New(rand.NewSource(time.Now().UnixNano()))

	count := 0
	finished := 0

	for i := 0; i < 10; i++ {
		go func() {
			vote := requestVote1()
			if vote {
				count++
			}
			finished++
		}()
	}

	for count < 5 && finished != 10 {
		// wait
	}
	if count >= 5 {
		println("received 5+ votes!")
	} else {
		println("lost")
	}
}

func requestVote1() bool {
	time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
	return rand.Int()%2 == 0
}
