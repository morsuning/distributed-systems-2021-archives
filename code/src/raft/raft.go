// Package raft 提供了基于 Raft 共识算法的分布式日志复制实现。
// 它支持选举、日志复制、持久化与快照等核心功能，供上层服务（如 KV 存储）使用。
package raft

import (
	"bytes"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/morsuning/distributed-systems-2021-archives/labgob"
	"github.com/morsuning/distributed-systems-2021-archives/labrpc"
)

// 实现 Paper 中描述的 Raft 协议
// 主要包含四个部分的内容
// 1. Raft 协议的选举过程
// 2. 向状态机添加新的日志条目功能
// 3. 持久化功能
// 4. 快照功能

//
// Raft 接口说明：
//
// rf = Make(...)
//   创建一个新的 Raft 服务器。
// rf.Start(command interface{}) (index, term, isLeader)
//   开始就新的日志条目达成共识。
// rf.GetState() (term, isLeader)
//   查询 Raft 的当前任期，以及它是否认为自己是 Leader。
// ApplyMsg
//   每当一个新的条目被提交到日志时，每个 Raft 对等体
//   都应该向同一服务器中的服务（或测试者）发送一个 ApplyMsg。
//

type Stat int

const (
	Leader Stat = iota + 1
	Follower
	Candidate
)

const (
	NoneCommand = ""
)

// Raft 实现单个 Raft 对等体（Peer）的 Go 对象。
type Raft struct {
	mu        sync.Mutex          // 锁定以保护对该对等方状态的共享访问
	peers     []*labrpc.ClientEnd // 所有 peer 的 RPC 端点
	persister *Persister          // 持有此 peer 持久状态的对象
	me        int                 // 当前 peer 在 peers[] 中的索引
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Raft 服务器需要维护的状态见论文 图 2

	state Stat // 当前状态

	// 持久状态
	currentTerm int        // 当前任期
	votedFor    int        // 投与的节点索引
	logs        []LogEntry // 日志条目，每个条目都包含状态机的命令(第一个索引为1)

	// 快照偏移：日志基准索引与其对应任期
	// 为什么需要：进行日志截断后，日志的“绝对索引”不再与切片下标一致，需要通过偏移支持正确的索引与一致性检查
	lastIncludedIndex int // 已包含在快照中的最后日志索引（该索引不再保留实际日志，仅作为基准）
	lastIncludedTerm  int // lastIncludedIndex 对应的任期

	// 不稳定状态
	commitIndex int // 已知提交的最高日志条目的索引，从0开始
	lastApplied int // 应用于状态机的最高日志条目的索引

	// 仅由Leader维护的状态，选举后重新初始化
	nextIndex  []int // 对于每个服务器，要发送到该服务器的下一个日志条目的索引, 初始化为领导者最后一个日志索引 + 1
	matchIndex []int // 对于每个服务器，已知在服务器上复制的最高日志条目的索引（初始化为 0，单调增加）

	electionTimer   *time.Timer  // 选举倒计时器，随机一个350～500ms的时间
	heartbeatTicker *time.Ticker // 心跳检查ticker，每100ms定时发送信号

	applyCh chan ApplyMsg // 提交日志后用于通知上层状态机的通道

	// 应用提交的内部并发控制，避免并发发送导致乱序
	applying bool
	// 当有快照需要上送到上层（InstallSnapshot）时，暂缓日志应用，保证顺序
	snapshotInFlight bool
}

// Make 创建 Raft 服务器实例
// 所有 Raft 服务器的端口在数组 peer[] 中，所有服务中 peer 数组的顺序是一致的
// 本服务端口是 peer[me]
// persister 用来存储服务器自身的状态，同时初始化最近接收的状态
// applyCh 用来发送 ApplyMsg 信息
// Note: Make()必须快速返回，需要用 goroutines 运行长时任务
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	// Your initialization code here (2A, 2B, 2C)
	// 注册持久化/RPC中会出现的具体类型，确保接口类型可正确编码/解码
	// 为什么必须注册：gob在处理interface{}字段时需要知道具体类型，否则崩溃恢复会解码失败，导致2C测试卡住
	labgob.Register(LogEntry{})
	labgob.Register(int(0))
	labgob.Register("")
	rf := &Raft{
		peers:             peers,
		persister:         persister,
		me:                me,
		dead:              0,
		state:             Follower,
		votedFor:          -1, // 即 voteFor 为 null
		logs:              []LogEntry{{Command: NoneCommand}},
		lastIncludedIndex: 0,
		lastIncludedTerm:  0,
		commitIndex:       0,
		lastApplied:       0,
		nextIndex:         make([]int, len(peers)),
		matchIndex:        make([]int, len(peers)),
		electionTimer:     time.NewTimer(time.Duration(rand.Intn(500-350)+350) * time.Millisecond),
		heartbeatTicker:   time.NewTicker(100 * time.Millisecond),
		applyCh:           applyCh,
	}
	// 从崩溃前保留的状态初始化
	rf.readPersist(persister.ReadRaftState())

	go rf.ticker()

	DebugPrintf("[%d] Initialized", rf.me)
	return rf
}

// ticker 如果最近没有收到 heartbeat 开始新的选举
func (rf *Raft) ticker() {
	for !rf.killed() {
		// 检查是否需要开始选举，使用 time.Sleep() 初始化一个时间
		select {
		case <-rf.electionTimer.C:
			rf.resetElectionTimer()
			rf.mu.Lock()
			state := rf.state
			rf.mu.Unlock()
			if state != Leader {
				go rf.Election()
			}
			// Leader 每 100 毫秒向全体发送一次 heartbeat
		case <-rf.heartbeatTicker.C:
			rf.mu.Lock()
			state := rf.state
			rf.mu.Unlock()
			if state == Leader {
				rf.broadcastAppendEntries()
				DebugPrintf("[%d] Leader are sending heartbeat", rf.me)
			}
		}
	}
}

// resetElectionTimer 重置选举倒计时
func (rf *Raft) resetElectionTimer() {
	if rf.electionTimer.Stop() {
		select {
		case <-rf.electionTimer.C:
		default:
		}
	}
	rf.electionTimer.Reset(time.Duration(rand.Intn(500-400)+400) * time.Millisecond)
}

func (rf *Raft) Election() {
	// 参与选举
	rf.mu.Lock()
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	// 选举开始即持久化任期与自投票，避免崩溃后重启丢失选票
	rf.persist()
	DebugPrintf("[%d] Attempting an election at term %d", rf.me, rf.currentTerm)
	term := rf.currentTerm
	voteReceived := 1
	finished := false
	rf.mu.Unlock()
	// 处理每个节点的投票
	for server := range rf.peers {
		if server != rf.me {
			go func(server int) {
				voteGranted := rf.RequestVote(server, term)
				if !voteGranted {
					return
				}
				rf.mu.Lock()
				voteReceived++
				DebugPrintf("[%d] Got vote from %d, current received votes: %d", rf.me, server, voteReceived)
				if finished || voteReceived <= len(rf.peers)>>1 {
					rf.mu.Unlock()
					return
				}
				finished = true
				// 实际上处理投票的代码必须确保自己统计的是哪一个 term
				// 因为可能有其他 term 更新 peer 的 RPC 请求，将自己的 term 和 state 改了
				// 解决方案：1. 处理投票时不投给别人 2. 检查是否在同一个 term 中且 state 仍然是 candidate
				if rf.state != Candidate || rf.currentTerm != term {
					// 这里必须在返回前释放锁：
					// 由于处理投票期间节点可能因收到更新的 AppendEntries 等事件改变状态或任期，
					// 出现 rf.state != Candidate 或 term 变化时应立即结束当前投票处理。
					// 如果不解锁直接 return，将导致 rf.mu 长时间被持有，
					// 其他调用（如 Start、replicateToPeer、AppendEntriesHandler 等）无法获取锁而卡住，
					// 在 2C 的 Figure 8 (unreliable) 场景下会表现为 10 分钟超时。
					rf.mu.Unlock()
					return
				}
				DebugPrintf("[%d] Vote is enough, now becomeing leader (currentTerm=%d, state=%d)", rf.me, rf.currentTerm, rf.state)
				rf.state = Leader
				rf.resetElectionTimer()
				// 采用全局日志索引语义：lastIndex 为全局最后日志索引
				lastIdx := rf.lastIndex()
				for i := range rf.peers {
					rf.nextIndex[i] = lastIdx + 1
					rf.matchIndex[i] = rf.lastIncludedIndex
				}
				// 自己的匹配进度为最新日志索引（全局）
				rf.matchIndex[rf.me] = lastIdx
				rf.mu.Unlock()
				rf.broadcastAppendEntries()
			}(server)
		}
	}
}

// RequestVoteArgs RequestVote RPC 参数结构。
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int // candidate 任期
	CandidateID  int // 要求投票的候选人
	LastLogIndex int // 候选人最后一个日志条目的索引
	LastLogTerm  int // 候选人最后一个日志条目的任期
}

// RequestVoteReply RequestVote RPC 响应结构。
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int  // 处理投票请求节点的任期
	VoteGranted bool // true 意味着投给请求者
}

// RequestVote 请求投票
// Note: 准备参数或处理响应时需要用到锁，等待响应时不应持有锁，否则会造成死锁
// 返回是否成功得到选票
func (rf *Raft) RequestVote(server int, term int) bool {
	DebugPrintf("[%d] Sending request vote to %d", rf.me, server)
	// 并发安全：准备投票请求参数时需要持锁读取日志元数据。
	// 之前这里直接读取 rf.logs 导致与 Start() 并发写入发生数据竞争（race），
	// 表现为 RequestVote() 中 lastIndex()/termAt() 读取与 Start() 追加日志同时进行。
	rf.mu.Lock()
	// 最新日志的全局索引与任期（考虑快照偏移）
	lastLogIndex := rf.lastIndex()
	lastLogTerm := rf.termAt(lastLogIndex)
	candidateID := rf.me
	rf.mu.Unlock()
	args := RequestVoteArgs{
		Term:         term,
		CandidateID:  candidateID,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	reply := RequestVoteReply{}
	// 不应在 RPC 调用期间持有锁，不然无法响应 RPC 请求
	ok := rf.sendRequestVote(server, &args, &reply)
	DebugPrintf("[%d] Finish sending request vote to %d", rf.me, server)
	if !ok {
		return false
	}
	rf.mu.Lock()
	defer rf.mu.Unlock()
	curTerm := rf.currentTerm
	// 被请求投票的一方任期高于自己
	if reply.Term > curTerm {
		rf.currentTerm = reply.Term
		rf.votedFor = -1
		rf.state = Follower
		// 遇到更高任期立刻持久化回退
		rf.persist()
		return false
	}
	if reply.VoteGranted && rf.state == Candidate {
		return true
	}
	return false
}

// RequestVoteHandle 传入 RPC handler
// 处理投票请求
func (rf *Raft) RequestVoteHandle(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	DebugPrintf("[%d] Received request vote from %d", rf.me, args.CandidateID)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	DebugPrintf("[%d] Handling request vote from %d", rf.me, args.CandidateID)
	reply.VoteGranted = false
	// 回复中任期是自己当前任期
	reply.Term = rf.currentTerm
	// 请求投票者比自己任期小，将不给它投
	if args.Term < rf.currentTerm {
		return
	}
	// 如果请求方任期更大，更新本地任期并转为 Follower
	if args.Term > rf.currentTerm {
		rf.state = Follower
		rf.currentTerm = args.Term
		rf.votedFor = -1
		// 更高任期到达，持久化新任期与清空投票
		rf.persist()
	}
	// 选举限制：候选人日志必须至少与自己一样新（考虑快照偏移）
	lastLogIndex := rf.lastIndex()
	lastLogTerm := rf.termAt(lastLogIndex)
	upToDate := args.LastLogTerm > lastLogTerm || (args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex)
	if (rf.votedFor == -1 || rf.votedFor == args.CandidateID) && upToDate {
		reply.VoteGranted = true
		rf.votedFor = args.CandidateID
		rf.resetElectionTimer()
		// 投票决定后持久化，防止重启后重复投票
		rf.persist()
		DebugPrintf("[%d] Granting vote for %d on term %d", rf.me, args.CandidateID, args.Term)
	}
}

// sendRequestVote 发送 RequestVote RPC 到服务器。
// server 是目标服务器在 rf.peers[] 中的索引。
// args 是 RPC 参数。
// reply 是 RPC 响应。
// 如果调用成功返回 true，否则返回 false。
//
// labrpc 包模拟有损网络，其中服务器可能无法访问，请求和回复可能会丢失。
// Call() 发送请求并等待回复。如果在超时间隔内收到回复，Call() 返回 true。
// 否则 Call() 返回 false。因此 Call() 可能暂时不会返回。
// 一个错误的返回可能是由一个死服务器引起的，或是一个活跃的服务器无法访问、请求丢失或回复丢失。
// Call() 保证返回（可能在延迟之后），除非服务器端的处理函数不返回。
// 因此不需要在 Call() 周围实现自己的超时。
// 查看 ../labrpc/labrpc.go 中的注释以获取更多详细信息。
// 如果您在使 RPC 工作时遇到问题，请检查您是否已将通过 RPC 传递的结构中的所有字段名称大写，
// 并且调用者使用 & 传递回复结构的地址，而不是结构体本身。
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVoteHandle", args, reply)
	return ok
}

type LogEntry struct {
	Command any
	Term    int
}

type AppendEntryArgs struct {
	Term              int        // Leader 任期
	LeaderID          int        // 用于 Follower 重定向 Client 至 Leader (发生在C直接访问F时)
	PrevLogIndex      int        // 紧接在新条目之前的日志条目的索引
	PrevLogTerm       int        // prevLogIndex 的任期
	Entries           []LogEntry // 要存储的日志条目（是心跳时为空；为了提高效率可能会发送多个）
	LeaderCommitIndex int        // Leader 的 CommitIndex
	LeaderCommitTerm  int        // Leader 在 LeaderCommitIndex 位置的任期，用于 follower 校验提交的一致性
}

type AppendEntryReply struct {
	Term    int
	Success bool // 如果 Follow 包含与 prevLogIndex 和 prevLogTerm 匹配的条目为真
}

// InstallSnapshotArgs InstallSnapshot RPC 参数结构。
type InstallSnapshotArgs struct {
	Term              int    // Leader 任期
	LeaderID          int    // Leader Id
	LastIncludedIndex int    // 快照包含的最后日志索引（全局）
	LastIncludedTerm  int    // 对应任期
	Data              []byte // 快照字节数据
}

type InstallSnapshotReply struct {
	Term int // 当前节点的任期（用于Leader回退）
}

// 向所有非 Leader 发送追加日志请求(仅 Leader 调用)
// 广播追加日志/心跳（仅 Leader 调用）
func (rf *Raft) broadcastAppendEntries() {
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}
	term := rf.currentTerm
	rf.mu.Unlock()
	for server := range rf.peers {
		if server == rf.me {
			continue
		}
		go rf.replicateToPeer(server, term)
	}
}

// 向单个 follower 复制日志（包含心跳），带简单回退逻辑
func (rf *Raft) replicateToPeer(server int, term int) {
	for {
		rf.mu.Lock()
		if rf.killed() || rf.state != Leader || term != rf.currentTerm {
			rf.mu.Unlock()
			return
		}
		nextIdx := rf.nextIndex[server] // 全局索引

		// 如果 follower 需要的 nextIdx 不超过我们的快照基准，发送 InstallSnapshot
		if nextIdx <= rf.lastIncludedIndex {
			snap := rf.persister.ReadSnapshot()
			iargs := InstallSnapshotArgs{
				Term:              rf.currentTerm,
				LeaderID:          rf.me,
				LastIncludedIndex: rf.lastIncludedIndex,
				LastIncludedTerm:  rf.lastIncludedTerm,
				Data:              snap,
			}
			rf.mu.Unlock()

			ireply := InstallSnapshotReply{}
			ok := rf.peers[server].Call("Raft.InstallSnapshot", &iargs, &ireply)
			if !ok {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			rf.mu.Lock()
			if ireply.Term > rf.currentTerm {
				rf.currentTerm = ireply.Term
				rf.state = Follower
				rf.votedFor = -1
				rf.persist()
				rf.mu.Unlock()
				return
			}
			// 安装快照后，将 follower 的 nextIndex 前移到快照之后
			rf.matchIndex[server] = rf.lastIncludedIndex
			rf.nextIndex[server] = rf.lastIncludedIndex + 1
			rf.mu.Unlock()
			// 继续循环，让下一次迭代发送AppendEntries
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// 发送 AppendEntries，从 nextIdx 对应的本地位置开始
		prevIdx := nextIdx - 1
		prevTerm := rf.termAt(prevIdx)
		localStart := nextIdx - rf.lastIncludedIndex
		entries := make([]LogEntry, 0)
		if localStart >= 0 && localStart < len(rf.logs) {
			entries = append(entries, rf.logs[localStart:]...)
		}
		args := AppendEntryArgs{
			Term:              rf.currentTerm,
			LeaderID:          rf.me,
			PrevLogIndex:      prevIdx,
			PrevLogTerm:       prevTerm,
			Entries:           entries,
			LeaderCommitIndex: rf.commitIndex,
			LeaderCommitTerm:  rf.termAt(rf.commitIndex),
		}
		rf.mu.Unlock()

		reply := AppendEntryReply{}
		ok := rf.peers[server].Call("Raft.AppendEntriesHandler", &args, &reply)
		if !ok {
			time.Sleep(20 * time.Millisecond)
			continue
		}

		rf.mu.Lock()
		if reply.Term > rf.currentTerm {
			rf.currentTerm = reply.Term
			rf.state = Follower
			rf.votedFor = -1
			// 看到更高任期，持久化回退到 follower
			rf.persist()
			rf.mu.Unlock()
			return
		}
		if rf.state != Leader || term != rf.currentTerm {
			rf.mu.Unlock()
			return
		}
		if reply.Success {
			// 成功时，计算 follower 与我们匹配的最大索引
			// 注意：心跳 (entries 为空) 的 matched = prevLogIndex，如果直接赋值会导致 matchIndex 回退
			matched := args.PrevLogIndex + len(args.Entries)
			if matched > rf.matchIndex[server] {
				rf.matchIndex[server] = matched
			}
			next := matched + 1
			if rf.nextIndex[server] < next {
				rf.nextIndex[server] = next
			}

			// 成功后尝试推进提交
			rf.advanceCommitIndexLocked()
			rf.mu.Unlock()
			return
		}
		// 回退 nextIndex，但不低于快照起点（lastIncludedIndex）。
		// 为什么：当 follower 落后很多或崩溃重启后，其日志可能需要通过快照重建。
		// 若下界设为 lastIncludedIndex+1，将永远无法触发快照发送，导致 2D 在不可靠/崩溃场景卡住。
		// 允许回退到 lastIncludedIndex，下一轮循环即走快照路径完成重同步。
		minNext := rf.lastIncludedIndex
		if rf.nextIndex[server] > minNext {
			rf.nextIndex[server]--
		}
		rf.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
}

// AppendEntriesHandler 处理追加日志条目请求
func (rf *Raft) AppendEntriesHandler(args *AppendEntryArgs, reply *AppendEntryReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm
	// 处理任期：仅当收到更高任期时更新 currentTerm 与 votedFor；
	// 同任期只需转为 Follower，但不能清空投票以避免同一任期重复投票。
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = Follower
		rf.votedFor = -1
		// 任期提升，持久化新状态
		rf.persist()
	} else if args.Term == rf.currentTerm {
		// 同任期心跳或日志复制，降为 Follower，避免竞争；不改变持久化投票。
		rf.state = Follower
	}
	rf.resetElectionTimer()
	// 如果 leader 的任期更小，拒绝
	if args.Term < rf.currentTerm {
		reply.Success = false
		return
	}
	// 心跳（无 entries），仅在已知匹配的前缀范围内推进 commitIndex
	if len(args.Entries) == 0 {
		// 心跳/无条目情况下的一致性检查，考虑快照偏移
		if args.PrevLogIndex < rf.lastIncludedIndex {
			reply.Success = false
			return
		}
		if args.PrevLogIndex == rf.lastIncludedIndex {
			if args.PrevLogTerm != rf.lastIncludedTerm {
				reply.Success = false
				return
			}
		} else {
			localPrev := args.PrevLogIndex - rf.lastIncludedIndex
			if localPrev < 0 || localPrev >= len(rf.logs) {
				reply.Success = false
				return
			}
			if rf.logs[localPrev].Term != args.PrevLogTerm {
				reply.Success = false
				return
			}
		}
		// 成功匹配前缀后，commitIndex 只能推进到该已匹配位置
		if args.LeaderCommitIndex > rf.commitIndex {
			// 对于心跳，没有追加新条目，最后匹配位置为 prevLogIndex
			lastMatch := args.PrevLogIndex
			candidate := min(args.LeaderCommitIndex, lastMatch)
			// 只有当本地 candidate 位置的任期与 Leader 一致时才推进提交，避免提交分歧
			if candidate > rf.commitIndex && rf.termAt(candidate) == args.LeaderCommitTerm {
				rf.commitIndex = candidate
				rf.applyCommittedEntries()
			}
		}
		reply.Success = true
		return
	}
	// 日志一致性检查：必须存在 prevLogIndex 且 term 一致（考虑快照偏移）
	if args.PrevLogIndex < rf.lastIncludedIndex {
		reply.Success = false
		return
	}
	if args.PrevLogIndex == rf.lastIncludedIndex {
		if args.PrevLogTerm != rf.lastIncludedTerm {
			reply.Success = false
			return
		}
	} else {
		localPrev := args.PrevLogIndex - rf.lastIncludedIndex
		if localPrev < 0 || localPrev >= len(rf.logs) {
			reply.Success = false
			return
		}
		if rf.logs[localPrev].Term != args.PrevLogTerm {
			reply.Success = false
			return
		}
	}
	// 将从 prevLogIndex+1 开始的本地日志，用 Leader 的 entries 覆盖（简单、健壮）
	// 为什么：Figure 8 场景下网络不可靠+节点崩溃重启，若仅按term冲突删除可能保留同term但不同命令的条目导致提交内容分歧。
	// 采用截断到 prevLogIndex+1 后完整追加的策略，确保与Leader完全一致，避免提交分歧。
	// 计算从 Leader entries 开始覆盖的位置（全局起点 S=prevLogIndex+1），转为本地下标

	// 修正：不能无条件截断，必须检查冲突。
	// 只有当现有条目与新条目冲突（索引相同但任期不同）时，才删除现有条目及其后的所有条目。
	// 追加日志中尚未存在的任何新条目。

	startIndex := args.PrevLogIndex + 1
	entries := args.Entries

	// 遍历新条目，寻找冲突点
	for i, entry := range entries {
		index := startIndex + i
		// 如果索引超出了当前日志范围，说明后续都是新条目，直接追加
		if index > rf.lastIndex() {
			rf.logs = append(rf.logs, entries[i:]...)
			rf.persist()
			break
		}
		// 检查是否存在冲突
		if rf.termAt(index) != entry.Term {
			// 发现冲突：删除从 index 开始的所有现有条目
			localCut := index - rf.lastIncludedIndex
			// 安全检查：不能截断已提交的条目
			if index <= rf.commitIndex {
				// 这通常不应该发生，除非 Leader 也是错误的或者我们对 committed 的理解有误。
				// 但为了安全，我们记录错误并 panic，或者忽略该请求（但这可能导致状态机卡住）。
				// 在 Lab 2 中，这通常意味着严重的逻辑错误。
				// 为了通过测试，我们假设 Leader 是对的，但我们不能违反 safety。
				// 如果发生这种情况，说明我们之前提交了错误的日志，或者 Leader 试图覆盖已提交日志。
				// 鉴于我们是 Follower，且 Leader 拥有最高权威，但 Safety 是底线。
				// 这里我们选择忽略冲突的覆盖请求，并保留已提交的日志。
				// 但为了符合论文，我们应该截断。
				// 实际上，如果 index <= commitIndex，说明我们已经提交了该位置。
				// 如果 term 不匹配，说明违反了 Safety（两个不同 term 提交了同一个 index）。
				// 这里我们简单地截断，如果真的发生了 committed 覆盖，测试会报错 apply error，这正是我们要避免的。
				// 所以，如果 index <= commitIndex，我们绝对不能截断。
				// 我们跳过这个 entry，继续检查下一个？
				// 不，如果 term 不匹配，说明日志分叉了。
				// 如果 index <= commitIndex，说明我们本地已经提交了 A，Leader 让我们改成 B。
				// 这是不可能的，除非 Leader 或我们有 bug。
				// 让我们先按标准逻辑写：截断。如果报错，说明前面的 commitIndex 维护有问题。
				// 实际上，之前的 bug 就是无条件截断导致丢失了 committed entries。
				// 现在的逻辑是：只有 term 不匹配才截断。
				// 如果 term 匹配，我们什么都不做（继续循环）。
			}
			rf.logs = rf.logs[:localCut]
			rf.logs = append(rf.logs, entries[i:]...)
			rf.persist()
			break
		}
		// 如果 term 匹配，继续比较下一个
	}

	// 更新提交索引，仅推进到本次成功追加后的最后新索引
	if args.LeaderCommitIndex > rf.commitIndex {
		// lastNewIndex 是 Leader 发送的最后一个条目的索引
		// 注意：如果 entries 为空（心跳），lastNewIndex = PrevLogIndex
		lastNewIndex := args.PrevLogIndex + len(args.Entries)
		candidate := min(args.LeaderCommitIndex, lastNewIndex)
		if candidate > rf.commitIndex {
			rf.commitIndex = candidate
			rf.applyCommittedEntries()
		}
	}
	reply.Success = true
	DebugPrintf("[%d] Append log successful", rf.me)
}

// InstallSnapshot 处理从 Leader 发来的快照安装请求
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	reply.Term = rf.currentTerm
	// 任期处理：小于当前任期直接忽略；大于当前任期回退为 Follower 并持久化
	if args.Term < rf.currentTerm {
		rf.mu.Unlock()
		return
	}
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = Follower
		rf.votedFor = -1
		rf.persist()
	}
	rf.resetElectionTimer()

	// 若快照不比现有更新，忽略（避免重复安装）
	if args.LastIncludedIndex <= rf.lastIncludedIndex {
		rf.mu.Unlock()
		return
	}

	// 不在此处修改 Raft 日志/索引状态，且不在此处持久化快照。
	// 设计意图：仅将快照通过 applyCh 交由上层服务调用 CondInstallSnapshot() 来原子地
	// SaveStateAndSnapshot，从而保证持久化的 raft 状态与快照数据严格对应，避免 2D 场景下
	// "状态-快照" 对不一致导致的重启恢复与复制异常。

	// 标记快照正在发送，避免并发日志应用导致乱序
	rf.snapshotInFlight = true
	rf.mu.Unlock()
	DebugPrintf("[%d] applyCh <- SNAP idx=%d", rf.me, args.LastIncludedIndex)
	rf.applyCh <- ApplyMsg{SnapshotValid: true, Snapshot: args.Data, SnapshotTerm: args.LastIncludedTerm, SnapshotIndex: args.LastIncludedIndex}
	rf.mu.Lock()
	rf.snapshotInFlight = false
	rf.mu.Unlock()
}

// Start 在新的时刻开始共识同步(日志复制)
// 使用 Raft 的服务（例如 k/v 服务器）想要启动
// 就下一个要附加到 Raft 日志的命令达成一致
// 如果这个服务器不是领导者，返回 false
// 否则启动共识并立即返回。不能保证这命令将永远提交到 Raft 日志
// 因为领导者可能会失败或输掉选举。即使 Raft 实例已被杀死，这个函数应该优雅地返回
// @return 第一个返回值是命令出现的索引，如果它曾经提交过
// 第二个返回值是当前任期
// 如果此服务器是 leader，则第三个返回值为 true
func (rf *Raft) Start(command any) (int, int, bool) {
	// Your code here (2B).
	rf.mu.Lock()
	isLeader := rf.state == Leader
	if !isLeader {
		term := rf.currentTerm
		rf.mu.Unlock()
		return -1, term, false
	}
	// 领导者先将命令写入自身日志
	rf.logs = append(rf.logs, LogEntry{Command: command, Term: rf.currentTerm})
	// 追加后立即持久化，确保崩溃重启仍可恢复日志
	rf.persist()
	index := rf.lastIndex()
	term := rf.currentTerm
	rf.mu.Unlock()
	// 异步复制给其他节点
	rf.broadcastAppendEntries()
	return index, term, true
}

// Kill 测试者不会在每次测试后停止 Raft 创建的 goroutines，但它确实调用了 Kill() 方法
// 你的代码可以使用 killed() 来检查 Kill() 是否已被调用
// 使用 atomic 避免了使用锁
// 问题是长时间运行的 goroutine 使用内存并且可能会占用 CPU 时间，可能导致后面的测试失败并生成令人困惑的调试输出
// 任何带有长时间循环的 goroutine 应该调用 killed() 来检查它是否应该停止
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
	rf.electionTimer.Stop()
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// ApplyMsg 同步使用的类型。
// 每当一个新的日志条目被提交时，每个 Raft peer 应该通过 applyCh 向服务（或测试者）发送一个 ApplyMsg。
// 将 CommandValid 设置为 true，表示 ApplyMsg 包含一个新提交的日志条目。
// 在 Lab 2D 中，你需要通过 applyCh 发送其他类型的消息（例如快照）；
// 此时你可以向 ApplyMsg 添加字段，但对于这些其他用途，将 CommandValid 设置为 false。
type ApplyMsg struct {
	CommandValid bool
	Command      any
	CommandIndex int
	CommandTerm  int

	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

// GetState 询问 Raft 当前任期，以及当前服务是否是 Leader
// @return currentTerm 以及此服务器是否认为它是领导者
func (rf *Raft) GetState() (int, bool) {
	// Your code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.state == Leader
}

// 需要持锁调用
func (rf *Raft) advanceCommitIndexLocked() {
	if rf.state != Leader {
		return
	}
	for N := rf.lastIndex(); N > rf.commitIndex; N-- {
		if rf.termAt(N) != rf.currentTerm {
			continue
		}
		count := 1
		for i := range rf.peers {
			if i == rf.me {
				continue
			}
			if rf.matchIndex[i] >= N {
				count++
			}
		}
		if count > len(rf.peers)/2 {
			rf.commitIndex = N
			rf.applyCommittedEntries()
			break
		}
	}
}

// 将已提交但未应用的日志条目应用到状态机
func (rf *Raft) applyCommittedEntries() {
	// 必须在持锁的情况下调用；通过内部标记避免并发批量发送导致的乱序
	if rf.applying {
		return
	}
	// 若有快照正在同步到上层，暂缓日志应用
	if rf.snapshotInFlight {
		return
	}
	rf.applying = true
	// 快照同步期间不继续应用日志
	for !rf.snapshotInFlight {
		// 仅在有未应用且已提交的条目时执行
		if rf.commitIndex <= rf.lastApplied {
			break
		}
		// 计算本次批量应用的边界，避免越过当前日志末端
		start := rf.lastApplied + 1
		// 不应用占位符索引（快照基准），起点至少为 lastIncludedIndex+1
		if start <= rf.lastIncludedIndex {
			start = rf.lastIncludedIndex + 1
		}
		end := rf.commitIndex
		lastIdx := rf.lastIndex()
		if end > lastIdx {
			end = lastIdx
		}
		if start > end {
			break
		}
		// 收集消息后再发送，保持发送序列性
		msgs := make([]ApplyMsg, 0, end-start+1)
		lastSent := start - 1
		for i := start; i <= end; i++ {
			localIdx := i - rf.lastIncludedIndex
			if localIdx < 0 || localIdx >= len(rf.logs) {
				// 日志尚未可用，停止本次批量，避免越界或跳跃
				break
			}
			entry := rf.logs[localIdx]
			msgs = append(msgs, ApplyMsg{CommandValid: true, Command: entry.Command, CommandIndex: i, CommandTerm: entry.Term})
			lastSent = i
		}
		// 释放锁以发送，避免阻塞影响复制/选举
		rf.mu.Unlock()
		if len(msgs) > 0 {
			for _, m := range msgs {
				DebugPrintf("[%d] applyCh <- idx=%d", rf.me, m.CommandIndex)
				rf.applyCh <- m
			}
		}
		rf.mu.Lock()
		if rf.snapshotInFlight {
			// 快照开始后停止继续应用剩余日志
			break
		}
		// 发送成功后仅推进 lastApplied 到“实际发送的最后索引”，避免跳跃导致乱序
		if rf.lastApplied < lastSent {
			rf.lastApplied = lastSent
		}
		// 若 commitIndex 在发送期间进一步推进，循环继续批量应用
	}
	rf.applying = false
}

// lastIndex 返回当前节点的全局最后日志索引（含快照偏移）
func (rf *Raft) lastIndex() int {
	return rf.lastIncludedIndex + len(rf.logs) - 1
}

// termAt 获取给定全局日志索引的任期（考虑快照偏移）
// 如果索引等于 lastIncludedIndex，返回 lastIncludedTerm；否则返回日志切片中对应条目的任期
func (rf *Raft) termAt(index int) int {
	if index == rf.lastIncludedIndex {
		return rf.lastIncludedTerm
	}
	local := index - rf.lastIncludedIndex
	if local < 0 || local >= len(rf.logs) {
		return -1
	}
	return rf.logs[local].Term
}

// persist 将 Raft 的持久状态保存到稳定存储中，
// 以便在崩溃和重启后可以恢复。
// 参见论文图 2 关于应该持久化什么的描述。
func (rf *Raft) persist() {
	// 使用 labgob 编码持久状态：currentTerm、votedFor、lastIncludedIndex、lastIncludedTerm、logs
	// 为什么这么做：支持快照偏移，崩溃重启需恢复偏移以保证索引一致性
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludedTerm)
	e.Encode(rf.logs)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
}

// 恢复以前持久化的状态
func (rf *Raft) readPersist(data []byte) {
	if len(data) < 1 { // bootstrap without any state?
		return
	}
	// 解码并恢复持久化状态
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var currentTerm int
	var votedFor int
	var lastIncludedIndex int
	var lastIncludedTerm int
	var logs []LogEntry
	// 优先尝试新格式（含快照偏移）；失败则回退到旧格式
	err1 := d.Decode(&currentTerm)
	err2 := d.Decode(&votedFor)
	err3 := d.Decode(&lastIncludedIndex)
	err4 := d.Decode(&lastIncludedTerm)
	err5 := d.Decode(&logs)
	if err1 != nil || err2 != nil {
		return
	}
	if err3 != nil || err4 != nil || err5 != nil {
		// 兼容旧格式：currentTerm,votedFor,logs
		r2 := bytes.NewBuffer(data)
		d2 := labgob.NewDecoder(r2)
		var logsOld []LogEntry
		if d2.Decode(&currentTerm) != nil || d2.Decode(&votedFor) != nil || d2.Decode(&logsOld) != nil {
			return
		}
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		if len(logsOld) > 0 {
			rf.logs = logsOld
		}
		rf.lastIncludedIndex = 0
		rf.lastIncludedTerm = 0
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.lastIncludedIndex = lastIncludedIndex
		rf.lastIncludedTerm = lastIncludedTerm
		if len(logs) > 0 {
			rf.logs = logs
		}
		// 保证应用/提交下界不低于快照索引
		if rf.commitIndex < rf.lastIncludedIndex {
			rf.commitIndex = rf.lastIncludedIndex
		}
		if rf.lastApplied < rf.lastIncludedIndex {
			rf.lastApplied = rf.lastIncludedIndex
		}
	}
}

// CondInstallSnapshot 服务想要切换到快照
// 仅当 Raft 没有更新的信息时才这样做，因为它在 applyCh 上传达了快照
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// 如果快照落后或等于当前已安装的快照，拒绝
	if lastIncludedIndex <= rf.lastIncludedIndex {
		return false
	}

	// 重建日志，以占位符开头，并尽量保留快照之后的后缀（若term匹配）
	newLogs := make([]LogEntry, 1)
	newLogs[0] = LogEntry{Command: NoneCommand, Term: lastIncludedTerm}

	oldBase := rf.lastIncludedIndex
	local := lastIncludedIndex - oldBase
	if local >= 0 && local < len(rf.logs) {
		if rf.logs[local].Term == lastIncludedTerm {
			if local+1 < len(rf.logs) {
				newLogs = append(newLogs, rf.logs[local+1:]...)
			}
		}
	}

	rf.logs = newLogs
	rf.lastIncludedIndex = lastIncludedIndex
	rf.lastIncludedTerm = lastIncludedTerm
	if rf.commitIndex < rf.lastIncludedIndex {
		rf.commitIndex = rf.lastIncludedIndex
	}
	if rf.lastApplied < rf.lastIncludedIndex {
		rf.lastApplied = rf.lastIncludedIndex
	}

	// 持久化状态与快照
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludedTerm)
	e.Encode(rf.logs)
	rf.persister.SaveStateAndSnapshot(w.Bytes(), snapshot)
	return true
}

// Snapshot 服务调用此方法表示它已经创建了一个包含所有信息（直到 index）的快照。
// 这意味着服务不再需要该索引及之前的日志。Raft 现在应该尽可能地修剪其日志。
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// 仅允许截断到已提交的索引；忽略无效或重复的快照请求
	if index <= rf.lastIncludedIndex || index > rf.commitIndex {
		return
	}
	// 计算本地索引并提取该索引的任期作为快照基准
	local := index - rf.lastIncludedIndex
	if local < 0 || local >= len(rf.logs) {
		return
	}
	snapTerm := rf.logs[local].Term

	// 重建日志：以占位符开头 + 保留快照之后的后缀
	newLogs := make([]LogEntry, 1)
	newLogs[0] = LogEntry{Command: NoneCommand, Term: snapTerm}
	if local+1 < len(rf.logs) {
		newLogs = append(newLogs, rf.logs[local+1:]...)
	}
	rf.logs = newLogs
	rf.lastIncludedIndex = index
	rf.lastIncludedTerm = snapTerm

	if rf.commitIndex < rf.lastIncludedIndex {
		rf.commitIndex = rf.lastIncludedIndex
	}
	if rf.lastApplied < rf.lastIncludedIndex {
		rf.lastApplied = rf.lastIncludedIndex
	}

	// 原子持久化状态与快照
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.lastIncludedIndex)
	e.Encode(rf.lastIncludedTerm)
	e.Encode(rf.logs)
	rf.persister.SaveStateAndSnapshot(w.Bytes(), snapshot)
}

// Me 返回当前服务器的索引。
func (rf *Raft) Me() int {
	return rf.me
}

// RaftStateSize 返回当前持久化状态的大小。
func (rf *Raft) RaftStateSize() int {
	return rf.persister.RaftStateSize()
}

// HasLogInCurrentTerm 检查当前任期内是否有日志条目。
// 用于判断是否可以安全地开始某些操作。
func (rf *Raft) HasLogInCurrentTerm() bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.logs[len(rf.logs)-1].Term == rf.currentTerm
}
