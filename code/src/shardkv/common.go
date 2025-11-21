package shardkv

import "github.com/morsuning/distributed-systems-2021-archives/shardctrler"

// 分片键/值服务器。
// 包含多个复制组，每个组运行 Raft。
// 分片控制器决定哪个组服务哪个分片。
// 分片控制器可能会不时更改分片分配。
//
// 你需要修改这些定义。
//

const (
	OK             = "OK"
	ErrNoKey       = "ErrNoKey"
	ErrWrongGroup  = "ErrWrongGroup"
	ErrWrongLeader = "ErrWrongLeader"
	ErrOutDated    = "ErrOutDated"
	ErrNotReady    = "ErrNotReady"
)

type Err string

type OperationOp string

const (
	OpGet    OperationOp = "Get"
	OpPut    OperationOp = "Put"
	OpAppend OperationOp = "Append"
)

// CommandRequest 客户端操作的统一请求结构
type CommandRequest struct {
	Key       string
	Value     string
	Op        OperationOp
	ClientId  int64
	CommandId int64
}

// CommandResponse 客户端操作的统一响应结构
type CommandResponse struct {
	Err   Err
	Value string
}

// ShardOperationRequest 分片迁移和 GC 的请求结构
type ShardOperationRequest struct {
	ConfigNum int
	ShardIDs  []int
}

// ShardOperationResponse 分片迁移和 GC 的响应结构
type ShardOperationResponse struct {
	Err            Err
	ConfigNum      int
	Shards         map[int]map[string]string
	LastOperations map[int64]OperationContext
}

type OperationContext struct {
	MaxAppliedCommandId int64
	LastResponse        *CommandResponse
}

type CommandType uint8

const (
	Operation CommandType = iota
	Configuration
	InsertShards
	DeleteShards
	EmptyEntry
)

// Command Raft 日志条目结构
type Command struct {
	Op   CommandType
	Data interface{}
}

type ShardStatus uint8

const (
	Serving ShardStatus = iota
	Pulling
	BePulling
	GCing
)

// 键属于哪个分片？
// 请使用此函数，
// 请勿更改其实现。
func key2shard(key string) int {
	shard := 0
	if len(key) > 0 {
		shard = int(key[0])
	}
	shard %= shardctrler.NShards
	return shard
}
