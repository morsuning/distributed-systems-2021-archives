package raft

import "log"

// Debug 控制是否打印调试信息
const Debug = false

// DebugPrintf 如果 Debug 为 true，则打印格式化的调试信息。
func DebugPrintf(format string, a ...interface{}) {
	if Debug {
		log.Printf(format, a...)
	}
}
