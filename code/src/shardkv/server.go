package shardkv

import (
	"bytes"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/morsuning/distributed-systems-2021-archives/labgob"
	"github.com/morsuning/distributed-systems-2021-archives/labrpc"
	"github.com/morsuning/distributed-systems-2021-archives/raft"
	"github.com/morsuning/distributed-systems-2021-archives/shardctrler"
)

const (
	ConfigureMonitorTimeout   = 100 * time.Millisecond
	MigrationMonitorTimeout   = 50 * time.Millisecond
	GCMonitorTimeout          = 50 * time.Millisecond
	EmptyEntryDetectorTimeout = 200 * time.Millisecond
)

type Shard struct {
	// KV 存储分片内的数据
	KV map[string]string
	// Status 分片状态：Serving, Pulling, BePulling, GCing
	Status ShardStatus
}

func NewShard() *Shard {
	return &Shard{make(map[string]string), Serving}
}

func (shard *Shard) Get(key string) (string, Err) {
	if value, ok := shard.KV[key]; ok {
		return value, OK
	}
	return "", ErrNoKey
}

func (shard *Shard) Put(key, value string) Err {
	shard.KV[key] = value
	return OK
}

func (shard *Shard) Append(key, value string) Err {
	shard.KV[key] += value
	return OK
}

func (shard *Shard) deepCopy() map[string]string {
	newShard := make(map[string]string)
	for k, v := range shard.KV {
		newShard[k] = v
	}
	return newShard
}

type ShardKV struct {
	mu      sync.RWMutex
	me      int
	dead    int32
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg

	makeEnd func(string) *labrpc.ClientEnd
	gid     int
	sc      *shardctrler.Clerk

	maxRaftState int // 如果日志增长到这个大小则进行快照
	lastApplied  int // 记录 lastApplied 以防止状态机回滚

	lastConfig    shardctrler.Config
	currentConfig shardctrler.Config

	stateMachines  map[int]*Shard                // KV 状态机
	lastOperations map[int64]OperationContext    // 通过记录 clientId 对应的最后一个 commandId 和响应来确定日志是否重复
	notifyChans    map[int]chan *CommandResponse // 由 applier 协程通知 client 协程进行响应
}

func (kv *ShardKV) Get(args *CommandRequest, reply *CommandResponse) {
	// 将在后续步骤中实现，但如果使用旧的 RPC，需要签名匹配。
	// 然而，设计文档使用统一的 CommandRequest。
	// 原始代码有 Get(GetArgs, GetReply)。
	kv.Command(args, reply)
}

func (kv *ShardKV) PutAppend(args *CommandRequest, reply *CommandResponse) {
	kv.Command(args, reply)
}

// Command 处理所有客户端请求
func (kv *ShardKV) Command(request *CommandRequest, response *CommandResponse) {
	kv.mu.RLock()
	// 如果请求重复，直接返回结果，无需 raft 层参与
	if request.Op != OpGet && kv.isDuplicateRequest(request.ClientId, request.CommandId) {
		lastResponse := kv.lastOperations[request.ClientId].LastResponse
		response.Value, response.Err = lastResponse.Value, lastResponse.Err
		kv.mu.RUnlock()
		return
	}
	// 如果当前分片无法服务该 key，直接返回 ErrWrongGroup，让客户端获取最新配置并重试
	if !kv.canServe(key2shard(request.Key)) {
		response.Err = ErrWrongGroup
		kv.mu.RUnlock()
		return
	}
	kv.mu.RUnlock()
	kv.Execute(NewOperationCommand(request), response)
}

func (kv *ShardKV) Execute(command Command, response *CommandResponse) {
	index, term, isLeader := kv.rf.Start(command)
	if !isLeader {
		response.Err = ErrWrongLeader
		return
	}
	defer DPrintf("{Node %v}{Group %v} processes Command %v with CommandResponse %v", kv.rf.Me(), kv.gid, command, response)
	kv.mu.Lock()
	ch := kv.getNotifyChan(index)
	kv.mu.Unlock()
	select {
	case result := <-ch:
		response.Value, response.Err = result.Value, result.Err
	case <-time.After(500 * time.Millisecond):
		response.Err = ErrWrongLeader
	}
	go func() {
		kv.mu.Lock()
		kv.removeNotifyChan(index)
		kv.mu.Unlock()
	}()
	// 验证任期是否改变（失去领导权）
	if currentTerm, _ := kv.rf.GetState(); currentTerm != term {
		response.Err = ErrWrongLeader
	}
}

func (kv *ShardKV) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
}

func (kv *ShardKV) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

func StartServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxRaftState int, gid int, ctrlers []*labrpc.ClientEnd, makeEnd func(string) *labrpc.ClientEnd) *ShardKV {
	labgob.Register(Command{})
	labgob.Register(CommandRequest{})
	labgob.Register(shardctrler.Config{})
	labgob.Register(ShardOperationResponse{})
	labgob.Register(ShardOperationRequest{})

	applyCh := make(chan raft.ApplyMsg)

	kv := &ShardKV{
		dead:           0,
		me:             me,
		rf:             raft.Make(servers, me, persister, applyCh),
		applyCh:        applyCh,
		makeEnd:        makeEnd,
		gid:            gid,
		sc:             shardctrler.MakeClerk(ctrlers),
		lastApplied:    0,
		maxRaftState:   maxRaftState,
		currentConfig:  shardctrler.DefaultConfig(),
		lastConfig:     shardctrler.DefaultConfig(),
		stateMachines:  make(map[int]*Shard),
		lastOperations: make(map[int64]OperationContext),
		notifyChans:    make(map[int]chan *CommandResponse),
	}

	// 初始化分片
	for i := 0; i < shardctrler.NShards; i++ {
		kv.stateMachines[i] = NewShard()
	}

	kv.restoreSnapshot(persister.ReadSnapshot())

	go kv.applier()
	go kv.Monitor(kv.configureAction, ConfigureMonitorTimeout)
	go kv.Monitor(kv.migrationAction, MigrationMonitorTimeout)
	go kv.Monitor(kv.gcAction, GCMonitorTimeout)
	go kv.Monitor(kv.checkEntryInCurrentTermAction, EmptyEntryDetectorTimeout)

	DPrintf("{Node %v}{Group %v} has started", kv.rf.Me(), kv.gid)
	return kv
}

func (kv *ShardKV) Monitor(action func(), timeout time.Duration) {
	for !kv.killed() {
		if _, isLeader := kv.rf.GetState(); isLeader {
			action()
		}
		time.Sleep(timeout)
	}
}

func (kv *ShardKV) applier() {
	for !kv.killed() {
		message := <-kv.applyCh
		DPrintf("{Node %v}{Group %v} tries to apply message %v", kv.rf.Me(), kv.gid, message)
		if message.CommandValid {
			kv.mu.Lock()
			if message.CommandIndex <= kv.lastApplied {
				DPrintf("{Node %v}{Group %v} discards outdated message %v because a newer snapshot which lastApplied is %v has been restored", kv.rf.Me(), kv.gid, message, kv.lastApplied)
				kv.mu.Unlock()
				continue
			}
			kv.lastApplied = message.CommandIndex

			var response *CommandResponse
			command := message.Command.(Command)
			switch command.Op {
			case Operation:
				operation := command.Data.(CommandRequest)
				response = kv.applyOperation(&message, &operation)
			case Configuration:
				nextConfig := command.Data.(shardctrler.Config)
				response = kv.applyConfiguration(&nextConfig)
			case InsertShards:
				shardsInfo := command.Data.(ShardOperationResponse)
				response = kv.applyInsertShards(&shardsInfo)
			case DeleteShards:
				shardsInfo := command.Data.(ShardOperationRequest)
				response = kv.applyDeleteShards(&shardsInfo)
			case EmptyEntry:
				response = kv.applyEmptyEntry()
			}

			// 仅当节点为 leader 时，通知当前任期日志的相关通道
			if currentTerm, isLeader := kv.rf.GetState(); isLeader && message.CommandTerm == currentTerm {
				ch := kv.getNotifyChan(message.CommandIndex)
				ch <- response
			}

			needSnapshot := kv.needSnapshot()
			if needSnapshot {
				kv.takeSnapshot(message.CommandIndex)
			}
			kv.mu.Unlock()
		} else if message.SnapshotValid {
			kv.mu.Lock()
			if kv.rf.CondInstallSnapshot(message.SnapshotTerm, message.SnapshotIndex, message.Snapshot) {
				kv.restoreSnapshot(message.Snapshot)
				kv.lastApplied = message.SnapshotIndex
			}
			kv.mu.Unlock()
		} else {
			// panic(fmt.Sprintf("unexpected Message %v", message))
		}
	}
}

// 辅助函数

func (kv *ShardKV) isDuplicateRequest(clientId int64, commandId int64) bool {
	operationContext, ok := kv.lastOperations[clientId]
	return ok && commandId <= operationContext.MaxAppliedCommandId
}

func (kv *ShardKV) canServe(shardID int) bool {
	return kv.currentConfig.Shards[shardID] == kv.gid && (kv.stateMachines[shardID].Status == Serving || kv.stateMachines[shardID].Status == GCing)
}

func (kv *ShardKV) getNotifyChan(index int) chan *CommandResponse {
	if _, ok := kv.notifyChans[index]; !ok {
		kv.notifyChans[index] = make(chan *CommandResponse, 1)
	}
	return kv.notifyChans[index]
}

func (kv *ShardKV) removeNotifyChan(index int) {
	delete(kv.notifyChans, index)
}

func (kv *ShardKV) needSnapshot() bool {
	if kv.maxRaftState == -1 {
		return false
	}
	return kv.rf.RaftStateSize() >= kv.maxRaftState
}

func (kv *ShardKV) takeSnapshot(index int) {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(kv.stateMachines)
	e.Encode(kv.lastOperations)
	e.Encode(kv.currentConfig)
	e.Encode(kv.lastConfig)
	kv.rf.Snapshot(index, w.Bytes())
}

func (kv *ShardKV) restoreSnapshot(snapshot []byte) {
	if len(snapshot) < 1 {
		return
	}
	r := bytes.NewBuffer(snapshot)
	d := labgob.NewDecoder(r)
	var stateMachines map[int]*Shard
	var lastOperations map[int64]OperationContext
	var currentConfig shardctrler.Config
	var lastConfig shardctrler.Config
	if d.Decode(&stateMachines) != nil ||
		d.Decode(&lastOperations) != nil ||
		d.Decode(&currentConfig) != nil ||
		d.Decode(&lastConfig) != nil {
		log.Fatal("restoreSnapshot error")
	}
	kv.stateMachines = stateMachines
	kv.lastOperations = lastOperations
	kv.currentConfig = currentConfig
	kv.lastConfig = lastConfig
}

// configureAction 定期从 shardctrler 获取最新配置。
// 如果当前配置不是最新的且所有分片都处于 Serving 状态，
// 它将向 Raft 日志提交一个 Configuration 命令。
func (kv *ShardKV) configureAction() {
	canGetNextConfig := true
	kv.mu.RLock()
	for _, shard := range kv.stateMachines {
		if shard.Status != Serving {
			canGetNextConfig = false
			break
		}
	}
	currentConfigNum := kv.currentConfig.Num
	kv.mu.RUnlock()

	if canGetNextConfig {
		nextConfig := kv.sc.Query(currentConfigNum + 1)
		if nextConfig.Num == currentConfigNum+1 {
			kv.Execute(NewConfigurationCommand(&nextConfig), &CommandResponse{})
		}
	}
}

// applyConfiguration 将新配置应用到 ShardKV 服务器。
// 它更新当前和最后的配置，并根据所有权变更调整每个分片的状态。
func (kv *ShardKV) applyConfiguration(nextConfig *shardctrler.Config) *CommandResponse {
	if nextConfig.Num == kv.currentConfig.Num+1 {
		kv.lastConfig = kv.currentConfig
		kv.currentConfig = *nextConfig

		for i := 0; i < shardctrler.NShards; i++ {
			// 分片从另一个组移动到该组
			if kv.currentConfig.Shards[i] == kv.gid && kv.lastConfig.Shards[i] != kv.gid {
				if kv.lastConfig.Shards[i] == 0 {
					kv.stateMachines[i].Status = Serving
				} else {
					kv.stateMachines[i].Status = Pulling
				}
			}
			// 分片从该组移动到另一个组
			if kv.lastConfig.Shards[i] == kv.gid && kv.currentConfig.Shards[i] != kv.gid {
				if kv.currentConfig.Shards[i] == 0 {
					// 如果新组是 0（无效），这意味着分片未分配。
					// 我们应该重置为 Serving 状态（空），而不是 BePulling，因为没有组会来拉取数据。
					kv.stateMachines[i].Status = Serving
					kv.stateMachines[i].KV = make(map[string]string)
				} else {
					kv.stateMachines[i].Status = BePulling
				}
			}
		}
	}
	return &CommandResponse{Err: OK}
}

// 后续步骤中要实现的动作的占位符实现
func (kv *ShardKV) migrationAction() {
	kv.mu.RLock()
	gidToShards := make(map[int][]int)
	for shardID, shard := range kv.stateMachines {
		if shard.Status == Pulling {
			prevGID := kv.lastConfig.Shards[shardID]
			gidToShards[prevGID] = append(gidToShards[prevGID], shardID)
		}
	}
	currentConfigNum := kv.currentConfig.Num
	kv.mu.RUnlock()

	for gid, shardIDs := range gidToShards {
		go func(gid int, shardIDs []int) {
			servers := kv.lastConfig.Groups[gid]
			args := ShardOperationRequest{currentConfigNum, shardIDs}
			for _, server := range servers {
				srv := kv.makeEnd(server)
				var reply ShardOperationResponse
				if ok := srv.Call("ShardKV.GetShardsData", &args, &reply); ok && reply.Err == OK {
					kv.Execute(NewInsertShardsCommand(&reply), &CommandResponse{})
					return
				}
			}
		}(gid, shardIDs)
	}
}

func (kv *ShardKV) gcAction() {
	kv.mu.RLock()
	gidToShards := make(map[int][]int)
	for shardID, shard := range kv.stateMachines {
		if shard.Status == GCing {
			prevGID := kv.lastConfig.Shards[shardID]
			gidToShards[prevGID] = append(gidToShards[prevGID], shardID)
		}
	}
	currentConfigNum := kv.currentConfig.Num
	kv.mu.RUnlock()

	for gid, shardIDs := range gidToShards {
		go func(gid int, shardIDs []int) {
			servers := kv.lastConfig.Groups[gid]
			args := ShardOperationRequest{currentConfigNum, shardIDs}
			for _, server := range servers {
				srv := kv.makeEnd(server)
				var reply ShardOperationResponse
				if ok := srv.Call("ShardKV.DeleteShardsData", &args, &reply); ok && reply.Err == OK {
					kv.Execute(NewDeleteShardsCommand(&args), &CommandResponse{})
					return
				}
			}
		}(gid, shardIDs)
	}
}

func (kv *ShardKV) checkEntryInCurrentTermAction() {
	if !kv.rf.HasLogInCurrentTerm() {
		kv.Execute(NewEmptyEntryCommand(), &CommandResponse{})
	}
}

func (kv *ShardKV) GetShardsData(request *ShardOperationRequest, response *ShardOperationResponse) {
	if _, isLeader := kv.rf.GetState(); !isLeader {
		response.Err = ErrWrongLeader
		return
	}
	kv.mu.RLock()
	defer kv.mu.RUnlock()

	if request.ConfigNum != kv.currentConfig.Num {
		response.Err = ErrNotReady
		return
	}

	response.Shards = make(map[int]map[string]string)
	for _, shardID := range request.ShardIDs {
		if kv.stateMachines[shardID].Status == BePulling || kv.stateMachines[shardID].Status == GCing {
			response.Shards[shardID] = kv.stateMachines[shardID].deepCopy()
		}
	}
	response.LastOperations = make(map[int64]OperationContext)
	for k, v := range kv.lastOperations {
		response.LastOperations[k] = v
	}
	DPrintf("{Node %v}{Group %v} sending GetShardsData response with %d lastOperations", kv.rf.Me(), kv.gid, len(response.LastOperations))
	response.ConfigNum = request.ConfigNum
	response.Err = OK
}

func (kv *ShardKV) DeleteShardsData(request *ShardOperationRequest, response *ShardOperationResponse) {
	if _, isLeader := kv.rf.GetState(); !isLeader {
		response.Err = ErrWrongLeader
		return
	}
	kv.mu.RLock()
	if request.ConfigNum < kv.currentConfig.Num {
		response.Err = OK
		kv.mu.RUnlock()
		return
	}
	if request.ConfigNum > kv.currentConfig.Num {
		response.Err = ErrNotReady
		kv.mu.RUnlock()
		return
	}
	kv.mu.RUnlock()

	kv.Execute(NewDeleteShardsCommand(request), &CommandResponse{})
	response.Err = OK
}

func (kv *ShardKV) applyOperation(message *raft.ApplyMsg, operation *CommandRequest) *CommandResponse {
	var response *CommandResponse
	shardID := key2shard(operation.Key)
	if kv.canServe(shardID) {
		if operation.Op != OpGet && kv.isDuplicateRequest(operation.ClientId, operation.CommandId) {
			DPrintf("{Node %v}{Group %v} doesn't apply duplicated message %v to stateMachine because maxAppliedCommandId is %v for client %v", kv.rf.Me(), kv.gid, message, kv.lastOperations[operation.ClientId], operation.ClientId)
			return kv.lastOperations[operation.ClientId].LastResponse
		} else {
			response = kv.applyLogToStateMachines(operation, shardID)
			if operation.Op != OpGet {
				kv.lastOperations[operation.ClientId] = OperationContext{operation.CommandId, response}
				DPrintf("{Node %v}{Group %v} updated lastOperations for Client %v: CommandId %v", kv.rf.Me(), kv.gid, operation.ClientId, operation.CommandId)
			}
			return response
		}
	}
	return &CommandResponse{ErrWrongGroup, ""}
}

func (kv *ShardKV) applyLogToStateMachines(operation *CommandRequest, shardID int) *CommandResponse {
	var value string
	var err Err
	switch operation.Op {
	case OpPut:
		err = kv.stateMachines[shardID].Put(operation.Key, operation.Value)
	case OpAppend:
		err = kv.stateMachines[shardID].Append(operation.Key, operation.Value)
	case OpGet:
		value, err = kv.stateMachines[shardID].Get(operation.Key)
	}
	return &CommandResponse{err, value}
}

func (kv *ShardKV) applyInsertShards(shardsInfo *ShardOperationResponse) *CommandResponse {
	if shardsInfo.ConfigNum == kv.currentConfig.Num {
		for shardID, shardData := range shardsInfo.Shards {
			shard := kv.stateMachines[shardID]
			if shard.Status == Pulling {
				newShardData := make(map[string]string)
				for k, v := range shardData {
					newShardData[k] = v
				}
				shard.KV = newShardData
				shard.Status = GCing
			}
		}
		for clientId, opCtx := range shardsInfo.LastOperations {
			if lastOp, ok := kv.lastOperations[clientId]; !ok || lastOp.MaxAppliedCommandId < opCtx.MaxAppliedCommandId {
				kv.lastOperations[clientId] = opCtx
				DPrintf("{Node %v}{Group %v} merged lastOperations for Client %v: CommandId %v", kv.rf.Me(), kv.gid, clientId, opCtx.MaxAppliedCommandId)
			}
		}
	}
	return &CommandResponse{Err: OK}
}

func (kv *ShardKV) applyDeleteShards(shardsInfo *ShardOperationRequest) *CommandResponse {
	if shardsInfo.ConfigNum == kv.currentConfig.Num {
		for _, shardID := range shardsInfo.ShardIDs {
			shard := kv.stateMachines[shardID]
			switch shard.Status {
			case GCing:
				shard.Status = Serving
			case BePulling:
				shard.Status = Serving
				shard.KV = make(map[string]string)
			}
		}
	}
	return &CommandResponse{Err: OK}
}

func (kv *ShardKV) applyEmptyEntry() *CommandResponse {
	return &CommandResponse{Err: OK}
}

// Helper to create Commands
func NewOperationCommand(request *CommandRequest) Command {
	return Command{Operation, *request}
}

func NewConfigurationCommand(config *shardctrler.Config) Command {
	return Command{Configuration, *config}
}

func NewInsertShardsCommand(response *ShardOperationResponse) Command {
	return Command{InsertShards, *response}
}

func NewDeleteShardsCommand(request *ShardOperationRequest) Command {
	return Command{DeleteShards, *request}
}

func NewEmptyEntryCommand() Command {
	return Command{EmptyEntry, nil}
}

// 调试
const Debug = false

func DPrintf(format string, a ...any) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}
