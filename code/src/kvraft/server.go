// Package kvraft 实现基于 Raft 的键值存储服务器。
package kvraft

import (
	"bytes"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/morsuning/distributed-systems-2021-archives/labgob"
	"github.com/morsuning/distributed-systems-2021-archives/labrpc"
	"github.com/morsuning/distributed-systems-2021-archives/raft"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

type Op struct {
	// 为什么将操作编码进 Raft 日志：通过复制日志驱动状态机，保证顺序与一致性。
	// 携带客户端唯一请求标识实现幂等，避免重试导致的重复写。
	OpType    string // "Get" | "Put" | "Append"
	Key       string
	Value     string
	ClientID  int64
	RequestID int64
}

type KVServer struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	// set by Kill()
	dead int32
	// 如果日志增长到此大小，截取快照。
	maxraftstate int
	// 为什么要保存 Persister：用于检测 Raft 持久化状态大小是否超过阈值，从而在 3B 触发快照以截断日志。
	persister *raft.Persister

	// 你的扩展字段在此处定义。
	// 内部 K/V 状态机映射。
	kvStore map[string]string
	// 客户端去重表：记录每个客户端最后一次应用成功的请求序号。
	lastAppliedReq map[int64]int64
	// 索引到通知通道的映射：用于唤醒等待该日志索引的 RPC 处理协程。
	notify map[int]chan ApplyResult
	// 若某索引在 RPC 处理协程创建通知通道之前就已 apply，则先缓存结果，避免丢失导致等待超时。
	appliedResults map[int]ApplyResult
}

func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	// 读取操作也需要通过 Raft 提交，确保线性一致性。
	op := Op{OpType: "Get", Key: args.Key, ClientID: args.ClientId, RequestID: args.RequestId}
	index, term, isLeader := kv.rf.Start(op)
	if !isLeader {
		reply.Err = ErrWrongLeader
		return
	}
	ch := kv.getNotifyCh(index)
	defer kv.removeNotifyCh(index)

	for {
		select {
		case res := <-ch:
			// 只有当该索引应用的操作与本次请求一致，才返回成功；否则意味着领导者变化导致冲突。
			if sameOp(&res.Op, &op) {
				reply.Err = OK
				reply.Value = res.Value
				return
			}
			reply.Err = ErrWrongLeader
			return
		case <-time.After(500 * time.Millisecond):
			curTerm, stillLeader := kv.rf.GetState()
			// 若当前不是领导者或任期发生变化，返回 WrongLeader 以便客户端切换并重试。
			if !stillLeader || curTerm != term {
				reply.Err = ErrWrongLeader
				return
			}
		}
	}
}

func (kv *KVServer) PutAppend(args *PutAppendArgs, reply *PutAppendReply) {
	// 写操作必须通过 Raft 复制；通过服务端去重和索引匹配通知实现“至多一次”。
	op := Op{OpType: args.Op, Key: args.Key, Value: args.Value, ClientID: args.ClientId, RequestID: args.RequestId}
	index, term, isLeader := kv.rf.Start(op)
	if !isLeader {
		reply.Err = ErrWrongLeader
		return
	}
	ch := kv.getNotifyCh(index)
	defer kv.removeNotifyCh(index)

	for {
		select {
		case res := <-ch:
			if sameOp(&res.Op, &op) {
				reply.Err = OK
				return
			}
			reply.Err = ErrWrongLeader
			return
		case <-time.After(500 * time.Millisecond):
			curTerm, stillLeader := kv.rf.GetState()
			if !stillLeader || curTerm != term {
				reply.Err = ErrWrongLeader
				return
			}
		}
	}
}

// Kill：当某个 KVServer 实例不再需要时，测试框架会调用该方法。
// 为了方便，我们提供了无需加锁即可设置 rf.dead 的代码，
// 以及在长循环中检测 rf.dead 的 killed() 方法。
// 你也可以在 Kill() 中加入自己的代码；你并不需要做任何事情，
// 但这可能很方便（例如）在实例被 Kill 后抑制调试输出。
func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	// 如需，可在此处添加你的代码。
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

// StartKVServer：
// servers[] 包含通过 Raft 协作的服务器端口集合，
// 它们共同组成容错的 K/V 服务。
// me 是当前服务器在 servers[] 中的索引。
// K/V 服务器应通过底层 Raft 存储快照，Raft 应调用 persister.SaveStateAndSnapshot()
// 以原子方式将 Raft 状态与快照一起保存。
// 当 Raft 的持久化状态超过 maxRaftState 字节时，K/V 服务器应触发快照，
// 以便 Raft 对其日志进行垃圾回收；若 maxRaftState 为 -1，则无需快照。
// StartKVServer() 必须尽快返回，因此应为任何长时间运行的工作启动 goroutine。
func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// 对需要进行编解码的结构调用 labgob.Register；
	// 以便 Go 的 RPC 库正确进行序列化/反序列化。
	labgob.Register(Op{})

	kv := new(KVServer)
	kv.me = me
	kv.maxraftstate = maxraftstate

	// 你可能需要在此处进行初始化代码。
	kv.kvStore = make(map[string]string)
	kv.lastAppliedReq = make(map[int64]int64)
	kv.notify = make(map[int]chan ApplyResult)
	kv.appliedResults = make(map[int]ApplyResult)

	kv.applyCh = make(chan raft.ApplyMsg)
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)
	// 保存 persister 引用：服务端需要根据 Raft 持久化大小决定何时触发快照。
	kv.persister = persister

	// 注册快照载荷类型，并在重启时从已有快照恢复服务端状态。
	labgob.Register(KVSnapshot{})
	kv.restoreSnapshot(persister.ReadSnapshot())

	// 启动 apply 循环：按 Raft 提交顺序驱动状态机。
	go kv.applier()

	return kv
}

// ApplyResult 用于在索引维度上向 RPC 处理协程通知对应日志已应用，便于返回结果。
type ApplyResult struct {
	Op    Op
	Value string // 仅 Get 使用
}

// 为什么采用结构体编码：显式定义所需字段，避免使用隐式类型导致 gob 注册或版本演进问题；同时便于扩展。
// KVSnapshot 将服务端状态机（键值映射与客户端去重表）编码到快照中，用于崩溃恢复与 follower 安装快照。
type KVSnapshot struct {
	KV        map[string]string
	LastReqID map[int64]int64
}

// applier 持续消费 Raft 的 applyCh，将日志指令按顺序驱动状态机。
// 去重原因：在网络不可靠或领导者变更下，客户端可能重试，重复的 Put/Append 不应再次生效。
func (kv *KVServer) applier() {
	for msg := range kv.applyCh {
		if msg.CommandValid {
			op := msg.Command.(Op)
			var res ApplyResult
			res.Op = op
			kv.mu.Lock()
			switch op.OpType {
			case "Get":
				// Get 不改变状态，但需要返回当前值。
				res.Value = kv.kvStore[op.Key]
			case "Put":
				if kv.shouldApply(op.ClientID, op.RequestID) {
					kv.kvStore[op.Key] = op.Value
					kv.lastAppliedReq[op.ClientID] = op.RequestID
				}
			case "Append":
				if kv.shouldApply(op.ClientID, op.RequestID) {
					cur := kv.kvStore[op.Key]
					kv.kvStore[op.Key] = cur + op.Value
					kv.lastAppliedReq[op.ClientID] = op.RequestID
				}
			}
			// 先缓存结果，避免丢通知导致 RPC 协程永久等待。
			kv.appliedResults[msg.CommandIndex] = res
			ch := kv.notify[msg.CommandIndex]
			kv.mu.Unlock()
			if ch != nil {
				// 非阻塞投递，避免在无人接收时卡住 apply 循环。
				select {
				case ch <- res:
				default:
				}
			}
			// 为什么在应用后检测：只有已应用的索引才能安全截断，否则会丢失状态机所需的事务。
			kv.maybeSnapshot(msg.CommandIndex)
		} else if msg.SnapshotValid {
			// Handle snapshots delivered from Raft (via leader InstallSnapshot or on restart).
			// 为什么需要 CondInstallSnapshot：原子地将 Raft 状态与快照一并持久化，避免状态-快照不一致。
			ok := kv.rf.CondInstallSnapshot(msg.SnapshotTerm, msg.SnapshotIndex, msg.Snapshot)
			if ok {
				kv.restoreSnapshot(msg.Snapshot)
				// 清理被快照覆盖的索引对应的通知/结果，避免内存泄漏与无效等待。
				kv.trimNotifications(msg.SnapshotIndex)
			}
		}
		if kv.killed() {
			return
		}
	}
}

func (kv *KVServer) shouldApply(clientID int64, reqID int64) bool {
	last, ok := kv.lastAppliedReq[clientID]
	return !ok || reqID > last
}

func (kv *KVServer) getNotifyCh(index int) chan ApplyResult {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	ch, ok := kv.notify[index]
	if !ok {
		ch = make(chan ApplyResult, 1)
		kv.notify[index] = ch
		// 若该索引的结果已产生（在通道创建之前），立即补投一次，避免等待超时。
		if res, exists := kv.appliedResults[index]; exists {
			select {
			case ch <- res:
			default:
			}
		}
	}
	return ch
}

func (kv *KVServer) removeNotifyCh(index int) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	delete(kv.notify, index)
	// 同步删除该索引的缓存结果，避免内存增长。
	delete(kv.appliedResults, index)
}

// maybeSnapshot checks the current Raft persistent state size and triggers snapshot if it exceeds maxraftstate.
// 为什么用大小阈值：当日志无限增长，崩溃恢复与复制成本显著上升；快照用于垃圾回收前缀日志，保持系统可运行与高效。
func (kv *KVServer) maybeSnapshot(appliedIndex int) {
	if kv.maxraftstate == -1 || kv.persister == nil {
		return
	}
	// 简单阈值策略：当 Raft 状态达到/超过上限时，截断至当前已应用索引。
	if kv.persister.RaftStateSize() >= kv.maxraftstate {
		data := kv.encodeSnapshot()
		kv.rf.Snapshot(appliedIndex, data)
	}
}

// encodeSnapshot marshals current service state.
// 为什么编码快照：使 Raft 能原子地持久化状态机与其自身状态，支持崩溃恢复与 follower 重建。
func (kv *KVServer) encodeSnapshot() []byte {
	kv.mu.Lock()
	snap := KVSnapshot{
		KV:        make(map[string]string, len(kv.kvStore)),
		LastReqID: make(map[int64]int64, len(kv.lastAppliedReq)),
	}
	for k, v := range kv.kvStore {
		snap.KV[k] = v
	}
	for cid, rid := range kv.lastAppliedReq {
		snap.LastReqID[cid] = rid
	}
	kv.mu.Unlock()
	var w bytes.Buffer
	e := labgob.NewEncoder(&w)
	_ = e.Encode(snap)
	return w.Bytes()
}

// restoreSnapshot unmarshals service state from snapshot bytes.
// 为什么解码快照：在节点重启或安装快照后，服务端需要恢复状态机与幂等去重表以保证线性一致性与至多一次语义。
func (kv *KVServer) restoreSnapshot(data []byte) {
	if len(data) == 0 {
		return
	}
	var snap KVSnapshot
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	if d.Decode(&snap) != nil {
		// 快照损坏：忽略，避免破坏服务；实际场景可选择报警或降级。
		// 修正：如果快照损坏，必须 Panic，否则会丢失状态导致不一致
		log.Fatal("restoreSnapshot error: decode failed")
	}
	kv.mu.Lock()
	kv.kvStore = make(map[string]string, len(snap.KV))
	for k, v := range snap.KV {
		kv.kvStore[k] = v
	}
	kv.lastAppliedReq = make(map[int64]int64, len(snap.LastReqID))
	for cid, rid := range snap.LastReqID {
		kv.lastAppliedReq[cid] = rid
	}
	kv.mu.Unlock()
}

// trimNotifications removes channels and cached results for indices covered by the snapshot.
// 为什么清理：快照之后，旧索引不会再有应用通知；清理可防止资源泄漏与错误匹配。
func (kv *KVServer) trimNotifications(snapshotIndex int) {
	kv.mu.Lock()
	for idx := range kv.notify {
		if idx <= snapshotIndex {
			delete(kv.notify, idx)
		}
	}
	for idx := range kv.appliedResults {
		if idx <= snapshotIndex {
			delete(kv.appliedResults, idx)
		}
	}
	kv.mu.Unlock()
}

func sameOp(a, b *Op) bool {
	// 辅助函数：判断两个操作是否完全一致，用于通知匹配。
	return a.ClientID == b.ClientID && a.RequestID == b.RequestID && a.OpType == b.OpType && a.Key == b.Key && a.Value == b.Value
}
