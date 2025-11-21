package shardctrler

//
// 分片控制器：将分片分配到各个复制组中。
//
// RPC 接口：
// Join(servers) -- 添加一组复制组（gid -> 服务器列表映射）。
// Leave(gids) -- 删除一组复制组。
// Move(shard, gid) -- 将一个分片从当前所属组迁移到指定 gid。
// Query(num) -> 读取编号为 num 的配置；若 num==-1 则读取最新配置。
//
// 一个 Config（配置）描述了复制组集合，以及每个分片由哪个复制组负责。配置以编号区分。
// 编号 #0 为初始配置：没有任何组，所有分片分配给 0 号（无效）组。
//
// 需要在 RPC 参数结构体中添加额外字段以支持幂等与去重。
//

// NShards 分片总数。
const NShards = 10

// Config 表示一种配置，即分片到组的分配方案。
// 请不要修改此结构体定义（测试用例依赖其形状）。
type Config struct {
	Num int // config number
	// 配置编号，用于历史查询与线性序列验证。
	Shards [NShards]int // shard -> gid
	// 分片到 gid 的映射，表示每个分片的当前归属组。
	Groups map[int][]string // gid -> servers[]
	// gid 到服务器列表的映射，描述组内成员。
}

const (
	OK = "OK"
)

type Err string

type JoinArgs struct {
	Servers map[int][]string // new GID -> servers mappings
	// 新加入组的服务器映射；每个 gid 对应一组服务器地址。
	// 客户端唯一标识与请求序号，用于去重确保“至多一次”。
	ClientID  int64
	RequestID int64
}

type JoinReply struct {
	WrongLeader bool
	Err         Err
}

type LeaveArgs struct {
	GIDs []int
	// 离开（移除）的 gid 列表；这些组的分片将被重新分配。
	// 客户端唯一标识与请求序号，用于去重确保“至多一次”。
	ClientID  int64
	RequestID int64
}

type LeaveReply struct {
	WrongLeader bool
	Err         Err
}

type MoveArgs struct {
	Shard int
	GID   int
	// 需要被移动的分片编号及目标 gid；用于临时修复或微调负载。
	// 客户端唯一标识与请求序号，用于去重确保“至多一次”。
	ClientID  int64
	RequestID int64
}

type MoveReply struct {
	WrongLeader bool
	Err         Err
}

type QueryArgs struct {
	Num int // desired config number
	// 期望读取的配置编号；-1 表示读取最新配置。
	// 客户端唯一标识与请求序号，用于在不可靠网络下保持顺序与可见性。
	ClientID  int64
	RequestID int64
}

type QueryReply struct {
	WrongLeader bool
	Err         Err
	Config      Config
}

func DefaultConfig() Config {
	return Config{
		Num:    0,
		Shards: [NShards]int{},
		Groups: make(map[int][]string),
	}
}
