package shardkv

// 与分片键/值服务交互的客户端代码。
// 客户端首先与分片控制器通信以获取
// 分片（键）到各组的分配，然后
// 再与持有该键所属分片的组通信。

import (
	"crypto/rand"
	"math/big"
	"time"

	"github.com/morsuning/distributed-systems-2021-archives/labrpc"
	"github.com/morsuning/distributed-systems-2021-archives/shardctrler"
)

func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

type Clerk struct {
	sm       *shardctrler.Clerk
	config   shardctrler.Config
	make_end func(string) *labrpc.ClientEnd
	// You will have to modify this struct.
	clientId  int64
	commandId int64
}

// MakeClerk 测试器会调用 MakeClerk。
// 调用 shardctrler.MakeClerk() 需要传入 ctrlers[]。
// make_end(servername) 将来自
// Config.Groups[gid][i] 的服务器名转换为可用的 labrpc.ClientEnd，
// 用于发送 RPC。
func MakeClerk(ctrlers []*labrpc.ClientEnd, make_end func(string) *labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.sm = shardctrler.MakeClerk(ctrlers)
	ck.make_end = make_end
	// You'll have to add code here.
	ck.clientId = nrand()
	ck.commandId = 0
	return ck
}

func (ck *Clerk) Command(request *CommandRequest) string {
	request.ClientId, request.CommandId = ck.clientId, ck.commandId
	for {
		shard := key2shard(request.Key)
		gid := ck.config.Shards[shard]
		if servers, ok := ck.config.Groups[gid]; ok {
			for si := 0; si < len(servers); si++ {
				srv := ck.make_end(servers[si])
				var response CommandResponse
				ok := srv.Call("ShardKV.Command", request, &response)
				if ok && (response.Err == OK || response.Err == ErrNoKey) {
					ck.commandId++
					return response.Value
				}
				if ok && (response.Err == ErrWrongGroup) {
					break
				}
				// ... not ok, or ErrWrongLeader
			}
		}
		time.Sleep(100 * time.Millisecond)
		// ask controler for the latest configuration.
		ck.config = ck.sm.Query(-1)
	}
}

// Get 获取给定键的当前值。
// 若键不存在，返回空字符串 ""。
// 在其他错误情况下会持续重试，直至成功。
// 你需要实现或修改此函数。
func (ck *Clerk) Get(key string) string {
	return ck.Command(&CommandRequest{Key: key, Op: OpGet})
}

// PutAppend 供 Put 和 Append 共用。
// 你需要实现或修改此函数。
func (ck *Clerk) PutAppend(key string, value string, op OperationOp) {
	ck.Command(&CommandRequest{Key: key, Value: value, Op: op})
}

func (ck *Clerk) Put(key string, value string) {
	ck.PutAppend(key, value, OpPut)
}
func (ck *Clerk) Append(key string, value string) {
	ck.PutAppend(key, value, OpAppend)
}
