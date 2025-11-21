package shardkv

import (
	"os"
	"testing"

	"github.com/morsuning/distributed-systems-2021-archives/labrpc"
	"github.com/morsuning/distributed-systems-2021-archives/shardctrler"

	// import "log"
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"math/rand"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/morsuning/distributed-systems-2021-archives/raft"
)

func randstring(n int) string {
	b := make([]byte, 2*n)
	crand.Read(b)
	s := base64.URLEncoding.EncodeToString(b)
	return s[0:n]
}

func makeSeed() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := crand.Int(crand.Reader, max)
	x := bigx.Int64()
	return x
}

// random_handles Randomize server handles
// 随机化服务器句柄
func random_handles(kvh []*labrpc.ClientEnd) []*labrpc.ClientEnd {
	sa := make([]*labrpc.ClientEnd, len(kvh))
	copy(sa, kvh)
	for i := range sa {
		j := rand.Intn(i + 1)
		sa[i], sa[j] = sa[j], sa[i]
	}
	return sa
}

type group struct {
	gid       int
	servers   []*ShardKV
	saved     []*raft.Persister
	endnames  [][]string
	mendnames [][]string
}

type config struct {
	mu    sync.Mutex
	t     *testing.T
	net   *labrpc.Network
	start time.Time // time at which make_config() was called
	// make_config() 被调用的时间（用于测试超时控制）

	nctrlers      int
	ctrlerservers []*shardctrler.ShardCtrler
	mck           *shardctrler.Clerk

	ngroups int
	n       int // servers per k/v group
	// 每个 K/V 副本组中的服务器数量
	groups []*group

	clerks       map[*Clerk][]string
	nextClientID int
	maxraftstate int
}

func (cfg *config) checkTimeout() {
	// enforce a two minute real-time limit on each test
	// 为每个测试设置两分钟的实时上限
	if !cfg.t.Failed() && time.Since(cfg.start) > 120*time.Second {
		cfg.t.Fatal("test took longer than 120 seconds")
	}
}

func (cfg *config) cleanup() {
	for gi := 0; gi < cfg.ngroups; gi++ {
		cfg.ShutdownGroup(gi)
	}
	for i := 0; i < cfg.nctrlers; i++ {
		cfg.ctrlerservers[i].Kill()
	}
	cfg.net.Cleanup()
	cfg.checkTimeout()
}

// check that no server's log is too big.
// 检查是否存在某个服务器的日志过大。
func (cfg *config) checklogs() {
	for gi := 0; gi < cfg.ngroups; gi++ {
		for i := 0; i < cfg.n; i++ {
			raft := cfg.groups[gi].saved[i].RaftStateSize()
			snap := len(cfg.groups[gi].saved[i].ReadSnapshot())
			if cfg.maxraftstate >= 0 && raft > 8*cfg.maxraftstate {
				cfg.t.Fatalf("persister.RaftStateSize() %v, but maxraftstate %v",
					raft, cfg.maxraftstate)
			}
			if cfg.maxraftstate < 0 && snap > 0 {
				cfg.t.Fatalf("maxraftstate is -1, but snapshot is non-empty!")
			}
		}
	}
}

// controler server name for labrpc.
// labrpc 中控制器服务的名称。
func (cfg *config) ctrlername(i int) string {
	return "ctrler" + strconv.Itoa(i)
}

// shard server name for labrpc.
// labrpc 中分片服务的服务器名称。
// i'th server of group gid.
// group gid 的第 i 台服务器。
func (cfg *config) servername(gid int, i int) string {
	return "server-" + strconv.Itoa(gid) + "-" + strconv.Itoa(i)
}

func (cfg *config) makeClient() *Clerk {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	// ClientEnds to talk to controler service.
	// 面向控制器服务的客户端端点。
	ends := make([]*labrpc.ClientEnd, cfg.nctrlers)
	endnames := make([]string, cfg.n)
	for j := 0; j < cfg.nctrlers; j++ {
		endnames[j] = randstring(20)
		ends[j] = cfg.net.MakeEnd(endnames[j])
		cfg.net.Connect(endnames[j], cfg.ctrlername(j))
		cfg.net.Enable(endnames[j], true)
	}

	ck := MakeClerk(ends, func(servername string) *labrpc.ClientEnd {
		name := randstring(20)
		end := cfg.net.MakeEnd(name)
		cfg.net.Connect(name, servername)
		cfg.net.Enable(name, true)
		return end
	})
	cfg.clerks[ck] = endnames
	cfg.nextClientID++
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

// Shutdown i'th server of gi'th group, by isolating it
// 通过隔离方式关闭第 gi 个组的第 i 台服务器
func (cfg *config) ShutdownServer(gi int, i int) {
	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	gg := cfg.groups[gi]

	// prevent this server from sending
	// 阻止该服务器继续对外发送请求
	for j := 0; j < len(gg.servers); j++ {
		name := gg.endnames[i][j]
		cfg.net.Enable(name, false)
	}
	for j := 0; j < len(gg.mendnames[i]); j++ {
		name := gg.mendnames[i][j]
		cfg.net.Enable(name, false)
	}

	// disable client connections to the server.
	// 禁用客户端与该服务器的连接。
	// it's important to do this before creating
	// 在创建新的 Persister 之前执行这一步非常关键，
	// the new Persister in saved[i], to avoid
	// 以避免在 saved[i] 创建新 Persister 后，
	// the possibility of the server returning a
	// 服务器仍返回 Append 成功，
	// positive reply to an Append but persisting
	// 并将结果写入已被替换的 Persister，
	// the result in the superseded Persister.
	// 导致持久化状态不一致。
	cfg.net.DeleteServer(cfg.servername(gg.gid, i))

	// a fresh persister, in case old instance
	// 新建一个 Persister，防止旧实例继续写入旧持久化存储。
	// continues to update the Persister.
	// 以确保持久化状态正确隔离。
	// but copy old persister's content so that we always
	// 但需复制旧 Persister 的内容，
	// pass Make() the last persisted state.
	// 确保传递给新启动实例的是最近一次持久化的最新状态。
	if gg.saved[i] != nil {
		gg.saved[i] = gg.saved[i].Copy()
	}

	kv := gg.servers[i]
	if kv != nil {
		cfg.mu.Unlock()
		kv.Kill()
		cfg.mu.Lock()
		gg.servers[i] = nil
	}
}

func (cfg *config) ShutdownGroup(gi int) {
	for i := 0; i < cfg.n; i++ {
		cfg.ShutdownServer(gi, i)
	}
}

// start i'th server in gi'th group
// 启动第 gi 个组中的第 i 台服务器
func (cfg *config) StartServer(gi int, i int) {
	cfg.mu.Lock()

	gg := cfg.groups[gi]

	// a fresh set of outgoing ClientEnd names
	// 为该服务器生成一组新的对外 ClientEnd 名称，
	// to talk to other servers in this group.
	// 用于与同组的其他服务器通信。
	gg.endnames[i] = make([]string, cfg.n)
	for j := 0; j < cfg.n; j++ {
		gg.endnames[i][j] = randstring(20)
	}

	// and the connections to other servers in this group.
	// 建立到同组其他服务器的连接。
	ends := make([]*labrpc.ClientEnd, cfg.n)
	for j := 0; j < cfg.n; j++ {
		ends[j] = cfg.net.MakeEnd(gg.endnames[i][j])
		cfg.net.Connect(gg.endnames[i][j], cfg.servername(gg.gid, j))
		cfg.net.Enable(gg.endnames[i][j], true)
	}

	// ends to talk to shardctrler service
	// 面向分片控制器服务的客户端端点。
	mends := make([]*labrpc.ClientEnd, cfg.nctrlers)
	gg.mendnames[i] = make([]string, cfg.nctrlers)
	for j := 0; j < cfg.nctrlers; j++ {
		gg.mendnames[i][j] = randstring(20)
		mends[j] = cfg.net.MakeEnd(gg.mendnames[i][j])
		cfg.net.Connect(gg.mendnames[i][j], cfg.ctrlername(j))
		cfg.net.Enable(gg.mendnames[i][j], true)
	}

	// a fresh persister, so old instance doesn't overwrite
	// 新建 Persister，避免旧实例覆盖新实例的持久化状态。
	// new instance's persisted state.
	// 确保持久化状态正确隔离。
	// give the fresh persister a copy of the old persister's
	// 将旧 Persister 的内容复制到新 Persister，
	// state, so that the spec is that we pass StartKVServer()
	// 确保传给 StartKVServer() 的是最近一次持久化的最新状态，
	// the last persisted state.
	// 满足接口规范。
	if gg.saved[i] != nil {
		gg.saved[i] = gg.saved[i].Copy()
	} else {
		gg.saved[i] = raft.MakePersister()
	}
	cfg.mu.Unlock()

	gg.servers[i] = StartServer(ends, i, gg.saved[i], cfg.maxraftstate,
		gg.gid, mends,
		func(servername string) *labrpc.ClientEnd {
			name := randstring(20)
			end := cfg.net.MakeEnd(name)
			cfg.net.Connect(name, servername)
			cfg.net.Enable(name, true)
			return end
		})

	kvsvc := labrpc.MakeService(gg.servers[i])
	rfsvc := labrpc.MakeService(gg.servers[i].rf)
	srv := labrpc.MakeServer()
	srv.AddService(kvsvc)
	srv.AddService(rfsvc)
	cfg.net.AddServer(cfg.servername(gg.gid, i), srv)
}

func (cfg *config) StartGroup(gi int) {
	for i := 0; i < cfg.n; i++ {
		cfg.StartServer(gi, i)
	}
}

func (cfg *config) StartCtrlerserver(i int) {
	// ClientEnds to talk to other controler replicas.
	// 面向其他控制器副本的客户端端点。
	ends := make([]*labrpc.ClientEnd, cfg.nctrlers)
	for j := 0; j < cfg.nctrlers; j++ {
		endname := randstring(20)
		ends[j] = cfg.net.MakeEnd(endname)
		cfg.net.Connect(endname, cfg.ctrlername(j))
		cfg.net.Enable(endname, true)
	}

	p := raft.MakePersister()

	cfg.ctrlerservers[i] = shardctrler.StartServer(ends, i, p)

	msvc := labrpc.MakeService(cfg.ctrlerservers[i])
	rfsvc := labrpc.MakeService(cfg.ctrlerservers[i].Raft())
	srv := labrpc.MakeServer()
	srv.AddService(msvc)
	srv.AddService(rfsvc)
	cfg.net.AddServer(cfg.ctrlername(i), srv)
}

func (cfg *config) shardclerk() *shardctrler.Clerk {
	// ClientEnds to talk to ctrler service.
	// 面向控制器服务的客户端端点。
	ends := make([]*labrpc.ClientEnd, cfg.nctrlers)
	for j := 0; j < cfg.nctrlers; j++ {
		name := randstring(20)
		ends[j] = cfg.net.MakeEnd(name)
		cfg.net.Connect(name, cfg.ctrlername(j))
		cfg.net.Enable(name, true)
	}

	return shardctrler.MakeClerk(ends)
}

// tell the shardctrler that a group is joining.
// 通知分片控制器有副本组加入。
func (cfg *config) join(gi int) {
	cfg.joinm([]int{gi})
}

func (cfg *config) joinm(gis []int) {
	m := make(map[int][]string, len(gis))
	for _, g := range gis {
		gid := cfg.groups[g].gid
		servernames := make([]string, cfg.n)
		for i := 0; i < cfg.n; i++ {
			servernames[i] = cfg.servername(gid, i)
		}
		m[gid] = servernames
	}
	cfg.mck.Join(m)
}

// tell the shardctrler that a group is leaving.
// 通知分片控制器有副本组离开。
func (cfg *config) leave(gi int) {
	cfg.leavem([]int{gi})
}

func (cfg *config) leavem(gis []int) {
	gids := make([]int, 0, len(gis))
	for _, g := range gis {
		gids = append(gids, cfg.groups[g].gid)
	}
	cfg.mck.Leave(gids)
}

var ncpu_once sync.Once

func make_config(t *testing.T, n int, unreliable bool, maxraftstate int) *config {
	ncpu_once.Do(func() {
		if runtime.NumCPU() < 2 {
			fmt.Printf("warning: only one CPU, which may conceal locking bugs\n")
		}
		rand.Seed(makeSeed())
	})
	runtime.GOMAXPROCS(4)
	cfg := &config{}
	cfg.t = t
	cfg.maxraftstate = maxraftstate
	cfg.net = labrpc.MakeNetwork()
	cfg.start = time.Now()

	// controler
	// 控制器（shardctrler）
	cfg.nctrlers = 3
	cfg.ctrlerservers = make([]*shardctrler.ShardCtrler, cfg.nctrlers)
	for i := 0; i < cfg.nctrlers; i++ {
		cfg.StartCtrlerserver(i)
	}
	cfg.mck = cfg.shardclerk()

	cfg.ngroups = 3
	cfg.groups = make([]*group, cfg.ngroups)
	cfg.n = n
	for gi := 0; gi < cfg.ngroups; gi++ {
		gg := &group{}
		cfg.groups[gi] = gg
		gg.gid = 100 + gi
		gg.servers = make([]*ShardKV, cfg.n)
		gg.saved = make([]*raft.Persister, cfg.n)
		gg.endnames = make([][]string, cfg.n)
		gg.mendnames = make([][]string, cfg.nctrlers)
		for i := 0; i < cfg.n; i++ {
			cfg.StartServer(gi, i)
		}
	}

	cfg.clerks = make(map[*Clerk][]string)
	cfg.nextClientID = cfg.n + 1000 // client ids start 1000 above the highest serverid

	cfg.net.Reliable(!unreliable)

	return cfg
}
