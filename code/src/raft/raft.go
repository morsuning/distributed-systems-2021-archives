package raft

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

/*
实现 Paper 中描述的 Raft 协议
主要包含四个部分的内容
1. Raft 协议的选举过程
2. 向状态机添加新的日志条目功能
3. 持久化功能，
4. 快照功能
*/

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"../labrpc"
)

// import "bytes"
// import "../labgob"

type Stat int

const (
	Leader Stat = iota
	Follower
	Candidate
)

// Raft A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // 锁定以保护对该对等方状态的共享访问
	peers     []*labrpc.ClientEnd // 所有 peer 的 RPC 端点
	persister *Persister          // 持有此 peer 持久状态的对象
	me        int                 // 当前 peer 在 peers[] 中的索引
	dead      int32               // set by Kill()

	// Your data here (2A, 2B, 2C).
	// Raft 服务器需要维护的状态见论文 图 2

	state Stat // 当前状态

	currentTerm int // 当前任期
	votedFor    int // 投与

	logs []LogEntry

	commitIndex int // 已知提交的最高日志条目的索引
	lastApplied int // 应用于状态机的最高日志条目的索引

	// 选举后重新初始化
	nextIndex  []int // 对于每个服务器，要发送到该服务器的下一个日志条目的索引, 初始化为领导者最后一个日志索引 + 1
	matchIndex []int // 对于每个服务器，已知在服务器上复制的最高日志条目的索引（初始化为 0，单调增加）

	electionTimer   *time.Timer
	heartbeatTicker *time.Ticker
}

// Make 创建 Raft 服务器实例
// @para
// 所有 Raft 服务器的端口在数组 peer[] 中，所有服务中 peer 数组的顺序是一致的
// 本服务端口是 peer[me]
// persister 用来存储服务器自身的状态，同时初始化最近接收的状态
// applyCh 用来发送 ApplyMsg 信息
// Note: Make()必须快速返回，需要用 goroutines 运行长时任务
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C)
	rf.state = Follower

	rf.currentTerm = 0
	rf.votedFor = -1 // 即 voteFor 为 null
	rf.commitIndex = 0
	rf.lastApplied = 0

	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))
	rf.logs = []LogEntry{}

	rf.electionTimer = time.NewTimer(time.Duration(rand.Intn(500-350)+350) * time.Millisecond)
	rf.heartbeatTicker = time.NewTicker(100 * time.Millisecond)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	go rf.ticker()
	DPrintf("[%d] Initialized", rf.me)
	return rf
}

func (rf *Raft) resetElectionTimer() {
	if rf.electionTimer.Stop() {
		select {
		case <-rf.electionTimer.C:
		default:
		}
	}
	rf.electionTimer.Reset(time.Duration(rand.Intn(500-400)+400) * time.Millisecond)
}

// ticker 如果最近没有收到 heartbeat 开始新的选举
func (rf *Raft) ticker() {
	for rf.killed() == false {
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
			// 100 毫秒向全体发送一次 heartbeat
		case <-rf.heartbeatTicker.C:
			rf.mu.Lock()
			state := rf.state
			rf.mu.Unlock()
			if state == Leader {
				rf.sendAppendEntries()
				DPrintf("[%d] Leader are sending appendEntries", rf.me)
			}
		}
	}
}

func (rf *Raft) Election() {
	rf.mu.Lock()
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	DPrintf("[%d] Attempting an election at term %d", rf.me, rf.currentTerm)
	term := rf.currentTerm
	voteReceived := 1
	finished := false
	rf.mu.Unlock()
	// ↑参与选举的代码和实际处理投票的代码↓
	for server := range rf.peers {
		if server != rf.me {
			go func(server int) {
				voteGranted := rf.RequestVote(server, term)
				if !voteGranted {
					return
				}
				rf.mu.Lock()
				voteReceived++
				DPrintf("[%d] Got vote from %d, current received votes: %d", rf.me, server, voteReceived)
				if finished || voteReceived <= len(rf.peers)>>1 {
					rf.mu.Unlock()
					return
				}
				finished = true
				// 实际上处理投票的代码必须确保自己统计的是哪一个 term
				// 因为可能有其他 term 更新 peer 的 RPC 请求，将自己的 term 和 state 改了
				// 解决方案：1. 处理投票时不投给别人 2. 检查是否在同一个 term 中且 state 仍然是 candidate
				if rf.state != Candidate || rf.currentTerm != term {
					return
				}
				DPrintf("[%d] Vote is enough, now becomeing leader (currentTerm=%d, state=%d)", rf.me, rf.currentTerm, rf.state)
				rf.state = Leader
				rf.resetElectionTimer()
				rf.mu.Unlock()
				rf.sendAppendEntries()
			}(server)
		}
	}
}

// RequestVoteArgs
// example RequestVote RPC arguments structure.
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int // candidate 任期
	CandidateId  int // 要求投票的候选人
	LastLogIndex int // 候选人最后一个日志条目的索引
	LastLogTerm  int // 候选人最后一个日志条目的任期
}

// RequestVoteReply
// example RequestVote RPC reply structure.
// NOTE: 字段名称必须以大写字母开头
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool // true 意味着该 candidate 收到选票
}

// RequestVote 准备参数或处理响应时需要用到锁，等待响应时不应持有锁，否则此时服务将处于不可用状态
// 返回是否成功得到选票
func (rf *Raft) RequestVote(server int, term int) bool {
	// DPrintf("[%d] Sending request vote to %d", rf.me, server)
	lastLogTerm := 0
	if len(rf.logs) > 0 {
		lastLogTerm = rf.logs[len(rf.logs)-1].Term
	}
	args := RequestVoteArgs{
		Term:         term,
		CandidateId:  rf.me,
		LastLogIndex: len(rf.logs),
		LastLogTerm:  lastLogTerm,
	}
	reply := RequestVoteReply{}
	// 不应在 RPC 调用期间持有锁，不然无法相应 RPC 请求
	ok := rf.sendRequestVote(server, &args, &reply)
	// DPrintf("[%d] Finish sending request vote to %d", rf.me, server)
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
		return true
	}
	if reply.VoteGranted && rf.state == Candidate {
		return true
	}
	return false
}

// RequestVoteHandle 传入 RPC handler
// 候选人在选举期间发起请求投票
// 如果投票人自己的日志比候选人的日志更新，则投票人拒绝投票。
func (rf *Raft) RequestVoteHandle(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	// DPrintf("[%d] Received request vote from %d", rf.me, args.CandidateId)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// DPrintf()("[%d] Handling request vote from %d", rf.me, args.CandidateId)
	reply.VoteGranted = false
	// 回复中是任期自己当前任期
	reply.Term = rf.currentTerm
	// 请求投票者比自己任期小，将不给它投
	if args.Term < rf.currentTerm {
		return
		// 请求投票者任期和自己相同
	}
	// 请求投票者任期大于自己
	if args.Term > rf.currentTerm {
		rf.state = Follower
		DPrintf("[%d] Become a Follower", rf.me)
		rf.currentTerm = args.Term
		rf.votedFor = -1
		reply.VoteGranted = true
		DPrintf("[%d] Granting vote for %d on its term %d", rf.me, args.CandidateId, args.Term)
		return
	}
	if args.Term == rf.currentTerm {
		if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
			lastLogTerm := 0
			if len(rf.logs) > 0 {
				lastLogTerm = rf.logs[len(rf.logs)-1].Term
			}
			// 请求投票者日志最后任期比自己新
			if args.LastLogTerm > lastLogTerm {
				reply.VoteGranted = true
				rf.votedFor = args.CandidateId
				DPrintf("[%d] Granting vote for %d on its term %d", rf.me, args.CandidateId, args.Term)
				return
			}
			// 日志和自己一样
			if args.LastLogTerm == lastLogTerm && len(rf.logs) <= args.LastLogIndex {
				reply.VoteGranted = true
				rf.votedFor = args.CandidateId
				DPrintf("[%d] Granting vote for %d on its term %d", rf.me, args.CandidateId, args.Term)
				return
			}
		}
	}
}

// 发送 RPC
// example code to send a RequestVote RPC to a server.
// server 是目标服务器在 rf.peers[] 中的索引, 需要 args 中的 RPC 参数
// NOTE:传递给 Call() 的参数和回复的类型必须与声明在 handler 中的参数类型相同（包括是否为指针）
// labrpc 包模拟有损网络，其中服务器可能无法访问，其中请求和回复可能会丢失
// Call() 发送请求并等待回复。如果在超时间隔内收到回复，Call() 返回 true
// 否则 Call() 返回 false。因此 Call() 可能暂时不会返回
// 一个错误的返回可能是由一个死服务器引起的，或是一个活跃的服务器无法访问、请求丢失或回复丢失
// Call() 保证返回（可能在延迟之后）*除非*如果，服务器端的处理函数不返回
// 因此有不需要在 Call() 周围实现自己的超时
// 查看 ../labrpc/labrpc.go 中的注释以获取更多详细信息
// 如果您在使 RPC 工作时遇到问题，请检查您是否已将通过 RPC 传递的结构中的所有字段名称大写
// 并且调用者使用 & 传递回复结构的地址，而不是结构体本身
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVoteHandle", args, reply)
	return ok
}

type LogEntry struct {
	Command interface{}
	Term    int
}

// AppendEntries 同步
type AppendEntries struct {
}

type AppendEntryArgs struct {
	Term              int // Leader 任期
	LeaderId          int
	PrevLogIndex      int        // 紧接在新条目之前的日志条目的索引
	PrevLogTerm       int        // prevLogIndex 的任期
	Entries           []LogEntry // 要存储的日志条目（是心跳时为空；为了提高效率可能会发送多个）
	LeaderCommitIndex int        // Leader 的 CommitIndex
}

type AppendEntryReply struct {
	Term    int
	Success bool // 如果 Follow 包含与 prevLogIndex 和 prevLogTerm 匹配的条目为真
}

func (rf *Raft) sendAppendEntries() {
	rf.mu.Lock()
	args := AppendEntryArgs{
		Term:              rf.currentTerm,
		LeaderId:          rf.me,
		Entries:           rf.logs,
		LeaderCommitIndex: rf.commitIndex,
	}
	rf.mu.Unlock()
	for server := range rf.peers {
		reply := AppendEntryReply{}
		go func(server int) {
			if server != rf.me {
				ok := rf.peers[server].Call("Raft.AppendEntries", &args, &reply)
				DPrintf("[%d] Sending the AppendEntries to %d", rf.me, server)
				if !ok {
					DPrintf("[%d] Sending the AppendEntry failed", rf.me)
				}
				// TODO 处理 Reply
			}
		}(server)
	}
}

func (rf *Raft) AppendEntries(args *AppendEntryArgs, reply *AppendEntryReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm
	if args.Term < rf.currentTerm {
		reply.Success = false
	} else {
		reply.Success = true
		rf.resetElectionTimer()
		DPrintf("[%d] Received heartbeat from %d", rf.me, args.LeaderId)
		rf.state = Follower
		// DPrintf("[%d] Become a Follower", rf.me)
		rf.currentTerm = args.Term
	}
}

// Start 在新的时刻开始共识同步
// 使用 Raft 的服务（例如 k/v 服务器）想要启动
// 就下一个要附加到 Raft 日志的命令达成一致
// 如果这个服务器不是领导者，返回 false
// 否则启动共识并立即返回。不能保证这命令将永远提交到 Raft 日志
// 因为领导者可能会失败或输掉选举。即使 Raft 实例已被杀死，这个函数应该优雅地返回
// @return 第一个返回值是命令出现的索引，如果它曾经提交过。
// 第二个返回值是当前任期
// 如果此服务器是 leader，则第三个返回值为 true
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).

	return index, term, isLeader
}

// Kill
// 测试者不会在每次测试后停止 Raft 创建的 goroutines，但它确实调用了 Kill() 方法
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

// ApplyMsg 同步使用的类型，每次将新条目提交日志时，每个 Raft peer，应该向服务发送 ApplyMsg （或测试员）
// 当每个 Raft peer 意识到连续的日志条目已提交，该 peer 应该发送一个 ApplyMsg，经由 applyCh（由 Make()创建的）
// 将 CommandValid 设置为 true，以表示 ApplyMsg 包含一个新提交的日志条目
// in Lab 3 you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh; at that point you can add fields to
// ApplyMsg, but set CommandValid to false for these other uses.
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

// GetState 询问 Raft 当前任期，以及当前服务是否是 Leader
// @return currentTerm 以及此服务器是否认为它是领导者
func (rf *Raft) GetState() (int, bool) {
	var term int
	var isLeader bool
	// Your code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.currentTerm
	isLeader = rf.state == Leader
	return term, isLeader
}

// save Raft's persistent state to stable storage,
// 将 Raft 中的连续的状态存储为稳定的状态，以后可以在崩溃和重新启动后检索它。
// @see paper's Figure 2 for a description of what should be persistent.
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

// 恢复以前持久化的状态
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

// CondInstallSnapshot
// 服务想要切换到快照
// 仅当 Raft 没有更新的信息时才这样做，因为它在 applyCh 上传达了快照
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {

	return true
}

// Snapshot
// 该服务表示，它已经创建了一个快照，其中包含所有信息，包括索引。
// 这意味着服务不再需要该索引的日志（包括该索引）。Raft现在应该尽可能地修剪其日志。
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

}
