package raft

import "log"

// Debug Debugging
const Debug = false

func DebugPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}
