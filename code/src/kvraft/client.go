package kvraft

import (
	"crypto/rand"
	"math/big"
	"time"

	"github.com/morsuning/distributed-systems-2021-archives/labrpc"
)

type Clerk struct {
	servers []*labrpc.ClientEnd
	// 为什么要缓存 Leader：减少每次 RPC 的寻址成本，满足速度测试；若失败再轮询。
	leaderHint int
	// 客户端唯一标识：用于服务端去重表。
	clientID int64
	// 递增的请求序号：同一客户端下唯一，用于判定重复请求。
	nextReqID int64
}

func nrand() int64 {
	maxInt := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, maxInt)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	ck.leaderHint = 0
	ck.clientID = nrand()
	ck.nextReqID = 1
	return ck
}

// Get 获取指定键的当前值。
// 如果键不存在则返回空字符串。
// 在其他错误情况下会持续重试。
//
// 你可以像下面这样发送一个 RPC：
// ok := ck.servers[i].Call("KVServer.Get", &args, &reply)
//
// 参数与返回值的类型（包括是否为指针）必须与 RPC 处理函数声明一致；
// 且 reply 必须以指针传入。
func (ck *Clerk) Get(key string) string {
	// 设计说明：将 Get 也作为日志指令提交，保证读操作只在多数派提交后返回，提供线性一致性。
	reqID := ck.nextReqID
	ck.nextReqID++
	args := &GetArgs{Key: key, ClientId: ck.clientID, RequestId: reqID}

	// 优先尝试缓存 Leader；若不成功再轮询其他服务器，直到成功。
	// 不设置整体超时：测试需要在网络分区恢复后继续完成。
	startAt := ck.leaderHint
	n := len(ck.servers)
	for {
		// 按照 leaderHint 开始的轮询顺序尝试所有服务器
		for i := 0; i < n; i++ {
			srvIdx := (startAt + i) % n
			var reply GetReply
			ok := ck.servers[srvIdx].Call("KVServer.Get", args, &reply)
			if ok && reply.Err == OK {
				// 更新 leaderHint，加快后续调用
				ck.leaderHint = srvIdx
				return reply.Value
			}
			// 错误场景：WrongLeader 或 RPC 失败，继续尝试下一个服务器
		}
		// 放弃 CPU，避免忙等导致测试耗时增大
		time.Sleep(20 * time.Millisecond)
	}
}

// PutAppend 被 Put 与 Append 共用。
// 你可以像下面这样发送一个 RPC：
// ok := ck.servers[i].Call("KVServer.PutAppend", &args, &reply)
//
// 参数与返回值的类型（包括是否为指针）必须与 RPC 处理函数声明一致；
// 且 reply 必须以指针传入。
func (ck *Clerk) PutAppend(key string, value string, op string) {
	// Put/Append 必须保证“至多一次”效果：通过唯一请求ID + 服务端去重实现。
	reqID := ck.nextReqID
	ck.nextReqID++
	args := &PutAppendArgs{Key: key, Value: value, Op: op, ClientId: ck.clientID, RequestId: reqID}

	startAt := ck.leaderHint
	n := len(ck.servers)
	for {
		for i := 0; i < n; i++ {
			srvIdx := (startAt + i) % n
			var reply PutAppendReply
			ok := ck.servers[srvIdx].Call("KVServer.PutAppend", args, &reply)
			if ok && reply.Err == OK {
				ck.leaderHint = srvIdx
				return
			}
			// WrongLeader 或 RPC 超时：继续尝试下一个
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (ck *Clerk) Put(key string, value string) {
	ck.PutAppend(key, value, "Put")
}
func (ck *Clerk) Append(key string, value string) {
	ck.PutAppend(key, value, "Append")
}
