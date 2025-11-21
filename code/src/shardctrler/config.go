package shardctrler

import (
	"os"
	"testing"

	"github.com/morsuning/distributed-systems-2021-archives/labrpc"
	"github.com/morsuning/distributed-systems-2021-archives/raft"

	// import "log"
	crand "crypto/rand"
	"encoding/base64"
	"math/rand"
	"runtime"
	"sync"
	"time"
)

func randString(n int) string {
    b := make([]byte, 2*n)
    crand.Read(b)
    s := base64.URLEncoding.EncodeToString(b)
    return s[0:n]
}

// Randomize server handles
// 随机化服务器句柄顺序，避免测试中固定顺序带来的偏差。
func randomHandles(kvh []*labrpc.ClientEnd) []*labrpc.ClientEnd {
	sa := make([]*labrpc.ClientEnd, len(kvh))
	copy(sa, kvh)
	for i := range sa {
		j := rand.Intn(i + 1)
		sa[i], sa[j] = sa[j], sa[i]
	}
	return sa
}

type config struct {
    mu           sync.Mutex
    t            *testing.T
    net          *labrpc.Network
    n            int
    servers      []*ShardCtrler
    saved        []*raft.Persister
    endNames     [][]string // names of each server's sending ClientEnds
                            // 每台服务器用于发送的 ClientEnd 名称列表（出站连接）。
    clerks       map[*Clerk][]string
    nextClientID int
    start        time.Time // time at which makeConfig() was called
                            // 记录 makeConfig() 被调用的时刻，用于测试超时控制。
}

func (cfg *config) checkTimeout() {
    // enforce a two-minute real-time limit on each test
    // 对每个测试实施两分钟的实时限制，避免挂起影响整体耗时。
    if !cfg.t.Failed() && time.Since(cfg.start) > 120*time.Second {
        cfg.t.Fatal("test took longer than 120 seconds")
    }
}

func (cfg *config) cleanup() {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	for i := 0; i < len(cfg.servers); i++ {
		if cfg.servers[i] != nil {
			cfg.servers[i].Kill()
		}
	}
	cfg.net.Cleanup()
	cfg.checkTimeout()
}

// LogSize Maximum log size across all servers
// LogSize 统计所有服务器中最大的 Raft 日志持久化大小，用于观察截断与快照效果。
func (cfg *config) LogSize() int {
	logSize := 0
	for i := 0; i < cfg.n; i++ {
		n := cfg.saved[i].RaftStateSize()
		if n > logSize {
			logSize = n
		}
	}
	return logSize
}

// attach server i to servers listed in to
// 将服务器 i 连接到 to 列表中的服务器集合。
// caller must hold cfg.mu
// 调用者必须持有 cfg.mu 锁以保证网络状态一致性。
func (cfg *config) connectUnlocked(i int, to []int) {
    // log.Printf("connect peer %d to %v\n", i, to)

    // outgoing socket files
    // 出站套接字文件：启用 i 到每个目标的发送端。
    for j := 0; j < len(to); j++ {
        endName := cfg.endNames[i][to[j]]
        cfg.net.Enable(endName, true)
    }

    // incoming socket files
    // 入站套接字文件：启用每个目标到 i 的接收端。
    for j := 0; j < len(to); j++ {
        endName := cfg.endNames[to[j]][i]
        cfg.net.Enable(endName, true)
    }
}

func (cfg *config) connect(i int, to []int) {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	cfg.connectUnlocked(i, to)
}

func (cfg *config) disconnect(i int, from []int) {
    cfg.mu.Lock()
    defer cfg.mu.Unlock()
    cfg.disconnectUnlocked(i, from)
}

// detach server i from the servers listed in from
// 将服务器 i 与 from 列表中的服务器断开连接。
// caller must hold cfg.mu
// 调用者必须持有 cfg.mu 锁以保证网络状态一致性。
func (cfg *config) disconnectUnlocked(i int, from []int) {
    // log.Printf("disconnect peer %d from %v\n", i, from)

    // outgoing socket files
    // 出站套接字文件：禁用 i 到每个来源的发送端。
    for j := 0; j < len(from); j++ {
        if cfg.endNames[i] != nil {
            endName := cfg.endNames[i][from[j]]
            cfg.net.Enable(endName, false)
        }
    }

    // incoming socket files
    // 入站套接字文件：禁用每个来源到 i 的接收端。
    for j := 0; j < len(from); j++ {
        if cfg.endNames[j] != nil {
            endName := cfg.endNames[from[j]][i]
            cfg.net.Enable(endName, false)
        }
    }
}

func (cfg *config) All() []int {
	all := make([]int, cfg.n)
	for i := 0; i < cfg.n; i++ {
		all[i] = i
	}
	return all
}

func (cfg *config) ConnectAll() {
    cfg.mu.Lock()
    defer cfg.mu.Unlock()
    for i := 0; i < cfg.n; i++ {
        cfg.connectUnlocked(i, cfg.All())
    }
}

// Sets up 2 partitions with connectivity between servers in each  partition.
// 设置两组分区，使得分区内服务器互联、分区间隔离，用于测试分区容错行为。
func (cfg *config) partition(p1 []int, p2 []int) {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	// log.Printf("partition servers into: %v %v\n", p1, p2)
	for i := 0; i < len(p1); i++ {
		cfg.disconnectUnlocked(p1[i], p2)
		cfg.connectUnlocked(p1[i], p1)
	}
	for i := 0; i < len(p2); i++ {
		cfg.disconnectUnlocked(p2[i], p1)
		cfg.connectUnlocked(p2[i], p2)
	}
}

// Create a clerk with clerk specific server names.
// 创建一个 clerk，并为其分配独立的服务器端点名称。
// Give it connections to all the servers, but for
// now enable only connections to servers in to[].
// 为其建立到所有服务器的连接，但仅启用 to[] 中服务器的连接，模拟部分可达。
func (cfg *config) makeClient(to []int) *Clerk {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()

    // a fresh set of ClientEnds.
    // 为该客户端生成一套新的 ClientEnd 并接入测试网络。
    ends := make([]*labrpc.ClientEnd, cfg.n)
    endNames := make([]string, cfg.n)
    for j := 0; j < cfg.n; j++ {
        endNames[j] = randString(20)
        ends[j] = cfg.net.MakeEnd(endNames[j])
        cfg.net.Connect(endNames[j], j)
    }

	ck := MakeClerk(randomHandles(ends))
	cfg.clerks[ck] = endNames
	cfg.nextClientID++
	cfg.ConnectClientUnlocked(ck, to)
	return ck
}

func (cfg *config) deleteClient(ck *Clerk) {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	v := cfg.clerks[ck]
	for i := 0; i < len(v); i++ {
		os.Remove(v[i])
	}
	delete(cfg.clerks, ck)
}

// ConnectClientUnlocked caller should hold cfg.mu
// 连接客户端到指定服务器集合（调用者需持有 cfg.mu）。
func (cfg *config) ConnectClientUnlocked(ck *Clerk, to []int) {
	// log.Printf("ConnectClient %v to %v\n", ck, to)
	endNames := cfg.clerks[ck]
	for j := 0; j < len(to); j++ {
		s := endNames[to[j]]
		cfg.net.Enable(s, true)
	}
}

func (cfg *config) ConnectClient(ck *Clerk, to []int) {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	cfg.ConnectClientUnlocked(ck, to)
}

// DisconnectClientUnlocked caller should hold cfg.mu
// 断开客户端与指定服务器集合（调用者需持有 cfg.mu）。
func (cfg *config) DisconnectClientUnlocked(ck *Clerk, from []int) {
	// log.Printf("DisconnectClient %v from %v\n", ck, from)
	endNames := cfg.clerks[ck]
	for j := 0; j < len(from); j++ {
		s := endNames[from[j]]
		cfg.net.Enable(s, false)
	}
}

func (cfg *config) DisconnectClient(ck *Clerk, from []int) {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()
	cfg.DisconnectClientUnlocked(ck, from)
}

// ShutdownServer Shutdown a server by isolating it
// 通过网络隔离的方式关闭服务器，用于模拟宕机或重启。
func (cfg *config) ShutdownServer(i int) {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	cfg.disconnectUnlocked(i, cfg.All())

    // disable client connections to the server.
    // 禁用客户端到该服务器的连接，确保不会再接受请求。
    // it's important to do this before creating
    // 在创建新的持久化对象之前必须先禁用连接，
    // the new Persister in saved[i], to avoid
    // 避免出现服务器对 Append 返回正面回复但实际写入旧 Persister 的情况，
    // the possibility of the server returning a
    // 造成状态不一致的可能。
    // positive reply to an Append but persisting
    // the result in the superseded Persister.
	cfg.net.DeleteServer(i)

    // a fresh persister, in case old instance
    // 准备一个新的持久化对象，防止旧实例继续写入旧对象。
    // continues to update the Persister.
    // but copy old persister's content so that we always
    // 同时复制旧持久化内容，以保证后续 Make() 总是接收最后一次持久化的状态。
    // pass Make() the last persisted state.
	if cfg.saved[i] != nil {
		cfg.saved[i] = cfg.saved[i].Copy()
	}

	kv := cfg.servers[i]
	if kv != nil {
		cfg.mu.Unlock()
		kv.Kill()
		cfg.mu.Lock()
		cfg.servers[i] = nil
	}
}

// StartServer If restart servers, first call ShutdownServer
// 启动服务器；若是重启场景，应先调用 ShutdownServer 进行必要的隔离与清理。
func (cfg *config) StartServer(i int) {
	cfg.mu.Lock()

    // a fresh set of outgoing ClientEnd names.
    // 为该服务器生成新的出站 ClientEnd 名称集合，避免与旧实例混淆。
    cfg.endNames[i] = make([]string, cfg.n)
	for j := 0; j < cfg.n; j++ {
		cfg.endNames[i][j] = randString(20)
	}

    // a fresh set of ClientEnds.
    // 为该服务器生成新的 ClientEnd 集合并接入测试网络。
    ends := make([]*labrpc.ClientEnd, cfg.n)
	for j := 0; j < cfg.n; j++ {
		ends[j] = cfg.net.MakeEnd(cfg.endNames[i][j])
		cfg.net.Connect(cfg.endNames[i][j], j)
	}

    // a fresh persister, so old instance doesn't overwrite
    // 创建新的持久化对象，避免旧实例覆盖新实例的持久化状态。
    // new instance's persisted state.
    // give the fresh persister a copy of the old persister's
    // 将旧持久化对象内容复制到新对象，
    // state, so that the spec is that we pass StartKVServer()
    // 以保证传递给 StartServer() 的是最后一次持久化的状态。
    // the last persisted state.
	if cfg.saved[i] != nil {
		cfg.saved[i] = cfg.saved[i].Copy()
	} else {
		cfg.saved[i] = raft.MakePersister()
	}

	cfg.mu.Unlock()

	cfg.servers[i] = StartServer(ends, i, cfg.saved[i])

	kvsvc := labrpc.MakeService(cfg.servers[i])
	rfsvc := labrpc.MakeService(cfg.servers[i].rf)
	srv := labrpc.MakeServer()
	srv.AddService(kvsvc)
	srv.AddService(rfsvc)
	cfg.net.AddServer(i, srv)
}

func (cfg *config) Leader() (bool, int) {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	for i := 0; i < cfg.n; i++ {
		if cfg.servers[i] != nil {
			_, isLeader := cfg.servers[i].rf.GetState()
			if isLeader {
				return true, i
			}
		}
	}
	return false, 0
}

// Partition servers into 2 groups and put current leader in minority
// 将服务器分成两组，并将当前领导者放入少数派分区，用于制造领导权转移与网络分区场景。
func (cfg *config) makePartition() ([]int, []int) {
	_, l := cfg.Leader()
	p1 := make([]int, cfg.n/2+1)
	p2 := make([]int, cfg.n/2)
	j := 0
	for i := 0; i < cfg.n; i++ {
		if i != l {
			if j < len(p1) {
				p1[j] = i
			} else {
				p2[j-len(p1)] = i
			}
			j++
		}
	}
	p2[len(p2)-1] = l
	return p1, p2
}

func makeConfig(t *testing.T, n int, unreliable bool) *config {
	runtime.GOMAXPROCS(4)
	cfg := &config{}
	cfg.t = t
	cfg.net = labrpc.MakeNetwork()
	cfg.n = n
	cfg.servers = make([]*ShardCtrler, cfg.n)
	cfg.saved = make([]*raft.Persister, cfg.n)
	cfg.endNames = make([][]string, cfg.n)
	cfg.clerks = make(map[*Clerk][]string)
    cfg.nextClientID = cfg.n + 1000 // client ids start 1000 above the highest serverid
                                    // 客户端 ID 从最高服务器 ID 之上 1000 处开始分配，避免冲突。
	cfg.start = time.Now()

    // create a full set of KV servers.
    // 启动完整的服务器集合以支持接下来的测试。
    for i := 0; i < cfg.n; i++ {
        cfg.StartServer(i)
    }

	cfg.ConnectAll()

    cfg.net.Reliable(!unreliable)
    // 根据参数决定网络是否可靠，以覆盖不同测试场景。

	return cfg
}
