package raft

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
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// import "bytes"
// import "../labgob"

type Stat int32

const (
	Leader Stat = iota
	Follower
	Candidate
	Shutdown
)

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
}

type Log struct {
	Command interface{}
	Term    int32
}

// 1 秒不超过 10 次，即 100 毫秒一次
var heartbeat = 100 * time.Millisecond // 心跳间隔

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

	currentTerm int32 // 当前任期
	votedFor    int   // 投与
	logs        []Log

	voteCount int32 // 投票人数

	commitIndex int // 已知提交的最高日志条目的索引
	lastApplied int // 应用于状态机的最高日志条目的索引

	// 选举后重新初始化
	nextIndex  []int // 对于每个服务器，要发送到该服务器的下一个日志条目的索引, 初始化为领导者最后一个日志索引 + 1
	matchIndex []int // 对于每个服务器，已知在服务器上复制的最高日志条目的索引（初始化为 0，单调增加）

	applyCh chan ApplyMsg
	statCh  chan Stat
}

// GetState 询问 Raft 当前任期，以及当前服务是否是 Leader
// @return currentTerm 以及此服务器是否认为它是领导者
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isLeader bool
	// Your code here (2A).
	term = int(atomic.LoadInt32(&rf.currentTerm))
	isLeader = atomic.LoadInt32((*int32)(&rf.state)) == int32(Leader)
	return term, isLeader
}

// SetState 通知 RaftService 状态变化
func (rf *Raft) SetState(stat Stat) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.state = stat
	rf.statCh <- stat
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

// RequestVoteArgs
// example RequestVote RPC arguments structure.
// NOTE: 字段名称必须以大写字母开头
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int32 // candidate 任期
	CandidateId  int   // 要求投票的候选人
	LastLogIndex int   // 候选人最后一个日志条目的索引
	LastLogTerm  int32 // 候选人最后一个日志条目的任期
}

// RequestVoteReply
// example RequestVote RPC reply structure.
// NOTE: 字段名称必须以大写字母开头
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int32
	VoteGranted bool // true 意味着该 candidate 收到选票
}

// RequestVote 传入 RPC handler
// 候选人在选举期间发起请求投票
// 如果投票人自己的日志比候选人的日志更新，则投票人拒绝投票。
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if args.Term < rf.currentTerm {
		reply.VoteGranted = false
	} else if args.Term == rf.currentTerm {
		if rf.votedFor == -1 || rf.votedFor == args.CandidateId && args.LastLogIndex >= len(rf.logs)-1 {
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
		} else {
			reply.VoteGranted = false
		}
	} else {
		rf.SetState(Follower)
		rf.currentTerm = args.Term
		rf.votedFor = -1
		reply.VoteGranted = true
	}
	reply.Term = rf.currentTerm
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
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// AppendEntries 同步
type AppendEntries struct {
}

type AppendEntryArgs struct {
	Term              int32 // Leader 任期
	LeaderId          int
	PrevLogIndex      int   // 紧接在新条目之前的日志条目的索引
	PrevLogTerm       int32 // prevLogIndex 的任期
	Entries           []Log // 要存储的日志条目（是心跳时为空；为了提高效率可能会发送多个）
	LeaderCommitIndex int   // Leader 的 CommitIndex
}

type AppendEntryReply struct {
	Term    int32
	Success bool // 如果 Follow 包含与 prevLogIndex 和 prevLogTerm 匹配的条目为真
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntryArgs, reply *AppendEntryReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
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
	rf.statCh <- Shutdown
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
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
	rf := new(Raft)
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C)
	rand.Seed(time.Now().Unix())
	go rf.raftService()
	rf.currentTerm = 0
	rf.votedFor = -1 // 即 voteFor 为 null
	rf.voteCount = 0

	rf.commitIndex = 0
	rf.lastApplied = 0

	rf.nextIndex = make([]int, len(peers))
	rf.matchIndex = make([]int, len(peers))
	rf.logs = make([]Log, 0)
	rf.applyCh = applyCh

	rf.SetState(Follower)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	return rf
}

func (rf *Raft) getOvertime() time.Duration {
	return time.Duration(rand.Intn(300-150)+150) * time.Millisecond
}

// raftService Raft 运行 Raft 服务
// 监听状态服务
func (rf *Raft) raftService() {
	ctx, cancel := context.WithCancel(context.Background())
	for {
		curStat := <-rf.statCh
		cancel()
		switch curStat {
		case Follower:
			go rf.followerService(ctx)
		case Leader:
			go rf.leaderService(ctx)
		case Candidate:
			go rf.candidateService(ctx)
		case Shutdown:
			return
		}
	}
}

// leaderService 复制日志，发送心跳包
func (rf *Raft) leaderService(ctx context.Context) {
	leaderContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rf.sendHeartBeat(leaderContext)
	select {
	case <-ctx.Done():
		return
	default:
		// 日志服务
	}
}

// 发送心跳包
func (rf *Raft) sendHeartBeat(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			args := AppendEntryArgs{
				Term:              rf.currentTerm,
				LeaderId:          rf.me,
				PrevLogIndex:      len(rf.logs) - 1,
				PrevLogTerm:       rf.currentTerm,
				LeaderCommitIndex: rf.commitIndex,
			}
			reply := AppendEntryReply{}
			for k := range rf.peers {
				if k != rf.me {
					rf.sendAppendEntries(k, &args, &reply)
					time.Sleep(heartbeat)
				}
			}
		}
	}
}

// followerService
// 响应 leader 和 candidate 的 RPC
// 如果超时没有收到心跳包或者投票请求，则变成candidate
func (rf *Raft) followerService(ctx context.Context) {
	for {
		select {
		case <-rf.applyCh:
		case <-time.After(rf.getOvertime()):
			rf.mu.Lock()
			rf.votedFor = rf.me
			rf.voteCount = 1
			rf.mu.Unlock()
			rf.SetState(Candidate)
		case <-ctx.Done():
			return
		}
	}
}

// candidateService
// 当转换成 candidate 时开始选举
// 如果从半数以上服务器收到选票，变成 leader
func (rf *Raft) candidateService(ctx context.Context) {

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(rf.getOvertime()):
			// 如果选举在一定时间内未完成，重新进行选举
		default:
			rf.election()
		}
	}
}

func (rf *Raft) election() {
	atomic.AddInt32(&rf.currentTerm, 1)
	rf.mu.Lock()
	rf.votedFor = rf.me
	rf.mu.Unlock()
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: len(rf.logs) - 1,
		LastLogTerm:  rf.currentTerm,
	}
	if len(rf.logs) > 0 {
		args.LastLogTerm = rf.logs[args.LastLogIndex].Term
	}
	for server := range rf.peers {
		if server != rf.me {
			go rf.voteResultHandle(server, args)
		}
	}
}

func (rf *Raft) voteResultHandle(server int, args RequestVoteArgs) {
	reply := RequestVoteReply{}
	ok := rf.sendRequestVote(server, &args, &reply)
	if ok {
		if reply.Term > rf.currentTerm {
			rf.mu.Lock()
			rf.currentTerm = reply.Term
			rf.votedFor = -1
			rf.mu.Unlock()
			rf.SetState(Follower)
			return
		}
		if reply.VoteGranted && rf.state == Candidate {
			atomic.AddInt32(&rf.voteCount, 1)
			if int(rf.voteCount) >= len(rf.peers)/2 {
				for i := range rf.peers {
					if i != rf.me {
						rf.nextIndex[i] = len(rf.logs)
						rf.matchIndex[i] = -1
					}
				}
				rf.SetState(Leader)
			}
		}
	}
}
