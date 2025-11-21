package raft

// 支持 Raft 和 kvraft 保存持久化 Raft 状态（日志等）和 k/v 服务器快照。
//
// 我们将使用原始的 persister.go 来测试你的代码以进行评分。
// 因此，虽然你可以修改此代码以帮助调试，但在提交之前请使用原始代码进行测试。

import "sync"

// Persister 用于保存 Raft 状态和快照的持久化器。
type Persister struct {
	mu        sync.Mutex
	raftState []byte // Raft 的持久化状态（任期、投票、日志等）
	snapshot  []byte // 服务的快照数据
}

// MakePersister 创建一个新的 Persister 实例。
func MakePersister() *Persister {
	return &Persister{}
}

// clone 复制字节切片。
func clone(orig []byte) []byte {
	x := make([]byte, len(orig))
	copy(x, orig)
	return x
}

// Copy 创建 Persister 的副本。
func (ps *Persister) Copy() *Persister {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	np := MakePersister()
	np.raftState = ps.raftState
	np.snapshot = ps.snapshot
	return np
}

// SaveRaftState 仅保存 Raft 状态。
func (ps *Persister) SaveRaftState(state []byte) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.raftState = clone(state)
}

// ReadRaftState 读取 Raft 状态。
func (ps *Persister) ReadRaftState() []byte {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return clone(ps.raftState)
}

// RaftStateSize 返回 Raft 状态的大小。
func (ps *Persister) RaftStateSize() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return len(ps.raftState)
}

// SaveStateAndSnapshot 将 Raft 状态和 K/V 快照作为一个原子操作保存，
// 以帮助避免它们不同步。
func (ps *Persister) SaveStateAndSnapshot(state []byte, snapshot []byte) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.raftState = clone(state)
	ps.snapshot = clone(snapshot)
}

// ReadSnapshot 读取快照。
func (ps *Persister) ReadSnapshot() []byte {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return clone(ps.snapshot)
}

// SnapshotSize 返回快照的大小。
func (ps *Persister) SnapshotSize() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return len(ps.snapshot)
}
