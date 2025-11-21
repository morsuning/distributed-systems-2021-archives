package kvraft

const (
	OK             = "OK"
	ErrNoKey       = "ErrNoKey"
	ErrWrongLeader = "ErrWrongLeader"
	ErrTimeout     = "ErrTimeout"
)

type Err string

// PutAppendArgs Put 或 Append 请求参数
type PutAppendArgs struct {
	// 你需要在此添加字段定义。
	Key   string
	Value string
	Op    string // "Put" or "Append"
	// 字段名必须以大写字母开头，否则 RPC 编解码会失败。
	// 为什么需要唯一请求标识：3A 要求同一 Clerk 的一次调用只能执行一次；
	// 通过 ClientId + RequestId 标识幂等性，服务端据此进行去重。
	ClientId  int64
	RequestId int64
}

type PutAppendReply struct {
	Err Err
}

type GetArgs struct {
	Key string
	// 你需要在此添加字段定义。
	// 为了保证读取也具备线性一致性，我们将 Get 纳入 Raft 日志。
	// 同样携带唯一请求标识以便端到端重试不会重复影响状态机。
	ClientId  int64
	RequestId int64
}

type GetReply struct {
	Err   Err
	Value string
}
