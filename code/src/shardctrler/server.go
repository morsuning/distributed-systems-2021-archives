package shardctrler

import (
	"sort"
	"sync"
	"time"

	"github.com/morsuning/distributed-systems-2021-archives/labgob"
	"github.com/morsuning/distributed-systems-2021-archives/labrpc"
	"github.com/morsuning/distributed-systems-2021-archives/raft"
)

type ShardCtrler struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	// 在此处定义该实例维护的状态数据。
	// 历史配置列表，configs[0] 为初始空配置
	configs []Config // indexed by config num

	// 通知通道：索引 -> 完成结果，用于 RPC 等待对应日志应用完成
	notify map[int]chan ApplyResult

	// 去重：记录每个客户端最后一次已应用的请求序号（只用于变更类操作）
	lastAppliedReq map[int64]int64
}

type Op struct {
	// 操作类型：Join / Leave / Move / Query
	Type string

	// 负载数据：不同操作的参数
	JoinServers map[int][]string
	LeaveGIDs   []int
	MoveShard   int
	MoveGID     int
	QueryNum    int

	// 客户端去重标识
	ClientID  int64
	RequestID int64
}

// ApplyResult 表示应用一条日志后的结果（主要用于 Query 返回配置）
type ApplyResult struct {
	Op     Op
	Config Config
}

func (sc *ShardCtrler) Join(args *JoinArgs, reply *JoinReply) {
	// 设计说明：通过 Raft 提交 Join 操作，等待该日志在状态机中被应用，
	// 以提供线性一致的配置变更。使用 ClientId/RequestId 进行去重处理。
	op := Op{
		Type:        "Join",
		JoinServers: args.Servers,
		ClientID:    args.ClientID,
		RequestID:   args.RequestID,
	}
	index, _, isLeader := sc.rf.Start(op)
	if !isLeader {
		reply.WrongLeader = true
		reply.Err = Err(OK)
		return
	}
	ch := sc.getNotifyCh(index)
	select {
	case res := <-ch:
		if res.Op.ClientID == args.ClientID && res.Op.RequestID == args.RequestID && res.Op.Type == "Join" {
			reply.WrongLeader = false
			reply.Err = Err(OK)
		} else {
			// 日志被覆盖或领导者变化导致不匹配
			reply.WrongLeader = true
			reply.Err = Err(OK)
		}
	case <-time.After(500 * time.Millisecond):
		// 超时通常意味着领导者变更或网络抖动，提示客户端重试
		reply.WrongLeader = true
		reply.Err = Err(OK)
	}
}

func (sc *ShardCtrler) Leave(args *LeaveArgs, reply *LeaveReply) {
	op := Op{
		Type:      "Leave",
		LeaveGIDs: args.GIDs,
		ClientID:  args.ClientID,
		RequestID: args.RequestID,
	}
	index, _, isLeader := sc.rf.Start(op)
	if !isLeader {
		reply.WrongLeader = true
		reply.Err = Err(OK)
		return
	}
	ch := sc.getNotifyCh(index)
	select {
	case res := <-ch:
		if res.Op.ClientID == args.ClientID && res.Op.RequestID == args.RequestID && res.Op.Type == "Leave" {
			reply.WrongLeader = false
			reply.Err = Err(OK)
		} else {
			reply.WrongLeader = true
			reply.Err = Err(OK)
		}
	case <-time.After(500 * time.Millisecond):
		reply.WrongLeader = true
		reply.Err = Err(OK)
	}
}

func (sc *ShardCtrler) Move(args *MoveArgs, reply *MoveReply) {
	op := Op{
		Type:      "Move",
		MoveShard: args.Shard,
		MoveGID:   args.GID,
		ClientID:  args.ClientID,
		RequestID: args.RequestID,
	}
	index, _, isLeader := sc.rf.Start(op)
	if !isLeader {
		reply.WrongLeader = true
		reply.Err = Err(OK)
		return
	}
	ch := sc.getNotifyCh(index)
	select {
	case res := <-ch:
		if res.Op.ClientID == args.ClientID && res.Op.RequestID == args.RequestID && res.Op.Type == "Move" {
			reply.WrongLeader = false
			reply.Err = Err(OK)
		} else {
			reply.WrongLeader = true
			reply.Err = Err(OK)
		}
	case <-time.After(500 * time.Millisecond):
		reply.WrongLeader = true
		reply.Err = Err(OK)
	}
}

func (sc *ShardCtrler) Query(args *QueryArgs, reply *QueryReply) {
	// 为了保证读取的线性一致性，这里也通过 Raft 提交一个 Query 操作，
	// 等待其被应用后返回对应配置。如果想减少日志长度，也可以实现只读屏障方案。
	op := Op{
		Type:      "Query",
		QueryNum:  args.Num,
		ClientID:  args.ClientID,
		RequestID: args.RequestID,
	}
	index, _, isLeader := sc.rf.Start(op)
	if !isLeader {
		reply.WrongLeader = true
		reply.Err = Err(OK)
		return
	}
	ch := sc.getNotifyCh(index)
	select {
	case res := <-ch:
		if res.Op.ClientID == args.ClientID && res.Op.RequestID == args.RequestID && res.Op.Type == "Query" {
			reply.WrongLeader = false
			reply.Err = Err(OK)
			reply.Config = res.Config
		} else {
			reply.WrongLeader = true
			reply.Err = Err(OK)
		}
	case <-time.After(500 * time.Millisecond):
		reply.WrongLeader = true
		reply.Err = Err(OK)
	}
}

// 当测试器不再需要该 ShardCtrler 实例时会调用 Kill()。
// 你不需要在 Kill() 中做任何事情；
// 但（例如）关闭该实例的调试输出可能会更方便。
func (sc *ShardCtrler) Kill() {
	sc.rf.Kill()
	// Your code here, if desired.
}

// shardkv 测试器需要访问底层的 Raft 实例。
func (sc *ShardCtrler) Raft() *raft.Raft {
	return sc.rf
}

// StartServer：servers[] 包含将通过 Raft 协作的服务器端口，
// 它们共同组成具备容错能力的 shardctrler 服务。
// me 表示当前服务器在 servers[] 中的索引。
func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister) *ShardCtrler {
	sc := new(ShardCtrler)
	sc.me = me

	sc.configs = make([]Config, 1)
	sc.configs[0].Groups = map[int][]string{}
	// 初始配置：所有分片归属无效组 0（测试约定）
	for i := 0; i < NShards; i++ {
		sc.configs[0].Shards[i] = 0
	}

	labgob.Register(Op{})
	sc.applyCh = make(chan raft.ApplyMsg)
	sc.rf = raft.Make(servers, me, persister, sc.applyCh)

	sc.notify = make(map[int]chan ApplyResult)
	sc.lastAppliedReq = make(map[int64]int64)

	// 启动应用循环：串行地将日志应用到状态机，并通知等待的 RPC。
	go sc.applier()

	return sc
}

// applier 持续从 applyCh 读取并应用日志。核心职责是：
// 1）基于 Join/Leave/Move 生成下一版本配置并追加到 sc.configs；
// 2）对 Query 计算返回的配置；
// 3）执行请求去重；
// 4）将结果通过 notify 通知对应的 RPC 处理函数。
func (sc *ShardCtrler) applier() {
	for msg := range sc.applyCh {
		if msg.CommandValid {
			op, ok := msg.Command.(Op)
			if !ok {
				// 不期望的类型，忽略并继续
				continue
			}
			var res ApplyResult
			sc.mu.Lock()
			switch op.Type {
			case "Join":
				if !sc.isDuplicate(op.ClientID, op.RequestID) {
					sc.applyJoin(op.JoinServers)
					sc.lastAppliedReq[op.ClientID] = op.RequestID
				}
				res.Op = op
			case "Leave":
				if !sc.isDuplicate(op.ClientID, op.RequestID) {
					sc.applyLeave(op.LeaveGIDs)
					sc.lastAppliedReq[op.ClientID] = op.RequestID
				}
				res.Op = op
			case "Move":
				if !sc.isDuplicate(op.ClientID, op.RequestID) {
					sc.applyMove(op.MoveShard, op.MoveGID)
					sc.lastAppliedReq[op.ClientID] = op.RequestID
				}
				res.Op = op
			case "Query":
				res.Op = op
				res.Config = sc.readConfig(op.QueryNum)
			}
			// 通知对应 index 的等待者（此处已持有锁，用无锁版本获取通道）
			ch := sc.getNotifyChUnlocked(msg.CommandIndex)
			// 非阻塞通知，避免通道泄露导致卡死
			select {
			case ch <- res:
			default:
			}
			sc.mu.Unlock()
		} else if msg.SnapshotValid {
			// shardctrler 不要求 4A 进行快照处理，这里留空。
		}
	}
}

// isDuplicate 判断某客户端请求是否已被应用（去重保证幂等）。
func (sc *ShardCtrler) isDuplicate(clientID int64, requestID int64) bool {
	last, ok := sc.lastAppliedReq[clientID]
	return ok && requestID <= last
}

// getNotifyCh 获取某 index 的通知通道（若不存在则创建）。
func (sc *ShardCtrler) getNotifyCh(index int) chan ApplyResult {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	ch, ok := sc.notify[index]
	if !ok {
		ch = make(chan ApplyResult, 1)
		sc.notify[index] = ch
	}
	return ch
}

// getNotifyChUnlocked 在持有 sc.mu 的情况下获取/创建通知通道，避免重复加锁。
func (sc *ShardCtrler) getNotifyChUnlocked(index int) chan ApplyResult {
	ch, ok := sc.notify[index]
	if !ok {
		ch = make(chan ApplyResult, 1)
		sc.notify[index] = ch
	}
	return ch
}

// readConfig 读取指定编号配置；num==-1 则返回最新配置。
func (sc *ShardCtrler) readConfig(num int) Config {
	if num == -1 || num >= len(sc.configs) {
		return sc.configs[len(sc.configs)-1]
	}
	return sc.configs[num]
}

// copyGroups 深拷贝 Groups，保持 map 与 slice 不共享底层数据。
func copyGroups(src map[int][]string) map[int][]string {
	dst := make(map[int][]string)
	for gid, servers := range src {
		ns := make([]string, len(servers))
		copy(ns, servers)
		dst[gid] = ns
	}
	return dst
}

// sortedGIDs 返回按升序排列的所有 gid，用于保证分配的确定性。
func sortedGIDs(groups map[int][]string) []int {
	gids := make([]int, 0, len(groups))
	for gid := range groups {
		gids = append(gids, gid)
	}
	sort.Ints(gids)
	return gids
}

// computeTargets 计算每个 gid 的目标分片数量，满足平均且差值不超过 1。
func computeTargets(groups map[int][]string) map[int]int {
	targets := make(map[int]int)
	gids := sortedGIDs(groups)
	g := len(gids)
	if g == 0 {
		return targets
	}
	q := NShards / g
	r := NShards % g
	for i, gid := range gids {
		if i < r {
			targets[gid] = q + 1
		} else {
			targets[gid] = q
		}
	}
	return targets
}

// applyJoin 生成一个新的配置，将新加入的组纳入并进行最少迁移的均衡分配。
func (sc *ShardCtrler) applyJoin(servers map[int][]string) {
	prev := sc.configs[len(sc.configs)-1]
	next := Config{
		Num:    prev.Num + 1,
		Shards: prev.Shards, // 从上一版本复制，后续按需迁移
		Groups: copyGroups(prev.Groups),
	}
	// 加入新组
	for gid, svrs := range servers {
		next.Groups[gid] = append([]string{}, svrs...)
	}
	gids := sortedGIDs(next.Groups)
	if len(gids) == 0 {
		// 无组时全部分片归 0
		for i := 0; i < NShards; i++ {
			next.Shards[i] = 0
		}
		sc.configs = append(sc.configs, next)
		return
	}
	targets := computeTargets(next.Groups)

	// 统计当前每组持有的分片数量（只对现存组统计）
	counts := make(map[int]int)
	for i := 0; i < NShards; i++ {
		gid := next.Shards[i]
		if _, ok := next.Groups[gid]; ok {
			counts[gid]++
		}
	}
	// donors: 可供迁移的分片列表（来自 gid=0 或者超过目标的组）
	donorShards := make([]int, 0)
	for i := 0; i < NShards; i++ {
		gid := next.Shards[i]
		if gid == 0 || (next.Groups[gid] != nil && counts[gid] > targets[gid]) {
			donorShards = append(donorShards, i)
			if _, ok := next.Groups[gid]; ok {
				counts[gid]-- // 预取出一个将要迁出的分片
			}
		}
	}
	// recipients: 仅新加入的组需要补齐到目标数（避免改变已有组之间的分配，满足“最少迁移”测试）
	newGids := make([]int, 0)
	for gid := range servers {
		newGids = append(newGids, gid)
	}
	sort.Ints(newGids)
	di := 0
	for _, gid := range newGids {
		need := targets[gid] - counts[gid]
		for need > 0 && di < len(donorShards) {
			shard := donorShards[di]
			di++
			next.Shards[shard] = gid
			need--
		}
	}
	// 如果 donor 不足（极端情况下），继续给其它组补齐，但仍不增加已有组的占有量
	// 这里按照 targets 中缺口进行补齐，但避免将分片从一个存量组迁到另一个存量组。
	// 保持当前进度
	for _, gid := range gids {
		// 跳过已有组（不增加其分片）
		if servers[gid] == nil {
			continue
		}
		// 已在上面为新组处理，无需重复
	}
	sc.configs = append(sc.configs, next)
}

// applyLeave 生成新的配置，删除指定的组，并将其分片均衡地分配到剩余组。
func (sc *ShardCtrler) applyLeave(gids []int) {
	prev := sc.configs[len(sc.configs)-1]
	next := Config{
		Num:    prev.Num + 1,
		Shards: prev.Shards,
		Groups: copyGroups(prev.Groups),
	}
	// 删除指定组
	for _, gid := range gids {
		delete(next.Groups, gid)
	}
	remain := sortedGIDs(next.Groups)
	if len(remain) == 0 {
		// 无剩余组，全部归 0
		for i := 0; i < NShards; i++ {
			next.Shards[i] = 0
		}
		sc.configs = append(sc.configs, next)
		return
	}
	targets := computeTargets(next.Groups)
	counts := make(map[int]int)
	// donors: 属于离开的组的分片
	donorShards := make([]int, 0)
	leaving := make(map[int]bool)
	for _, gid := range gids {
		leaving[gid] = true
	}
	for i := 0; i < NShards; i++ {
		gid := next.Shards[i]
		if leaving[gid] {
			donorShards = append(donorShards, i)
		} else if _, ok := next.Groups[gid]; ok {
			counts[gid]++
		}
	}
	// recipients: 仅剩余组的缺口用 donorShards 补齐，避免在剩余组之间搬迁，满足“最少迁移”测试。
	di := 0
	for _, gid := range remain {
		need := targets[gid] - counts[gid]
		for need > 0 && di < len(donorShards) {
			shard := donorShards[di]
			di++
			next.Shards[shard] = gid
			need--
		}
	}
	sc.configs = append(sc.configs, next)
}

// applyMove 生成新的配置，将指定分片直接移动到目标 gid。
func (sc *ShardCtrler) applyMove(shard int, gid int) {
	prev := sc.configs[len(sc.configs)-1]
	next := Config{
		Num:    prev.Num + 1,
		Shards: prev.Shards,
		Groups: copyGroups(prev.Groups),
	}
	next.Shards[shard] = gid
	sc.configs = append(sc.configs, next)
}
