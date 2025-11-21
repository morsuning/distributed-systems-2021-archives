# MIT 6.824 分布式系统 Spring 2021 资料

[English](./README.md) | [简体中文](./README_CN.md)

本项目实现了 MIT 6.824 分布式系统课程的核心 Lab，构建了一个完整的分布式系统，包括 MapReduce 框架和基于 Raft 共识算法的分片键值存储系统，此外还包括课程的全部笔记、论文和考试资料，并提供了中文翻译版本。

> Raft部分实现，即Lab 2，已通过连续50次完整测试不失败

## 功能特性

### Lab 1: MapReduce

实现了分布式 MapReduce 框架，具备容错和并行执行能力。

- **Coordinator**：任务调度、状态追踪、超时重试（10s）。
- **Worker**：并行执行 Map/Reduce 任务。
- **容错性**：自动处理 Worker 故障。

### Lab 2: Raft 共识算法

实现了 Raft 协议核心功能，确保分布式一致性。

- **Leader 选举**：处理网络分区与节点故障。
- **日志复制**：保证日志一致性。
- **持久化**：崩溃恢复。
- **日志压缩**：Snapshot 机制。

### Lab 3: 容错键值服务 (KVServer)

基于 Raft 的强一致性 KV 存储。

- **线性一致性**：Get/Put/Append 操作原子性。
- **至多一次语义**：去重机制防止重复执行。
- **快照管理**：自动压缩日志。

### Lab 4: 分片键值服务 (ShardKV)

支持动态扩缩容的分布式 KV 系统。

- **Shard Controller**：管理配置与分片分配。
- **分片迁移**：配置变更时自动迁移数据。
- **负载均衡**：均匀分配分片。

## 项目结构

```
.
├── code/           # 源代码
│   ├── src/mr/     # Lab 1: MapReduce
│   ├── src/raft/   # Lab 2: Raft
│   ├── src/kvraft/ # Lab 3: KVServer
│   ├── src/shardctrler/ # Lab 4A: Shard Controller
│   └── src/shardkv/     # Lab 4B: Sharded KV
├── docs/           # 详细实现文档
├── lab/            # 实验指导书
└── paper/          # 参考论文 (Raft 等)
```

## 快速开始

### 环境要求

- **Go**: 1.15+
- **macOS**: 需要安装 `coreutils` 以支持 `timeout` 命令 (Lab 1)。
  ```bash
  brew install coreutils
  ```

### 运行测试

#### Lab 1: MapReduce

```bash
cd code/src/main
# macOS 需要设置 PATH 以使用 gtimeout
PATH="/opt/homebrew/opt/coreutils/libexec/gnubin:$PATH" bash test-mr.sh
```

#### Lab 2: Raft

```bash
cd code/src/raft
go test -race
```

#### Lab 3: KVServer

```bash
cd code/src/kvraft
go test -race
```

#### Lab 4: Sharded KV

```bash
cd code/src/shardkv
go test -race
```

## 参考资料

- [MIT 6.824 Schedule](http://nil.csail.mit.edu/6.824/2021/schedule.html)
- [Raft Paper (Extended)](./paper/LEC%205%20-%20paper%204%20-%20raft-extended.pdf)
