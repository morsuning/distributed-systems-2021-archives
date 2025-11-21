# MIT 6.824 Distributed Systems Spring 2021 Materials

[English](./README.md) | [简体中文](./README_CN.md)

This project implements the core labs of the MIT 6.824 Distributed Systems course, building a complete distributed system that includes a MapReduce framework and a Sharded Key/Value Storage System based on the Raft consensus algorithm. In addition, it also includes all course notes, papers, and exam materials, and provides a Chinese translation version.

> The implementation of the Raft part, i.e., Lab 2, has passed 50 consecutive full tests without failure.

## Features

### Lab 1: MapReduce

Implemented a distributed MapReduce framework with fault tolerance and parallel execution capabilities.

- **Coordinator**: Task scheduling, state tracking, timeout retries (10s).
- **Worker**: Parallel execution of Map/Reduce tasks.
- **Fault Tolerance**: Automatic handling of Worker failures.

### Lab 2: Raft Consensus Algorithm

Implemented the core functionality of the Raft protocol to ensure distributed consistency.

- **Leader Election**: Handles network partitions and node failures.
- **Log Replication**: Ensures log consistency.
- **Persistence**: Crash recovery.
- **Log Compaction**: Snapshot mechanism.

### Lab 3: Fault-tolerant Key/Value Service (KVServer)

Strongly consistent KV storage based on Raft.

- **Linearizability**: Atomicity for Get/Put/Append operations.
- **At-Most-Once Semantics**: Deduplication mechanism to prevent duplicate execution.
- **Snapshot Management**: Automatic log compaction.

### Lab 4: Sharded Key/Value Service (ShardKV)

Distributed KV system supporting dynamic scaling.

- **Shard Controller**: Manages configuration and shard allocation.
- **Shard Migration**: Automatic data migration during configuration changes.
- **Load Balancing**: Even distribution of shards.

## Project Structure

```
.
├── code/           # Source Code
│   ├── src/mr/     # Lab 1: MapReduce
│   ├── src/raft/   # Lab 2: Raft
│   ├── src/kvraft/ # Lab 3: KVServer
│   ├── src/shardctrler/ # Lab 4A: Shard Controller
│   └── src/shardkv/     # Lab 4B: Sharded KV
├── docs/           # Detailed Implementation Documentation
├── lab/            # Lab Instructions
└── paper/          # Reference Papers (Raft, etc.)
```

## Getting Started

### Prerequisites

- **Go**: 1.15+
- **macOS**: Requires `coreutils` for the `timeout` command (Lab 1).
  ```bash
  brew install coreutils
  ```

### Running Tests

#### Lab 1: MapReduce

```bash
cd code/src/main
# macOS requires setting PATH to use gtimeout
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

## References

- [MIT 6.824 Schedule](http://nil.csail.mit.edu/6.824/2021/schedule.html)
- [Raft Paper (Extended)](./paper/LEC%205%20-%20paper%204%20-%20raft-extended.pdf)
