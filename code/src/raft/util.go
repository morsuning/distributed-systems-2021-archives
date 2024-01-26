package raft

import "log"

// Debug Debugging
const Debug = false

func DebugPrintf(format string, a ...interface{}) {
	if Debug {
		log.Printf(format, a...)
	}
}
