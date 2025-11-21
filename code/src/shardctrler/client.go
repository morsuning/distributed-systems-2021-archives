// Package shardctrler 包实现了分片控制器，负责在分布式系统中分配与迁移分片。
package shardctrler

// shardctrler 的客户端封装，提供 Join/Leave/Move/Query 的重试与去重逻辑。

import (
	"crypto/rand"
	"math/big"
	"time"

	"github.com/morsuning/distributed-systems-2021-archives/labrpc"
)

type Clerk struct {
	servers []*labrpc.ClientEnd
	// 客户端唯一标识与请求序号，用于服务端请求去重
	clientID   int64
	requestSeq int64
	// 记录最近一次成功的领导者索引，优化后续重试
	leaderHint int
}

func nrand() int64 {
	maxVal := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, maxVal)
	x := bigx.Int64()
	return x
}

func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := new(Clerk)
	ck.servers = servers
	ck.clientID = nrand()
	ck.requestSeq = 0
	ck.leaderHint = 0
	// 设计原因：初始化从 0 开始尝试，随后基于成功的回复更新 leaderHint，
	// 减少无效重试开销，提升请求吞吐与响应时间。
	return ck
}

func (ck *Clerk) Query(num int) Config {
	args := &QueryArgs{}
	// 填充去重字段，保证线性一致的读取通过 Raft 屏障实现
	ck.requestSeq++
	args.ClientID = ck.clientID
	args.RequestID = ck.requestSeq
	args.Num = num
	for {
		// 优先尝试最近的领导者，失败后遍历所有服务器
		n := len(ck.servers)
		for i := 0; i < n; i++ {
			srv := ck.servers[(ck.leaderHint+i)%n]
			var reply QueryReply
			ok := srv.Call("ShardCtrler.Query", args, &reply)
			if ok && !reply.WrongLeader {
				ck.leaderHint = (ck.leaderHint + i) % n
				return reply.Config
			}
		}
		// 选择 100ms 的退避：在不稳定网络下仍能快速重试，同时避免过于频繁的请求造成拥塞。
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Join(servers map[int][]string) {
	args := &JoinArgs{}
	// 去重与重试封装
	ck.requestSeq++
	args.ClientID = ck.clientID
	args.RequestID = ck.requestSeq
	args.Servers = servers

	for {
		n := len(ck.servers)
		for i := 0; i < n; i++ {
			srv := ck.servers[(ck.leaderHint+i)%n]
			var reply JoinReply
			ok := srv.Call("ShardCtrler.Join", args, &reply)
			if ok && !reply.WrongLeader {
				ck.leaderHint = (ck.leaderHint + i) % n
				return
			}
		}
		// 选择 100ms 的退避：在不稳定网络下仍能快速重试，同时避免过于频繁的请求造成拥塞。
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Leave(gids []int) {
	args := &LeaveArgs{}
	ck.requestSeq++
	args.ClientID = ck.clientID
	args.RequestID = ck.requestSeq
	args.GIDs = gids

	for {
		n := len(ck.servers)
		for i := 0; i < n; i++ {
			srv := ck.servers[(ck.leaderHint+i)%n]
			var reply LeaveReply
			ok := srv.Call("ShardCtrler.Leave", args, &reply)
			if ok && !reply.WrongLeader {
				ck.leaderHint = (ck.leaderHint + i) % n
				return
			}
		}
		// 选择 100ms 的退避：在不稳定网络下仍能快速重试，同时避免过于频繁的请求造成拥塞。
		time.Sleep(100 * time.Millisecond)
	}
}

func (ck *Clerk) Move(shard int, gid int) {
	args := &MoveArgs{}
	ck.requestSeq++
	args.ClientID = ck.clientID
	args.RequestID = ck.requestSeq
	args.Shard = shard
	args.GID = gid

	for {
		n := len(ck.servers)
		for i := 0; i < n; i++ {
			srv := ck.servers[(ck.leaderHint+i)%n]
			var reply MoveReply
			ok := srv.Call("ShardCtrler.Move", args, &reply)
			if ok && !reply.WrongLeader {
				ck.leaderHint = (ck.leaderHint + i) % n
				return
			}
		}
		// 选择 100ms 的退避：在不稳定网络下仍能快速重试，同时避免过于频繁的请求造成拥塞。
		time.Sleep(100 * time.Millisecond)
	}
}
